package com.purujawa.puru.launcher

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import org.json.JSONObject

/**
 * Single screen: the config form, start/stop controls, and the gateway log.
 *
 * Saving the form shells out to `puru setup --force`, so the Go binary stays
 * the only writer of config.json and the app never duplicates that schema.
 */
class MainActivity : AppCompatActivity() {

    private companion object {
        const val REQUEST_NOTIFICATIONS = 1
        const val LOG_TAIL_LINES = 200
        const val REFRESH_MS = 2_000L
        const val SETUP_TIMEOUT_SECONDS = 60L
        const val VERSION_TIMEOUT_SECONDS = 15L
    }

    private val handler = Handler(Looper.getMainLooper())
    private var saving = false

    private lateinit var binaryView: TextView
    private lateinit var statusView: TextView
    private lateinit var startButton: Button
    private lateinit var stopButton: Button
    private lateinit var tokenInput: EditText
    private lateinit var baseUrlInput: EditText
    private lateinit var apiKeyInput: EditText
    private lateinit var modelInput: EditText
    private lateinit var workspaceInput: EditText
    private lateinit var logView: TextView

    private val refresh = object : Runnable {
        override fun run() {
            renderStatus()
            renderLog()
            handler.postDelayed(this, REFRESH_MS)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        binaryView = findViewById(R.id.text_binary)
        statusView = findViewById(R.id.text_status)
        startButton = findViewById(R.id.button_start)
        stopButton = findViewById(R.id.button_stop)
        tokenInput = findViewById(R.id.input_token)
        baseUrlInput = findViewById(R.id.input_base_url)
        apiKeyInput = findViewById(R.id.input_api_key)
        modelInput = findViewById(R.id.input_model)
        workspaceInput = findViewById(R.id.input_workspace)
        logView = findViewById(R.id.text_log)

        workspaceInput.hint = PuruRuntime.defaultWorkspace(this)

        findViewById<Button>(R.id.button_save).setOnClickListener { saveConfig(startAfter = false) }
        findViewById<Button>(R.id.button_start).setOnClickListener { startGateway() }
        findViewById<Button>(R.id.button_stop).setOnClickListener { PuruService.stop(this) }
        findViewById<Button>(R.id.button_clear_log).setOnClickListener {
            PuruRuntime.clearLog(this)
            renderLog()
        }

        prefillFromConfig()
        showBinaryVersion()
    }

    override fun onResume() {
        super.onResume()
        handler.post(refresh)
    }

    override fun onPause() {
        super.onPause()
        handler.removeCallbacks(refresh)
    }

    /** Start = save the form first, then launch the gateway. */
    private fun startGateway() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                REQUEST_NOTIFICATIONS,
            )
        }
        saveConfig(startAfter = true)
    }

    private fun saveConfig(startAfter: Boolean) {
        val token = tokenInput.text.toString().trim()
        if (token.isEmpty()) {
            toast(getString(R.string.error_token_required))
            return
        }
        val values = linkedMapOf(
            "TELEGRAM_BOT_TOKEN" to token,
            "PURU_BASE_URL" to baseUrlInput.text.toString().trim(),
            "PURU_API_KEY" to apiKeyInput.text.toString().trim(),
            "PURU_MODEL" to modelInput.text.toString().trim(),
            "PURU_WORKSPACE" to workspaceInput.text.toString().trim(),
        )
        // Empty fields are skipped, so `puru setup` keeps the existing value or
        // its own default for them.
        val env = values.filterValues { it.isNotEmpty() }

        val appContext = applicationContext
        saving = true
        renderStatus()
        Thread {
            val (code, output) = PuruRuntime.run(
                appContext,
                env,
                SETUP_TIMEOUT_SECONDS,
                "setup",
                "--force",
                "--config",
                PuruRuntime.configFile(appContext).absolutePath,
            )
            runOnUiThread {
                saving = false
                PuruRuntime.appendLog(appContext, "\n$ puru setup\n$output")
                renderStatus()
                renderLog()
                if (code != 0) {
                    toast(getString(R.string.config_failed))
                    return@runOnUiThread
                }
                prefillFromConfig()
                if (!startAfter) {
                    toast(getString(R.string.config_saved))
                    return@runOnUiThread
                }
                toast(getString(R.string.gateway_starting))
                PuruService.start(appContext)
            }
        }.apply {
            isDaemon = true
            start()
        }
    }

    private fun renderStatus() {
        val running = PuruService.running
        statusView.text = getString(if (running) R.string.status_running else R.string.status_stopped)
        statusView.setTextColor(
            ContextCompat.getColor(this, if (running) R.color.status_running else R.color.status_stopped),
        )
        startButton.isEnabled = !running && !saving
        stopButton.isEnabled = running
    }

    private fun renderLog() {
        val text = PuruRuntime.tailLog(this, LOG_TAIL_LINES).trimEnd()
        logView.text = text.ifEmpty { getString(R.string.log_empty) }
    }

    /** Shows the bundled binary version; doubles as an exec self-test. */
    private fun showBinaryVersion() {
        val appContext = applicationContext
        Thread {
            val (code, output) = PuruRuntime.run(
                appContext,
                emptyMap(),
                VERSION_TIMEOUT_SECONDS,
                "--version",
            )
            runOnUiThread {
                val text = output.trim()
                binaryView.text = if (code == 0) {
                    text
                } else {
                    getString(R.string.binary_unavailable, text)
                }
            }
        }.apply {
            isDaemon = true
            start()
        }
    }

    /** Fills the form with the values of an existing config.json, if any. */
    private fun prefillFromConfig() {
        val file = PuruRuntime.configFile(this)
        if (!file.isFile) {
            return
        }
        val root = runCatching { JSONObject(file.readText()) }.getOrNull() ?: return
        val model = root.optJSONObject("model")
        tokenInput.setText(root.optString("telegram_bot_token"))
        baseUrlInput.setText(model?.optString("base_url").orEmpty())
        apiKeyInput.setText(model?.optString("api_key").orEmpty())
        modelInput.setText(model?.optString("model").orEmpty())
        workspaceInput.setText(
            root.optString("workspace").ifEmpty { PuruRuntime.defaultWorkspace(this) },
        )
    }

    private fun toast(message: String) {
        Toast.makeText(this, message, Toast.LENGTH_SHORT).show()
    }
}
