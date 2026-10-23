// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package org.phorepoc.phesms

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import android.telephony.SmsManager
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.security.SecureRandom
import java.security.cert.X509Certificate
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager

// The Android side: the SMS provider, SmsManager, preferences and HTTP. Nothing here has logic worth a unit test; the logic is in Core.kt.

/** Settings and the engine's durable state, in one private preferences file. */
class Prefs(ctx: Context) : State {
    private val p: SharedPreferences = ctx.getSharedPreferences("phesms", Context.MODE_PRIVATE)
    override fun getLong(key: String, def: Long) = p.getLong(key, def)
    override fun putLong(key: String, v: Long) { p.edit().putLong(key, v).apply() }
    override fun getString(key: String, def: String) = p.getString(key, def) ?: def
    override fun putString(key: String, v: String) { p.edit().putString(key, v).apply() }

    var hub: String get() = getString("hub", ""); set(v) = putString("hub", v)
    var pin: String get() = getString("pin", ""); set(v) = putString("pin", v.lowercase().filter { it.isLetterOrDigit() })
    var dev: String get() = getString("dev", ""); set(v) = putString("dev", v)
    var key: String get() = getString("key", ""); set(v) = putString("key", v)
    var exclude: String get() = getString("exclude", ""); set(v) = putString("exclude", v)   // one entry per line
    val paired get() = hub.isNotEmpty() && dev.isNotEmpty() && key.isNotEmpty()
    fun excludeList(): List<String> = exclude.lines().map { it.trim() }.filter { it.isNotEmpty() }
    fun status(): String = getString("status", "")
    fun setStatus(s: String) = putString("status", s)
}

/** Reads the inbox (content://sms/inbox) and the sent box (content://sms/sent). */
class ProviderSource(private val ctx: Context) : SmsSource {
    override fun rows(box: String, after: Long, limit: Int): List<Row> {
        val uri = Uri.parse(if (box == "in") "content://sms/inbox" else "content://sms/sent")
        val out = ArrayList<Row>()
        ctx.contentResolver.query(uri, arrayOf("_id", "address", "body", "date"), "_id > ?", arrayOf(after.toString()), "_id ASC")?.use { c ->
            while (c.moveToNext() && out.size < limit) {
                out.add(Row(box, c.getLong(0), c.getString(1) ?: "", c.getString(2) ?: "", c.getLong(3) / 1000))
            }
        }
        return out
    }
}

/** Sends through SmsManager; the sent / delivered / failure reports arrive at [SendResultReceiver]. */
class ManagerSender(private val ctx: Context) : SmsSender {
    override fun send(outId: Long, number: String, body: String) {
        val sm = if (android.os.Build.VERSION.SDK_INT >= 31) ctx.getSystemService(SmsManager::class.java) else @Suppress("DEPRECATION") SmsManager.getDefault()
        val parts = sm.divideMessage(body)
        fun pi(kind: String, n: Int) = PendingIntent.getBroadcast(ctx, (outId.toInt() * 8 + n) and 0x7fffffff,
            Intent(ctx, SendResultReceiver::class.java).setAction("org.phorepoc.phesms.$kind").putExtra("out", outId).putExtra("part", n).putExtra("parts", parts.size),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        if (parts.size > 1) {
            sm.sendMultipartTextMessage(number, null, parts, ArrayList(parts.indices.map { pi("SENT", it) }), ArrayList(parts.indices.map { pi("DELIVERED", it) }))
        } else {
            sm.sendTextMessage(number, null, body, pi("SENT", 0), pi("DELIVERED", 0))
        }
    }
}

class SendResultReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, i: Intent) {
        val out = i.getLongExtra("out", -1)
        if (out < 0) return
        val kind = i.action?.substringAfterLast('.') ?: return
        val prefs = Prefs(ctx)
        val engine = App.engine(ctx)
        if (kind == "SENT") {
            if (resultCode == android.app.Activity.RESULT_OK) {
                // multipart: report "sent" once, after the last part
                if (i.getIntExtra("part", 0) == i.getIntExtra("parts", 1) - 1) engine.onSendResult(out, "sent")
            } else engine.onSendResult(out, "failed", when (resultCode) {
                SmsManager.RESULT_ERROR_NO_SERVICE -> "no signal"
                SmsManager.RESULT_ERROR_RADIO_OFF -> "radio off"
                SmsManager.RESULT_ERROR_NULL_PDU -> "bad message"
                else -> "send failed ($resultCode)"
            })
        } else if (resultCode == android.app.Activity.RESULT_OK) {
            if (i.getIntExtra("part", 0) == i.getIntExtra("parts", 1) - 1) engine.onSendResult(out, "delivered")
        } else engine.onSendResult(out, "failed", "not delivered")
        prefs.setStatus("send report for #$out: $kind")
        SyncJob.now(ctx)
    }
}

/** An incoming message wakes the sync (RECEIVE_SMS; the message itself is read from the provider). */
class SmsReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, i: Intent) { SyncJob.now(ctx) }
}

class Boot : BroadcastReceiver() {
    override fun onReceive(ctx: Context, i: Intent) { SyncJob.schedule(ctx) }
}

object Http {
    /** The hub's certificate is trusted only when the SHA-256 of its leaf (DER) equals the pin; an empty pin means the system CAs. */
    private fun conn(url: String, pin: String): HttpURLConnection {
        val c = URL(url).openConnection() as HttpURLConnection
        if (c is HttpsURLConnection && pin.isNotEmpty()) {
            val tm = object : X509TrustManager {
                override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
                override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
                    if (Signer.sha256Hex(chain[0].encoded) != pin) throw java.security.cert.CertificateException("server certificate does not match the pin")
                }
                override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
            }
            val ctx = SSLContext.getInstance("TLS"); ctx.init(null, arrayOf<TrustManager>(tm), SecureRandom())
            c.sslSocketFactory = ctx.socketFactory
            c.hostnameVerifier = javax.net.ssl.HostnameVerifier { _, _ -> true }     // the pin is the identity
        }
        c.connectTimeout = 15000; c.readTimeout = 30000
        return c
    }

    fun post(base: String, path: String, pin: String, body: String, auth: String?): Pair<Int, String> {
        val c = conn(base.trimEnd('/') + path, pin)
        c.requestMethod = "POST"; c.doOutput = true
        c.setRequestProperty("Content-Type", "application/json")
        if (auth != null) c.setRequestProperty("X-Phe-Auth", auth)
        c.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
        val code = c.responseCode
        val text = (if (code < 400) c.inputStream else c.errorStream)?.use { it.readBytes().toString(Charsets.UTF_8) } ?: ""
        return code to text
    }

    /** The leaf certificate fingerprint a server presents, for the user to compare with the hub's before trusting it. */
    fun fetchPin(base: String): String {
        var seen = ""
        val tm = object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) { seen = Signer.sha256Hex(chain[0].encoded) }
            override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
        }
        val ctx = SSLContext.getInstance("TLS"); ctx.init(null, arrayOf<TrustManager>(tm), SecureRandom())
        val c = URL(base.trimEnd('/') + "/").openConnection() as HttpsURLConnection
        c.sslSocketFactory = ctx.socketFactory; c.hostnameVerifier = javax.net.ssl.HostnameVerifier { _, _ -> true }
        c.connectTimeout = 10000; c.readTimeout = 10000
        try { c.responseCode } catch (_: Exception) {}
        c.disconnect()
        return seen
    }

    /** Pairing: POST /sms/pair {code, name} -> {dev, key}. */
    fun pair(base: String, pin: String, code: String, name: String): Pair<String, String> {
        val (status, text) = post(base, "/sms/pair", pin, JSONObject().put("code", code.trim()).put("name", name).toString(), null)
        if (status != 200) throw java.io.IOException(runCatching { JSONObject(text).getString("error") }.getOrDefault("pairing failed ($status)"))
        val o = JSONObject(text)
        return o.getString("dev") to o.getString("key")
    }
}

class HttpHub(private val prefs: Prefs, private val clock: () -> Long = { System.currentTimeMillis() / 1000 }) : HubTransport {
    override fun sync(messages: List<Sms>, acks: List<Ack>): SyncReply {
        val body = JSONObject().put("messages", org.json.JSONArray(messages.map { it.toJson() })).put("acks", org.json.JSONArray(acks.map { it.toJson() })).toString()
        val auth = Signer.header(prefs.dev, Signer.unhex(prefs.key), "POST", "/sms/sync", clock())
        val (code, text) = Http.post(prefs.hub, "/sms/sync", prefs.pin, body, auth)
        if (code != 200) throw java.io.IOException("hub answered $code")
        val o = JSONObject(text)
        val pend = o.optJSONArray("pending")
        return SyncReply(o.optInt("accepted"), List(pend?.length() ?: 0) { val p = pend!!.getJSONObject(it); Pending(p.getLong("out_id"), p.getString("number"), p.getString("body")) })
    }
}

object App {
    @Volatile private var e: Engine? = null
    private val lock = Any()
    fun engine(ctx: Context): Engine = e ?: synchronized(lock) {
        e ?: Prefs(ctx.applicationContext).let { p ->
            Engine(ProviderSource(ctx.applicationContext), HttpHub(p), ManagerSender(ctx.applicationContext), p) { p.excludeList() }.also { e = it }
        }
    }
}
