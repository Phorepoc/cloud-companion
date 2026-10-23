// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phesmssync.cpp -- the Phorepoc SMS service for Phecloud (docs/sms.md, hub-design.md section 2).
//
// A background exe. It watches the Symbian message server and mirrors this phone's SMS into the owner's phecloud hub:
//  - every incoming SMS in the Inbox and every sent SMS in the Sent folder is uploaded once, with its own TMsvId as the message id
//    (the server ignores an id it has already seen, so a retry or a restart never duplicates anything);
//  - what the owner sends from the web app arrives in the phone's pending queue; it is built as an SMS in the Drafts folder through
//    CSmsClientMtm, moved to the Outbox and scheduled (ESmsMtmCommandScheduleCopy: the real send path, smss -> the SMS stack -> ETel),
//    and the result (sent / failed) is reported back when the message server marks the entry.
// Offline operation: nothing is dropped. The upload cursor (the highest TMsvId uploaded per folder) and the list of messages whose result
// still has to be reported live in C:\data\phesmssync\state.txt; a failed upload leaves the cursor where it was, so the next cycle sends the
// same messages again. The link is the Phecloud one (ss_net.c: TLS 1.3, pinned certificate, device key from C:\data\phecloud.ini).
#include <e32base.h>
#include <e32std.h>
#include <e32debug.h>
#include <e32math.h>
#include <f32file.h>
#include <msvapi.h>
#include <msvids.h>
#include <msvuids.h>
#include <mtclreg.h>
#include <smsclnt.h>
#include <smscmds.h>
#include <smut.h>
#include <smuthdr.h>
#include <txtrich.h>
#include <utf.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

#include "ss_net.h"

extern "C" int mbedtls_hardware_poll(void*, unsigned char* aOutput, size_t aLen, size_t* aOLen)
	{
	TPtr8 out(aOutput, 0, (TInt)aLen);
	out.SetLength((TInt)aLen);
	Math::Random(out);
	*aOLen = aLen;
	return 0;
	}

// the Phecloud settings file, written by the engine's pairing screen (read with the Symbian file server: Open C's fopen on C:\data did not find it from this exe)
extern "C" int ss_read_ini(char* aBuf, int aCap)
	{
	RFs fs;
	if (fs.Connect() != KErrNone)
		return -1;
	RFile f;
	TInt r = f.Open(fs, _L("C:\\data\\phecloud.ini"), EFileRead | EFileShareAny);
	if (r != KErrNone)
		{
		RDebug::Printf("[phesmssync] cannot open C:\\data\\phecloud.ini: %d", r);
		fs.Close();
		return -1;
		}
	TPtr8 p((TUint8*)aBuf, 0, aCap);
	r = f.Read(p);
	f.Close();
	fs.Close();
	return r == KErrNone ? p.Length() : -1;
	}

_LIT(KStateDir, "C:\\data\\phesmssync\\");
_LIT(KStateFile, "C:\\data\\phesmssync\\state.txt");
const TInt KCycleSeconds = 10;
const TInt KIdleSeconds = 300;     // no (or an unusable) C:\data\phecloud.ini: look again every 5 minutes, quietly
const TInt KBatch = 4;            // messages per upload (one frame must stay small)
const TInt KMaxAcks = 16;
const TInt KMaxPending = 4;

#define LOG(...) RDebug::Printf(__VA_ARGS__)

struct TAck { TInt64 iOutId; TMsvId iId; };   // an outbound message whose result has not been reported yet

class CSmsSync : public CActive, public MMsvSessionObserver
	{
public:
	static CSmsSync* NewL()
		{
		CSmsSync* s = new (ELeave) CSmsSync;
		CleanupStack::PushL(s);
		s->ConstructL();
		CleanupStack::Pop(s);
		return s;
		}
	~CSmsSync()
		{
		Cancel();
		iTimer.Close();
		delete iMtm;
		delete iRegistry;
		delete iSession;
		iFs.Close();
		ss_close();
		}
	void HandleSessionEventL(TMsvSessionEvent aEvent, TAny*, TAny*, TAny*)
		{
		switch (aEvent)
			{
		case EMsvServerReady: iReady = ETrue; break;
		case EMsvEntriesCreated: case EMsvEntriesChanged: iDirty = ETrue; break;   // the next cycle runs at once
		case EMsvCloseSession: case EMsvServerTerminated: CActiveScheduler::Stop(); break;
		default: break;
			}
		}
	void RunL()
		{
		TRAP_IGNORE(CycleL());
		Wait();
		}
	void DoCancel() { iTimer.Cancel(); }
	void Start()
		{
		LoadState();
		Wait();
		}

private:
	CSmsSync() : CActive(EPriorityStandard) { CActiveScheduler::Add(this); }
	void ConstructL()
		{
		User::LeaveIfError(iFs.Connect());
		iFs.MkDirAll(KStateDir);
		User::LeaveIfError(iTimer.CreateLocal());
		iSession = CMsvSession::OpenSyncL(*this);   // blocks until the message server is up (it is started by the system)
		iReady = ETrue;
		iRegistry = CClientMtmRegistry::NewL(*iSession);
		iMtm = static_cast<CSmsClientMtm*>(iRegistry->NewMtmL(KUidMsgTypeSMS));
		CMsvEntry* root = iSession->GetEntryL(KMsvRootIndexEntryId);
		CleanupStack::PushL(root);
		TSmsUtilities::ServiceIdL(*root, iService);   // leaves KErrNotFound until the SMS service exists: the first cycle retries
		CleanupStack::PopAndDestroy(root);
		}
	void Wait()
		{
		iTimer.After(iStatus, (iDirty ? 1 : (iUnconfigured ? KIdleSeconds : KCycleSeconds)) * 1000000);
		iDirty = EFalse;
		SetActive();
		}

	// ---- state ----
	void LoadState()
		{
		RFile f;
		if (f.Open(iFs, KStateFile, EFileRead) != KErrNone)
			return;
		TBuf8<512> b;
		f.Read(b);
		f.Close();
		b.Append(TChar(0));
		const char* p = (const char*)b.Ptr();
		unsigned in = 0, out = 0;
		sscanf(p, "in=%u\nout=%u\n", &in, &out);
		iCursorIn = in;
		iCursorOut = out;
		const char* a = strstr(p, "ack=");
		while (a && iAcks.Count() < KMaxAcks)
			{
			long long o = 0;
			unsigned id = 0;
			if (sscanf(a, "ack=%lld,%u", &o, &id) == 2)
				{
				TAck ack = { o, (TMsvId)id };
				iAcks.Append(ack);
				}
			a = strstr(a + 4, "ack=");
			}
		}
	void SaveState()
		{
		RFile f;
		if (f.Replace(iFs, KStateFile, EFileWrite) != KErrNone)
			return;
		char buf[400];
		int n = sprintf(buf, "in=%u\nout=%u\n", (unsigned)iCursorIn, (unsigned)iCursorOut);
		for (TInt i = 0; i < iAcks.Count() && n < 360; i++)
			n += sprintf(buf + n, "ack=%lld,%u\n", (long long)iAcks[i].iOutId, (unsigned)iAcks[i].iId);
		TPtrC8 d((const TUint8*)buf, n);
		f.Write(d);
		f.Close();
		}

	// ---- one cycle: collect new messages, sync, send what the web app queued, find out how the earlier sends went ----
	void CycleL()
		{
		if (!iReady || iService == KMsvNullIndexEntryId)
			{
			CMsvEntry* root = iSession->GetEntryL(KMsvRootIndexEntryId);
			CleanupStack::PushL(root);
			TRAPD(r, TSmsUtilities::ServiceIdL(*root, iService));
			CleanupStack::PopAndDestroy(root);
			if (r != KErrNone)
				return;
			}
		CollectAcksL();
		phc_sms_msg batch[KBatch];
		HBufC8* keep[KBatch * 4];          // the UTF-8 strings the batch points into
		TInt nkeep = 0;
		char ids[KBatch][16];
		TInt n = 0;
		TMsvId maxIn = iCursorIn, maxOut = iCursorOut;
		for (TInt dir = 0; dir < 2 && n < KBatch; dir++)
			{
			const TMsvId folder = dir == 0 ? KMsvGlobalInBoxIndexEntryId : KMsvSentEntryId;
			CMsvEntry* cf = iSession->GetEntryL(folder);
			CleanupStack::PushL(cf);
			CMsvEntrySelection* sel = cf->ChildrenWithMtmL(KUidMsgTypeSMS);
			CleanupStack::PushL(sel);
			for (TInt i = 0; i < sel->Count() && n < KBatch; i++)
				{
				const TMsvId id = (*sel)[i];
				if (id <= (dir == 0 ? iCursorIn : iCursorOut))
					continue;
				iMtm->SwitchCurrentEntryL(id);
				iMtm->LoadMessageL();
				const TMsvEntry e = iMtm->Entry().Entry();
				if (dir == 1 && e.SendingState() != KMsvSendStateSent)
					continue;
				TPtrC num = dir == 0 ? iMtm->SmsHeader().FromAddress() : TPtrC(KNullDesC);
				if (dir == 1 && iMtm->SmsHeader().Recipients().Count() > 0)
					num.Set(iMtm->SmsHeader().Recipients()[0]->Address());
				if (num.Length() == 0)
					num.Set(e.iDetails);
				if (num.Length() == 0)
					num.Set(_L("unknown"));     // the server drops a message without a number
				HBufC* body = HBufC::NewLC(iMtm->Body().DocumentLength() + 1);
				TPtr bp(body->Des());
				iMtm->Body().Extract(bp, 0);
				LOG("[phesmssync] %s message %u from/to %d chars, text %d chars", dir == 0 ? "incoming" : "sent", (unsigned)id, num.Length(), iMtm->Body().DocumentLength());
				keep[nkeep++] = Utf8L(num);
				keep[nkeep++] = Utf8L(bp);
				CleanupStack::PopAndDestroy(body);
				sprintf(ids[n], "%u", (unsigned)id);
				batch[n].id = ids[n];
				batch[n].number = (const char*)keep[nkeep - 2]->Ptr();
				batch[n].dir = dir == 0 ? "in" : "out";
				batch[n].body = (const char*)keep[nkeep - 1]->Ptr();
				batch[n].status = dir == 0 ? "" : "sent";
				batch[n].ts = UnixTime(e.iDate);
				if (dir == 0 && id > maxIn) maxIn = id;
				if (dir == 1 && id > maxOut) maxOut = id;
				n++;
				}
			CleanupStack::PopAndDestroy(2);   // sel, cf
			}
		// acks for results found earlier
		phc_sms_ack acks[KMaxAcks];
		TInt na = 0;
		for (TInt i = 0; i < iDone.Count() && na < KMaxAcks; i++)
			acks[na++] = iDone[i];
		phc_sms_pending pend[KMaxPending];
		int np = 0, accepted = 0;
		const int rc = ss_sync(batch, n, acks, na, pend, KMaxPending, &np, &accepted);
		for (TInt i = 0; i < nkeep; i++)
			delete keep[i];
		iUnconfigured = (rc <= -11 && rc >= -17);       // not paired yet: the service sleeps until the pairing screen has written the ini
		if (rc != 0)
			{
			if (!iUnconfigured || !iLoggedIdle)
				LOG("[phesmssync] sync failed (%d): %s; %d messages stay queued", rc, iUnconfigured ? "not paired (no usable C:\data\phecloud.ini), idle" : "offline, will retry", n);
			iLoggedIdle = iUnconfigured;
			return;                       // cursor unchanged: the same messages go out again next cycle
			}
		LOG("[phesmssync] synced: %d uploaded (%d new), %d acks, %d pending", n, accepted, na, np);
		iCursorIn = maxIn;
		iCursorOut = maxOut;
		iDone.Reset();
		for (TInt i = 0; i < np; i++)
			SendOneL(pend[i]);
		SaveState();
		if (n == KBatch)
			iDirty = ETrue;               // more waiting: do not sleep a whole cycle
		}

	// results of messages we sent: the message server marks the entry Sent / Failed
	void CollectAcksL()
		{
		for (TInt i = iAcks.Count() - 1; i >= 0; i--)
			{
			TMsvEntry e;
			TMsvId svc;
			if (iSession->GetEntry(iAcks[i].iId, svc, e) != KErrNone)
				{
				iAcks.Remove(i);          // gone (deleted by the user): nothing to report
				continue;
				}
			const TInt st = e.SendingState();
			if (st == KMsvSendStateSent || st == KMsvSendStateFailed)
				{
				phc_sms_ack a;
				memset(&a, 0, sizeof a);
				a.out_id = iAcks[i].iOutId;
				snprintf(iIdText[iDone.Count() % KMaxAcks], 16, "%u", (unsigned)iAcks[i].iId);
				a.id = iIdText[iDone.Count() % KMaxAcks];
				a.status = st == KMsvSendStateSent ? "sent" : "failed";
				a.error = st == KMsvSendStateFailed ? "the phone could not send it" : "";
				iDone.Append(a);
				iAcks.Remove(i);
				}
			}
		}

	void SendOneL(const phc_sms_pending& aP)
		{
		for (TInt i = 0; i < iAcks.Count(); i++)      // already built and scheduled: the web app's queue still lists it until our report arrives
			if (iAcks[i].iOutId == aP.out_id)
				return;
		for (TInt i = 0; i < iDone.Count(); i++)
			if (iDone[i].out_id == aP.out_id)
				return;
		HBufC* num = HBufC::NewLC(strlen(aP.number) + 1);
		HBufC* txt = HBufC::NewLC(strlen(aP.body) + 1);
		TPtr np(num->Des()), tp(txt->Des());
		CnvUtfConverter::ConvertToUnicodeFromUtf8(np, TPtrC8((const TUint8*)aP.number));
		CnvUtfConverter::ConvertToUnicodeFromUtf8(tp, TPtrC8((const TUint8*)aP.body));
		TRAPD(r, BuildAndScheduleL(aP.out_id, *num, *txt));
		if (r != KErrNone)
			{
			LOG("[phesmssync] could not queue out_id %d: %d", (int)aP.out_id, r);
			phc_sms_ack a;
			memset(&a, 0, sizeof a);
			a.out_id = aP.out_id;
			a.status = "failed";
			a.error = "the phone could not queue the message";
			iDone.Append(a);
			}
		CleanupStack::PopAndDestroy(2);
		}

	void BuildAndScheduleL(TInt64 aOutId, const TDesC& aNumber, const TDesC& aText)
		{
		iMtm->SwitchCurrentEntryL(KMsvDraftEntryIdValue);
		iMtm->CreateMessageL(iService);
		iMtm->Body().InsertL(0, aText);
		iMtm->AddAddresseeL(aNumber);
		TMsvEntry e = iMtm->Entry().Entry();
		e.SetInPreparation(EFalse);
		e.SetSendingState(KMsvSendStateWaiting);
		e.SetVisible(ETrue);
		e.iDate.UniversalTime();
		e.iDetails.Set(aNumber);
		iMtm->Entry().ChangeL(e);
		iMtm->SaveMessageL();
		const TMsvId id = iMtm->Entry().EntryId();
		// Drafts -> Outbox, then ask the SMS server MTM to schedule the send
		CMsvEntry* drafts = iSession->GetEntryL(KMsvDraftEntryIdValue);
		CleanupStack::PushL(drafts);
		CMsvOperationWait* w = CMsvOperationWait::NewLC();
		w->iStatus = KRequestPending;
		CMsvOperation* op = drafts->MoveL(id, KMsvGlobalOutBoxIndexEntryId, w->iStatus);
		CleanupStack::PushL(op);
		w->Start();
		CActiveScheduler::Start();
		User::LeaveIfError(op->iStatus.Int());
		CleanupStack::PopAndDestroy(3);
		CMsvEntrySelection* sel = new (ELeave) CMsvEntrySelection;
		CleanupStack::PushL(sel);
		sel->AppendL(id);
		iMtm->SwitchCurrentEntryL(KMsvGlobalOutBoxIndexEntryId);
		CMsvOperationWait* w2 = CMsvOperationWait::NewLC();
		w2->iStatus = KRequestPending;
		TBuf8<4> noParam;
		CMsvOperation* op2 = iMtm->InvokeAsyncFunctionL(ESmsMtmCommandScheduleCopy, *sel, noParam, w2->iStatus);
		CleanupStack::PushL(op2);
		w2->Start();
		CActiveScheduler::Start();
		User::LeaveIfError(op2->iStatus.Int());
		CleanupStack::PopAndDestroy(3);
		TAck a = { aOutId, id };
		iAcks.Append(a);
		LOG("[phesmssync] queued out_id %d as message %u", (int)aOutId, (unsigned)id);
		}

	static HBufC8* Utf8L(const TDesC& aDes)
		{
		HBufC8* b = CnvUtfConverter::ConvertFromUnicodeToUtf8L(aDes);
		CleanupStack::PushL(b);
		b = b->ReAllocL(b->Length() + 1);   // NUL terminated for the C core
		CleanupStack::Pop();
		TPtr8 p = b->Des();
		p.Append(TChar(0));
		return b;
		}
	static TInt64 UnixTime(const TTime& aT)
		{
		TTime epoch(TDateTime(1970, EJanuary, 0, 0, 0, 0, 0));
		return aT.MicroSecondsFrom(epoch).Int64() / 1000000;
		}

	RFs iFs;
	RTimer iTimer;
	CMsvSession* iSession = NULL;
	CClientMtmRegistry* iRegistry = NULL;
	CSmsClientMtm* iMtm = NULL;
	TMsvId iService = KMsvNullIndexEntryId;
	TMsvId iCursorIn = 0, iCursorOut = 0;
	TBool iReady = EFalse, iDirty = EFalse, iUnconfigured = EFalse, iLoggedIdle = EFalse;
	RArray<TAck> iAcks;           // sent by us, result not known yet
	RArray<phc_sms_ack> iDone;    // result known, not reported yet
	char iIdText[KMaxAcks][16];
	};

static void RunL()
	{
	CActiveScheduler* sched = new (ELeave) CActiveScheduler;
	CleanupStack::PushL(sched);
	CActiveScheduler::Install(sched);
	CSmsSync* s = CSmsSync::NewL();
	CleanupStack::PushL(s);
	s->Start();
	LOG("[phesmssync] running");
	CActiveScheduler::Start();
	CleanupStack::PopAndDestroy(2);
	}

// An Open C executable: libcrt0 (P.I.P.S.) provides E32Main, initialises the C library (stdio, sockets), creates the cleanup stack and calls main.
int main(int, char**)
	{
	TRAPD(r, RunL());
	if (r != KErrNone)
		LOG("[phesmssync] stopped: %d", r);
	return r;
	}
