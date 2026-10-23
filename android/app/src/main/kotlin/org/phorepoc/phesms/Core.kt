// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package org.phorepoc.phesms

import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

// The platform-free core of the app: what is read, queued, uploaded, sent and acknowledged. Android classes (content provider,
// SmsManager, preferences, HTTP) sit behind the small interfaces below, so this file is tested on the JVM (CoreTest).
// Wire format: phecloud docs/sms.md (POST /sms/sync).

/** One message as the hub stores it. [id] is this phone's own id ("in:12", "out:40", "hub:7" for a hub-queued message). */
data class Sms(val id: String, val number: String, val dir: String, val body: String, val ts: Long, val status: String? = null) {
    fun toJson(): JSONObject = JSONObject().put("id", id).put("number", number).put("dir", dir).put("body", body).put("ts", ts)
        .also { if (status != null) it.put("status", status) }

    companion object {
        fun fromJson(o: JSONObject) = Sms(o.getString("id"), o.getString("number"), o.getString("dir"), o.getString("body"), o.getLong("ts"),
            if (o.has("status")) o.getString("status") else null)
    }
}

/** A report on a message the hub asked this phone to send. status: sent | delivered | failed. */
data class Ack(val outId: Long, val id: String, val status: String, val error: String? = null) {
    fun toJson(): JSONObject = JSONObject().put("out_id", outId).put("id", id).put("status", status)
        .also { if (error != null) it.put("error", error) }

    companion object {
        fun fromJson(o: JSONObject) = Ack(o.getLong("out_id"), o.getString("id"), o.getString("status"), if (o.has("error")) o.getString("error") else null)
    }
}

/** A message the web app queued for this phone. */
data class Pending(val outId: Long, val number: String, val body: String)

data class SyncReply(val accepted: Int, val pending: List<Pending>)

/** A row of the SMS provider: box is "in" (inbox) or "out" (sent). */
data class Row(val box: String, val rowId: Long, val number: String, val body: String, val ts: Long)

interface SmsSource {
    /** Rows of one box with _id greater than [after], oldest first, at most [limit]. */
    fun rows(box: String, after: Long, limit: Int): List<Row>
}

interface HubTransport {
    /** One POST /sms/sync. Throws on any failure (no network, bad status): the caller keeps everything queued. */
    fun sync(messages: List<Sms>, acks: List<Ack>): SyncReply
}

interface SmsSender {
    /** Starts sending [body] to [number] for the hub's [outId]; the result comes back through [Engine.onSendResult]. */
    fun send(outId: Long, number: String, body: String)
}

/** Durable state: survives the process (SharedPreferences + a file on the phone; a map in the tests). */
interface State {
    fun getLong(key: String, def: Long): Long
    fun putLong(key: String, v: Long)
    fun getString(key: String, def: String): String
    fun putString(key: String, v: String)
}

/** The per-sender "do not sync" list (one-time codes, banks): a number, a short code or an alphanumeric sender id per entry. */
class Excluder(entries: Collection<String>) {
    private val digits = HashSet<String>()
    private val names = HashSet<String>()

    init {
        for (e in entries) {
            val t = e.trim()
            if (t.isEmpty()) continue
            val d = tail(t)
            if (d.isNotEmpty() && t.any { it.isDigit() } && t.none { it.isLetter() }) digits.add(d) else names.add(t.lowercase())
        }
    }

    fun excluded(number: String): Boolean {
        val t = number.trim()
        if (t.any { it.isLetter() }) return names.contains(t.lowercase())
        val d = tail(t)
        return d.isNotEmpty() && digits.contains(d)
    }

    companion object {
        /** The last 9 digits: "+49 171 5550100", "0049 171 5550100" and "171 5550100" are one sender. */
        fun tail(n: String): String = n.filter { it.isDigit() }.takeLast(9)
    }
}

/** The hub request signature: X-Phe-Auth = "<dev>.<ts>.<hex HMAC-SHA256(key, "<dev>.<ts>.<METHOD> <path>")>". */
object Signer {
    fun header(dev: String, key: ByteArray, method: String, path: String, ts: Long): String {
        val m = Mac.getInstance("HmacSHA256")
        m.init(SecretKeySpec(key, "HmacSHA256"))
        val sig = m.doFinal("$dev.$ts.$method $path".toByteArray(Charsets.UTF_8))
        return "$dev.$ts." + hex(sig)
    }

    fun hex(b: ByteArray): String = b.joinToString("") { "%02x".format(it.toInt() and 0xff) }
    fun unhex(s: String): ByteArray = ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }
    fun sha256Hex(b: ByteArray): String = hex(MessageDigest.getInstance("SHA-256").digest(b))
}

/**
 * The sync logic. Order of a run: read new rows past the high-water marks (minus the excluded senders) into the durable upload
 * queue, upload the queue in batches of at most 500 together with the queued acks, drop what the hub confirmed, start sending what
 * the hub's reply asks for (each hub out_id once). Every step is repeatable: a failed upload leaves the queue as it was and the hub
 * ignores an id it already has.
 */
class Engine(
    private val source: SmsSource, private val hub: HubTransport, private val sender: SmsSender, private val state: State,
    private val excluded: () -> Collection<String>,
) {
    companion object {
        const val BATCH = 500
        const val READ_CHUNK = 200
        const val KEY_MARK_IN = "mark_in"
        const val KEY_MARK_OUT = "mark_out"
        const val KEY_QUEUE = "queue"          // JSON array of Sms: read, not yet confirmed by the hub
        const val KEY_ACKS = "acks"            // JSON array of Ack: not yet delivered to the hub
        const val KEY_SEEN = "seen_out"        // JSON array of hub out_ids already handed to SmsManager
        const val KEY_FIRST = "first_run_done"
    }

    var lastError: String? = null
        private set
    private val ackLock = Any()           // send reports arrive on the main thread while a run holds the network

    /** On the very first run only the last [firstRunLimit] rows per box are taken (an inbox of years is not uploaded by surprise). */
    var firstRunLimit = 500

    private fun queue(): MutableList<Sms> = readArr(KEY_QUEUE) { Sms.fromJson(it) }
    private fun acks(): MutableList<Ack> = readArr(KEY_ACKS) { Ack.fromJson(it) }
    private fun <T> readArr(key: String, f: (JSONObject) -> T): MutableList<T> {
        val a = JSONArray(state.getString(key, "[]"))
        return MutableList(a.length()) { f(a.getJSONObject(it)) }
    }

    private fun saveQueue(q: List<Sms>) = state.putString(KEY_QUEUE, JSONArray(q.map { it.toJson() }).toString())
    private fun saveAcks(a: List<Ack>) = state.putString(KEY_ACKS, JSONArray(a.map { it.toJson() }).toString())
    private fun seen(): MutableSet<Long> = JSONArray(state.getString(KEY_SEEN, "[]")).let { a -> MutableList(a.length()) { a.getLong(it) }.toMutableSet() }
    private fun saveSeen(s: Set<Long>) = state.putString(KEY_SEEN, JSONArray(s.toList().takeLast(2000)).toString())

    fun queued(): Int = queue().size
    fun queuedAcks(): Int = acks().size

    /** Reads new rows into the queue. Returns how many were queued. */
    fun collect(): Int {
        val ex = Excluder(excluded())
        val q = queue()
        var n = 0
        for (box in listOf("in", "out")) {
            val key = if (box == "in") KEY_MARK_IN else KEY_MARK_OUT
            var mark = state.getLong(key, -1)
            if (mark < 0) {                                  // first run: start near the end of the box
                val all = ArrayList<Row>()
                var after = 0L
                while (true) {
                    val r = source.rows(box, after, READ_CHUNK)
                    if (r.isEmpty()) break
                    all.addAll(r); after = r.last().rowId
                }
                mark = if (all.size > firstRunLimit) all[all.size - firstRunLimit - 1].rowId else 0
            }
            while (true) {
                val rows = source.rows(box, mark, READ_CHUNK)
                if (rows.isEmpty()) break
                for (r in rows) {
                    mark = r.rowId
                    if (ex.excluded(r.number)) continue
                    q.add(Sms("$box:${r.rowId}", r.number, box, r.body, r.ts))
                    n++
                }
            }
            state.putLong(key, mark)                         // the mark advances over excluded rows too: they are never re-read
        }
        saveQueue(q)
        return n
    }

    /** One full run. Returns true when the hub was reached. */
    fun run(): Boolean {
        try {
            collect()
        } catch (e: Exception) {                             // no READ_SMS yet, a provider hiccup: still deliver acks and pending
            lastError = "read: ${e.message}"
        }
        val seenSet = seen()
        var reached = false
        while (true) {
            val q = queue()
            val a = synchronized(ackLock) { acks() }
            val batch = q.take(BATCH)
            val ackBatch = a.take(BATCH)
            val reply = try {
                hub.sync(batch, ackBatch)
            } catch (e: Exception) {
                lastError = "hub: ${e.message}"
                return reached
            }
            reached = true; lastError = null
            saveQueue(q.drop(batch.size))
            synchronized(ackLock) { saveAcks(acks().drop(ackBatch.size)) }     // acks that arrived during the upload are behind the sent ones
            for (p in reply.pending) {
                if (!seenSet.add(p.outId)) continue          // handed over already (the hub keeps it queued until our ack arrives)
                saveSeen(seenSet)
                try {
                    sender.send(p.outId, p.number, p.body)
                } catch (e: Exception) {
                    onSendResult(p.outId, "failed", e.message ?: "send failed")
                }
            }
            if (q.size <= BATCH && a.size <= BATCH) break
        }
        return reached
    }

    /** SmsManager's sent / delivered / failure reports for the hub's [outId]. The ack goes out with the next run. */
    fun onSendResult(outId: Long, status: String, error: String? = null) = synchronized(ackLock) {
        val a = acks()
        a.removeAll { it.outId == outId && it.status == status }
        a.add(Ack(outId, "hub:$outId", status, error))
        saveAcks(a)
    }
}
