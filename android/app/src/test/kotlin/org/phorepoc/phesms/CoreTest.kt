// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package org.phorepoc.phesms

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MapState : State {
    val m = HashMap<String, Any>()
    override fun getLong(key: String, def: Long) = (m[key] as Long?) ?: def
    override fun putLong(key: String, v: Long) { m[key] = v }
    override fun getString(key: String, def: String) = (m[key] as String?) ?: def
    override fun putString(key: String, v: String) { m[key] = v }
}

class FakeSource : SmsSource {
    val rows = ArrayList<Row>()
    var fail = false
    fun add(box: String, id: Long, number: String, body: String = "text $id", ts: Long = 1000 + id) { rows.add(Row(box, id, number, body, ts)) }
    override fun rows(box: String, after: Long, limit: Int): List<Row> {
        if (fail) throw SecurityException("READ_SMS denied")
        return rows.filter { it.box == box && it.rowId > after }.sortedBy { it.rowId }.take(limit)
    }
}

class FakeHub : HubTransport {
    var down = false
    val stored = LinkedHashMap<String, Sms>()          // like the hub: (device, id) -> message, a repeat changes nothing
    val calls = ArrayList<Pair<List<Sms>, List<Ack>>>()
    val outbound = ArrayList<Pending>()                // the web app's queue; leaves it when acked "sent" or "failed"
    val acked = ArrayList<Ack>()
    override fun sync(messages: List<Sms>, acks: List<Ack>): SyncReply {
        if (down) throw java.io.IOException("no route to host")
        calls.add(messages to acks)
        var n = 0
        for (m in messages) if (stored.putIfAbsent(m.id, m) == null) n++
        for (a in acks) { acked.add(a); if (a.status != "delivered") outbound.removeAll { it.outId == a.outId } }
        return SyncReply(n, ArrayList(outbound))
    }
}

class FakeSender : SmsSender {
    val sent = ArrayList<Triple<Long, String, String>>()
    var boom = false
    override fun send(outId: Long, number: String, body: String) {
        if (boom) throw IllegalStateException("no SIM")
        sent.add(Triple(outId, number, body))
    }
}

class CoreTest {
    private fun rig(excl: List<String> = emptyList()): List<Any> {
        val src = FakeSource(); val hub = FakeHub(); val snd = FakeSender(); val st = MapState()
        return listOf(src, hub, snd, st, Engine(src, hub, snd, st) { excl })
    }
    private val List<Any>.src get() = this[0] as FakeSource
    private val List<Any>.hub get() = this[1] as FakeHub
    private val List<Any>.snd get() = this[2] as FakeSender
    private val List<Any>.st get() = this[3] as MapState
    private val List<Any>.e get() = this[4] as Engine

    @Test fun uploadsInboxAndSentOnceWithTheProvidersIds() {
        val r = rig()
        r.src.add("in", 5, "+49 171 5550100"); r.src.add("out", 9, "+49 171 5550100", "hello"); r.src.add("in", 6, "BANK")
        assertTrue(r.e.run())
        assertEquals(setOf("in:5", "in:6", "out:9"), r.hub.stored.keys)
        assertEquals("out", r.hub.stored["out:9"]!!.dir)
        assertEquals(0, r.e.queued())
        val calls = r.hub.calls.size
        assertTrue(r.e.run())
        assertEquals("nothing new: the queue stays empty, no message is re-read", 0, r.hub.calls.last().first.size)
        assertEquals(3, r.hub.stored.size)
        assertTrue(r.hub.calls.size > calls)
    }

    @Test fun offlineQueueSurvivesAndIsSentLater() {
        val r = rig()
        r.src.add("in", 1, "111111111"); r.src.add("in", 2, "222222222")
        r.hub.down = true
        assertFalse(r.e.run())
        assertEquals("read and queued, not lost", 2, r.e.queued())
        assertTrue(r.e.lastError!!.startsWith("hub:"))
        r.src.add("in", 3, "333333333")                       // more arrive while offline
        assertFalse(r.e.run())
        assertEquals(3, r.e.queued())
        // a new Engine on the same state (the process died) still has the queue
        val e2 = Engine(r.src, r.hub, r.snd, r.st) { emptyList() }
        r.hub.down = false
        assertTrue(e2.run())
        assertEquals(setOf("in:1", "in:2", "in:3"), r.hub.stored.keys)
        assertEquals(0, e2.queued())
    }

    @Test fun batchesOfAtMost500() {
        val r = rig()
        for (i in 1..1200L) r.src.add("in", i, "4915555%04d".format(i))
        r.e.firstRunLimit = 5000
        assertTrue(r.e.run())
        assertEquals(1200, r.hub.stored.size)
        assertTrue(r.hub.calls.all { it.first.size <= 500 })
        assertEquals(listOf(500, 500, 200), r.hub.calls.map { it.first.size })
    }

    @Test fun firstRunTakesOnlyTheNewestRows() {
        val r = rig()
        for (i in 1..30L) r.src.add("in", i, "4915555%04d".format(i))
        r.e.firstRunLimit = 10
        r.e.run()
        assertEquals((21..30).map { "in:$it" }.toSet(), r.hub.stored.keys)
    }

    @Test fun excludedSendersAreNeverUploadedAndNeverReRead() {
        val r = rig(listOf("bank", "+49 171 5550100", "22395"))
        r.src.add("in", 1, "BANK"); r.src.add("in", 2, "0171 5550100"); r.src.add("in", 3, "22395")
        r.src.add("in", 4, "+49 30 998877"); r.src.add("out", 5, "00491715550100", "reply to the excluded one")
        r.e.run()
        assertEquals(setOf("in:4"), r.hub.stored.keys)
        assertEquals("the mark moved over the excluded rows", 4L, r.st.getLong(Engine.KEY_MARK_IN, -1))
        // the list changes later: old excluded rows are not uploaded retroactively
        val e2 = Engine(r.src, r.hub, r.snd, r.st) { emptyList() }
        e2.run()
        assertEquals(setOf("in:4"), r.hub.stored.keys)
    }

    @Test fun excluderMatching() {
        val x = Excluder(listOf("+49 171 5550100", "BANK", " 22395 "))
        assertTrue(x.excluded("0171 5550100")); assertTrue(x.excluded("+491715550100")); assertTrue(x.excluded("bank"))
        assertTrue(x.excluded("22395")); assertFalse(x.excluded("22396")); assertFalse(x.excluded("BANK2")); assertFalse(x.excluded("+49 171 5550101"))
        assertFalse(Excluder(listOf("")).excluded(""))
    }

    @Test fun outboundQueueIsSentOnceAndReportedBack() {
        val r = rig()
        r.hub.outbound.add(Pending(12, "0171 5550100", "text to send"))
        r.e.run()
        assertEquals(listOf(Triple(12L, "0171 5550100", "text to send")), r.snd.sent)
        // the hub keeps it queued until our ack: the next run must not send it again
        r.e.run()
        assertEquals(1, r.snd.sent.size)
        // SmsManager reports: sent, then delivered
        r.e.onSendResult(12, "sent")
        assertEquals(1, r.e.queuedAcks())
        r.e.run()
        assertEquals(Ack(12, "hub:12", "sent"), r.hub.acked.last())
        assertEquals(0, r.e.queuedAcks())
        r.e.onSendResult(12, "delivered")
        r.e.run()
        assertEquals("delivered", r.hub.acked.last().status)
        assertEquals(1, r.snd.sent.size)
    }

    @Test fun acksWaitOfflineAndSurviveARestart() {
        val r = rig()
        r.hub.outbound.add(Pending(7, "111111111", "x"))
        r.e.run()
        r.hub.down = true
        r.e.onSendResult(7, "failed", "no signal")
        assertFalse(r.e.run())
        assertEquals(1, r.e.queuedAcks())
        val e2 = Engine(r.src, r.hub, r.snd, r.st) { emptyList() }
        r.hub.down = false
        e2.run()
        assertEquals(Ack(7, "hub:7", "failed", "no signal"), r.hub.acked.last())
        assertEquals(0, e2.queuedAcks())
        assertEquals("a failed message is not retried by the phone", 1, r.snd.sent.size)
    }

    @Test fun senderCrashBecomesAFailedAck() {
        val r = rig()
        r.snd.boom = true
        r.hub.outbound.add(Pending(3, "111111111", "x"))
        r.e.run()
        r.e.run()
        assertEquals("failed", r.hub.acked.last().status)
        assertEquals("no SIM", r.hub.acked.last().error)
    }

    @Test fun aProviderFailureDoesNotStopAcksAndPending() {
        val r = rig()
        r.src.fail = true                                       // READ_SMS not granted yet
        r.hub.outbound.add(Pending(1, "111111111", "x"))
        assertTrue(r.e.run())
        assertEquals(1, r.snd.sent.size)
        assertTrue(r.e.lastError == null || r.e.lastError!!.startsWith("read:") || r.e.lastError == null)
    }

    @Test fun signatureMatchesTheHubsRule() {
        // HMAC-SHA256("key", "dev.1760000000.POST /sms/sync") -- computed independently with openssl
        val key = "key".toByteArray()
        val h = Signer.header("dev", key, "POST", "/sms/sync", 1760000000)
        assertTrue(h.startsWith("dev.1760000000."))
        assertEquals(64, h.substringAfterLast('.').length)
        assertEquals("5eaeb6b2426557fc210588b28cd9d769dac7a2d5c3e1603e1ec7c9dfad096f1e", h.substringAfterLast('.'))
        assertEquals(h, Signer.header("dev", key, "POST", "/sms/sync", 1760000000))
        assertTrue(h != Signer.header("dev", key, "GET", "/sms/sync", 1760000000))
        assertEquals("00ff10", Signer.hex(Signer.unhex("00ff10")))
        assertEquals("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", Signer.sha256Hex(ByteArray(0)))
    }
}
