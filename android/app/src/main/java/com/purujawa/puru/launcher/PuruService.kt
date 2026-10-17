package com.purujawa.puru.launcher

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import java.io.BufferedReader
import java.io.InputStreamReader
import java.util.concurrent.TimeUnit

/**
 * Foreground service that keeps `puru gateway` (the Telegram bot) alive while
 * the phone sleeps, and mirrors its output into the shared log file.
 */
class PuruService : Service() {

    private var process: Process? = null
    private var pump: Thread? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopGateway()
                stopSelf()
                return START_NOT_STICKY
            }

            else -> startGateway()
        }
        return START_STICKY
    }

    override fun onDestroy() {
        stopGateway()
        super.onDestroy()
    }

    private fun startGateway() {
        if (process?.isAlive == true) {
            return
        }
        startForegroundCompat()
        val config = PuruRuntime.configFile(this).absolutePath
        PuruRuntime.appendLog(this, "\n--- puru gateway --config $config ---\n")
        val gateway = try {
            PuruRuntime.process(this, emptyMap(), "gateway", "--config", config)
        } catch (error: Exception) {
            PuruRuntime.appendLog(this, "cannot start puru gateway: ${error.message}\n")
            running = false
            stopSelf()
            return
        }
        process = gateway
        running = true
        pump = Thread { drain(gateway) }.apply {
            isDaemon = true
            start()
        }
    }

    private fun stopGateway() {
        val gateway = process ?: return
        process = null
        running = false
        PuruRuntime.appendLog(this, "\n--- stopping puru gateway ---\n")
        // SIGTERM: puru cancels its context, reaps its own children, and exits.
        gateway.destroy()
        Thread {
            if (!gateway.waitFor(STOP_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
                gateway.destroyForcibly()
            }
        }.apply {
            isDaemon = true
            start()
        }
    }

    /** Copies the gateway output into the log file until the process exits. */
    private fun drain(gateway: Process) {
        try {
            BufferedReader(InputStreamReader(gateway.inputStream)).use { input ->
                val buffer = CharArray(4096)
                while (true) {
                    val read = input.read(buffer)
                    if (read < 0) {
                        break
                    }
                    PuruRuntime.appendLog(this, String(buffer, 0, read))
                }
            }
        } catch (error: Exception) {
            PuruRuntime.appendLog(this, "\ngateway output reader stopped: ${error.message}\n")
        }
        if (process === gateway) {
            process = null
        }
        running = false
        val code = runCatching { gateway.exitValue() }.getOrDefault(-1)
        PuruRuntime.appendLog(this, "\n--- puru gateway stopped (exit $code) ---\n")
        stopSelf()
    }

    private fun startForegroundCompat() {
        createChannel()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIFICATION_ID,
                buildNotification(),
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
            )
        } else {
            @Suppress("DEPRECATION")
            startForeground(NOTIFICATION_ID, buildNotification())
        }
    }

    private fun buildNotification(): Notification {
        val openApp = PendingIntent.getActivity(
            this,
            1,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val stopBot = PendingIntent.getService(
            this,
            2,
            Intent(this, PuruService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_puru)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(getString(R.string.notification_text))
            .setContentIntent(openApp)
            .setOngoing(true)
            .setShowWhen(false)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .addAction(0, getString(R.string.action_stop), stopBot)
            .build()
    }

    private fun createChannel() {
        val manager = getSystemService(NotificationManager::class.java) ?: return
        if (manager.getNotificationChannel(CHANNEL_ID) == null) {
            manager.createNotificationChannel(
                NotificationChannel(
                    CHANNEL_ID,
                    getString(R.string.channel_name),
                    NotificationManager.IMPORTANCE_LOW,
                ),
            )
        }
    }

    companion object {
        private const val CHANNEL_ID = "puru-gateway"
        private const val NOTIFICATION_ID = 1
        private const val STOP_TIMEOUT_SECONDS = 8L

        const val ACTION_START = "com.purujawa.puru.launcher.action.START"
        const val ACTION_STOP = "com.purujawa.puru.launcher.action.STOP"

        /** True while the gateway is starting, running, or stopping; the UI polls this. */
        @Volatile
        var running: Boolean = false
            private set

        fun start(context: Context) {
            val intent = Intent(context, PuruService::class.java).setAction(ACTION_START)
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, PuruService::class.java).setAction(ACTION_STOP)
            context.startService(intent)
        }
    }
}
