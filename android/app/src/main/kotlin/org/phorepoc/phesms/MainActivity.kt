// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package org.phorepoc.phesms

import android.Manifest
import android.app.Activity
import android.os.Bundle
import android.text.InputType
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView

/** One screen, built in code (no layout files, no libraries): pairing, the exclude list, Sync now, the last status. */
class MainActivity : Activity() {
    private lateinit var prefs: Prefs
    private lateinit var status: TextView

    private fun edit(hint: String, text: String = "", multi: Boolean = false) = EditText(this).also {
        it.hint = hint; it.setText(text)
        it.inputType = InputType.TYPE_CLASS_TEXT or (if (multi) InputType.TYPE_TEXT_FLAG_MULTI_LINE else 0)
    }

    override fun onCreate(b: Bundle?) {
        super.onCreate(b)
        prefs = Prefs(this)
        val col = LinearLayout(this).also { it.orientation = LinearLayout.VERTICAL; it.setPadding(32, 32, 32, 32) }
        setContentView(ScrollView(this).also { it.addView(col) })
        col.addView(TextView(this).also { it.text = "Cloud Companion SMS"; it.textSize = 22f })
        status = TextView(this); col.addView(status)

        val hub = edit("Hub address, e.g. https://hub.example:8443", prefs.hub)
        val pin = edit("Certificate fingerprint (SHA-256, 64 hex digits)", prefs.pin)
        val code = edit("Pairing code (ABCD-EF12)")
        val name = edit("Name of this phone", "Android phone")
        val fetch = Button(this).also { it.text = "Fetch fingerprint" }
        val pair = Button(this).also { it.text = "Pair" }
        for (v in listOf<View>(hub, pin, fetch, code, name, pair)) col.addView(v)
        fetch.setOnClickListener {
            val h = hub.text.toString().trim()
            Thread {
                val r = try { Http.fetchPin(h).ifEmpty { "no TLS certificate seen" } } catch (e: Exception) { "failed: ${e.message}" }
                runOnUiThread { pin.setText(r); say("Compare this fingerprint with the one the hub printed (phecloud paircode), digit by digit, before pairing.") }
            }.start()
        }
        pair.setOnClickListener {
            val h = hub.text.toString().trim()
            val p = pin.text.toString().lowercase().filter { it.isLetterOrDigit() }
            if (h.startsWith("https://") && p.length != 64) { say("An https hub needs its 64-digit fingerprint (Fetch fingerprint, then compare)."); return@setOnClickListener }
            Thread {
                try {
                    val (dev, key) = Http.pair(h, p, code.text.toString(), name.text.toString())
                    prefs.hub = h; prefs.pin = p; prefs.dev = dev; prefs.key = key
                    SyncJob.schedule(this); SyncJob.now(this)
                    runOnUiThread { say("Paired as device $dev. First sync started.") }
                } catch (e: Exception) { runOnUiThread { say("Pairing failed: ${e.message}") } }
            }.start()
        }

        col.addView(TextView(this).also { it.text = "Do not sync messages from (one number or sender per line):" })
        val ex = edit("e.g. BANK, 22395, +49 171 5550100", prefs.exclude, true)
        col.addView(ex)
        col.addView(Button(this).also {
            it.text = "Save list"
            it.setOnClickListener { prefs.exclude = ex.text.toString(); say("Saved. New messages from these senders are not uploaded (already uploaded ones stay on the hub).") }
        })
        col.addView(Button(this).also {
            it.text = "Sync now"
            it.setOnClickListener { if (prefs.paired) { SyncJob.now(this); say("Sync started.") } else say("Pair first.") }
        })
        col.addView(Button(this).also {
            it.text = "Forget this hub"
            it.setOnClickListener { prefs.dev = ""; prefs.key = ""; say("Unpaired on this phone. Revoke the device on the hub too (phecloud revoke).") }
        })
        requestPermissions(arrayOf(Manifest.permission.READ_SMS, Manifest.permission.RECEIVE_SMS, Manifest.permission.SEND_SMS), 1)
        if (prefs.paired) SyncJob.schedule(this)
    }

    override fun onResume() { super.onResume(); say(null) }

    private fun say(extra: String?) {
        status.text = listOfNotNull(if (prefs.paired) "Paired with ${prefs.hub}" else "Not paired", prefs.status().ifEmpty { null }, extra).joinToString("\n")
    }
}
