// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package org.phorepoc.phesms

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context

/** Runs one sync on a background thread. A periodic job (every 15 minutes, any network) plus one-shot jobs when a message arrives, a send report comes in or the user taps Sync. */
class SyncJob : JobService() {
    @Volatile private var thread: Thread? = null

    override fun onStartJob(p: JobParameters): Boolean {
        val prefs = Prefs(this)
        if (!prefs.paired) return false
        thread = Thread {
            val engine = App.engine(this)
            val ok = try { engine.run() } catch (e: Exception) { false }
            prefs.setStatus((if (ok) "synced" else "offline") + (engine.lastError?.let { ": $it" } ?: "") + " - queued ${engine.queued()} messages, ${engine.queuedAcks()} reports")
            jobFinished(p, !ok)                    // not reached: the scheduler tries again with its back-off
        }.also { it.start() }
        return true
    }

    override fun onStopJob(p: JobParameters): Boolean = true

    companion object {
        private const val PERIODIC = 1
        private const val NOW = 2

        fun schedule(ctx: Context) {
            val js = ctx.getSystemService(JobScheduler::class.java)
            js.schedule(JobInfo.Builder(PERIODIC, ComponentName(ctx, SyncJob::class.java))
                .setPeriodic(15 * 60 * 1000L).setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setPersisted(true).build())
        }

        fun now(ctx: Context) {
            val js = ctx.getSystemService(JobScheduler::class.java)
            js.schedule(JobInfo.Builder(NOW, ComponentName(ctx, SyncJob::class.java))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setOverrideDeadline(0).build())
        }
    }
}
