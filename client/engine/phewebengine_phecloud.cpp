// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phewebengine_phecloud.cpp -- the ECom shell of the Phecloud web-engine package. Interface 0xE7016B01
// (phewebengine_ecom.h), implementation 0xE7016B21. The engine itself is plain C (phengine_phc.c) over the phc client core.
#include <e32std.h>
#include <e32base.h>
#include <e32debug.h>
#include <f32file.h>
#include <e32math.h>
#include <stddef.h>
#include <ecom/implementationproxy.h>
#include <phorepoc/phewebengine_ecom.h>

extern "C" const PheWebEngineApi* PhePhecloudGetApi(unsigned aWanted);

// trace: the serial log and C:\data\phecloud_trace.txt
extern "C" void phc_log(const char* aMsg)
	{
	TBuf<240> w;
	TPtrC8 p(reinterpret_cast<const TUint8*>(aMsg), Min((TInt)User::StringLength(reinterpret_cast<const TUint8*>(aMsg)), 239));
	w.Copy(p);
	RDebug::Printf("PH: [phecloud] %s", aMsg);
	RFs fs;
	if (fs.Connect() != KErrNone)
		return;
	RFile f;
	TInt r = f.Open(fs, _L("C:\data\phecloud_trace.txt"), EFileWrite | EFileShareAny);
	if (r == KErrNotFound)
		r = f.Create(fs, _L("C:\data\phecloud_trace.txt"), EFileWrite | EFileShareAny);
	if (r == KErrNone)
		{
		TInt pos = 0;
		f.Seek(ESeekEnd, pos);
		TBuf8<260> line;
		line.Format(_L8("%d "), User::NTickCount());
		line.Append(p);
		line.Append(_L8("\r\n"));
		f.Write(line);
		f.Close();
		}
	fs.Close();
	}

// heap numbers of the calling thread's heap (the view thread allocates the phc object, tiles, TLS state and the view buffer)
extern "C" void phc_mem(const char* aTag)
	{
	TInt bytes = 0, biggest = 0;
	const TInt cells = User::AllocSize(bytes);        // returns the cell count, sets the byte total
	const TInt freeBytes = User::Available(biggest);
	TBuf8<120> b;
	b.Format(_L8("mem %s: allocated %d B in %d cells, free %d B (largest %d B)"), aTag, bytes, cells, freeBytes, biggest);
	b.Append(TChar(0));
	phc_log((const char*)b.Ptr());
	}

// mbedTLS's entropy source: the kernel's random number generator (MBEDTLS_ENTROPY_HARDWARE_ALT)
extern "C" int mbedtls_hardware_poll(void* /*aData*/, unsigned char* aOutput, size_t aLen, size_t* aOLen)
	{
	TPtr8 out(aOutput, 0, (TInt)aLen);
	out.SetLength((TInt)aLen);
	Math::Random(out);
	*aOLen = aLen;
	return 0;
	}

#if defined(MBEDTLS_PLATFORM_MS_TIME_ALT)
// TLS 1.3 ticket ages on a build whose libc has no clock for mbedTLS: milliseconds of universal time
extern "C" long long mbedtls_ms_time(void)
	{
	TTime now;
	now.UniversalTime();
	return now.Int64() / 1000;
	}
#endif

namespace
{
class CPhecloudEngine : public CPheWebEnginePlugin
	{
public:
	static CPhecloudEngine* NewL() { return new (ELeave) CPhecloudEngine; }
	const PheWebEngineApi* Api() { return PhePhecloudGetApi(PHE_WEBENGINE_VERSION); }
	};

const TImplementationProxy KImplementationTable[] =
	{
	IMPLEMENTATION_PROXY_ENTRY(0xE7016B21, CPhecloudEngine::NewL)
	};
}

EXPORT_C const TImplementationProxy* ImplementationGroupProxy(TInt& aTableCount)
	{
	aTableCount = sizeof(KImplementationTable) / sizeof(TImplementationProxy);
	return KImplementationTable;
	}
