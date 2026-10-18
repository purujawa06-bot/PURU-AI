package com.purujawa.puru.launcher

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Typeface
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.view.View
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import kotlin.math.floor

/**
 * Single screen: the config form, start/stop controls, and the gateway log.
 *
 * Every row of the form maps to one `puru setup` environment variable, so saving
 * shells out to the Go binary and config.json keeps a single writer. A row left
 * empty is simply not passed, and puru then keeps the value already in
 * config.json (or its own default), which each hint states.
 */
class MainActivity : AppCompatActivity() {

    private companion object {
        const val REQUEST_NOTIFICATIONS = 1
        const val REQUEST_STORAGE = 2
        const val LOG_TAIL_LINES = 200
        const val REFRESH_MS = 2_000L
        const val SETUP_TIMEOUT_SECONDS = 60L
        const val VERSION_TIMEOUT_SECONDS = 15L

        /** Largest config.json the import accepts, a sane guard. */
        const val MAX_IMPORT_CHARS = 1 shl 20
    }

    /** How a config row is edited. */
    private enum class Kind { TEXT, NUMBER, TOGGLE }

    /**
     * One `puru setup` field: the env var it feeds, the label and hint shown,
     * and how it is edited. Required rows are validated before saving; advanced
     * rows stay hidden until the user asks for them.
     */
    private class Field(
        val env: String,
        val label: String,
        val hint: String,
        val section: String,
        val kind: Kind = Kind.TEXT,
        val required: Boolean = false,
        val advanced: Boolean = false,
        val checkedByDefault: Boolean = false,
    )

    private val fields = listOf(
        Field(
            "TELEGRAM_BOT_TOKEN", "Bot token (required)", "123456:ABC… from @BotFather",
            "Telegram", required = true,
        ),
        Field(
            "PURU_ALLOWED_USERS", "Allowed Telegram user IDs (optional)",
            "empty = anyone can use the bot, else comma separated IDs", "Telegram",
        ),
        Field(
            "PURU_BASE_URL", "Base URL (required)", "https://api.openai.com/v1",
            "Model", required = true,
        ),
        Field("PURU_MODEL", "Model name (required)", "gpt-4o-mini", "Model", required = true),
        Field(
            "PURU_API_KEY", "API key (optional)",
            "empty = keep current; local endpoints need none", "Model",
        ),
        Field(
            "PURU_TEMPERATURE", "Temperature (optional)", "0 = provider default, e.g. 0.7",
            "Model", kind = Kind.NUMBER,
        ),
        Field(
            "PURU_WORKSPACE", "Workspace directory (optional)",
            "where the agent reads and writes files; default /sdcard/PURU-AI/workspace",
            "Workspace",
        ),
        Field(
            "PURU_RESTRICT_WORKSPACE", "Keep the agent inside the workspace",
            "recommended: blocks paths outside it", "Workspace",
            kind = Kind.TOGGLE, checkedByDefault = true,
        ),
        Field(
            "PURU_TIMEZONE", "Timezone (optional)", "Asia/Jakarta", "Runtime", advanced = true,
        ),
        Field(
            "PURU_MAX_ITERATIONS", "Max tool iterations (optional)", "500",
            "Runtime", kind = Kind.NUMBER, advanced = true,
        ),
        Field(
            "PURU_HISTORY_TOKEN_LIMIT", "History token limit (optional)", "30000",
            "Runtime", kind = Kind.NUMBER, advanced = true,
        ),
        Field(
            "PURU_TOOLS_PREVIEW", "Show live tool calls",
            "edits the chat message as each tool runs", "Runtime",
            kind = Kind.TOGGLE, checkedByDefault = true, advanced = true,
        ),
        Field(
            "PURU_LOOP_DELAY_SECONDS", "Loop delay seconds (optional)", "3",
            "Runtime", kind = Kind.NUMBER, advanced = true,
        ),
        Field(
            "PURU_EXEC_MEMORY_MB", "Shell memory limit MB (optional)", "64",
            "Runtime", kind = Kind.NUMBER, advanced = true,
        ),
        Field(
            "PURU_SKILLS_MODE", "Skills mode (optional)", "default | off | custom",
            "Skills", advanced = true,
        ),
        Field(
            "PURU_SKILLS_ALLOW", "Skills allowlist (optional)",
            "only used when skills_mode=custom, comma separated", "Skills", advanced = true,
        ),
        Field(
            "PURU_WEBSEARCH_AISTUDIO_ACTIVE", "AI Studio web search (Gemini grounding)",
            "off unless enabled", "Web search", kind = Kind.TOGGLE, advanced = true,
        ),
        Field(
            "PURU_WEBSEARCH_AISTUDIO_MODEL", "AI Studio model (optional)",
            "gemini-flash-lite-latest", "Web search", advanced = true,
        ),
        Field(
            "PURU_WEBSEARCH_AISTUDIO_KEY", "AI Studio API key (optional)",
            "required to search", "Web search", advanced = true,
        ),
        Field(
            "PURU_WEBSEARCH_EXA_ACTIVE", "Exa web search", "off unless enabled",
            "Web search", kind = Kind.TOGGLE, advanced = true,
        ),
        Field(
            "PURU_WEBSEARCH_EXA_KEY", "Exa API key (optional)", "required to search",
            "Web search", advanced = true,
        ),
    )

    private val handler = Handler(Looper.getMainLooper())
    private val inputs = mutableMapOf<String, EditText>()
    private val toggles = mutableMapOf<String, CheckBox>()
    private var saving = false

    /**
     * The user picks an existing config.json (Downloads, Drive, …) and the file
     * is handed to `puru setup` for validation before it becomes the live one.
     */
    private val pickConfigFile =
        registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
            if (uri != null) {
                importConfig(uri)
            }
        }

    /** Destination for "Export config.json" (Downloads, Drive, …). */
    private val createConfigFile =
        registerForActivityResult(ActivityResultContracts.CreateDocument("application/json")) { uri ->
            if (uri != null) {
                exportConfig(uri)
            }
        }

    private lateinit var binaryView: TextView
    private lateinit var statusView: TextView
    private lateinit var startButton: Button
    private lateinit var stopButton: Button
    private lateinit var advancedContainer: LinearLayout
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
        advancedContainer = findViewById(R.id.container_advanced)
        logView = findViewById(R.id.text_log)

        buildForm()
        prefillFromConfig()

        findViewById<CheckBox>(R.id.toggle_advanced).setOnCheckedChangeListener { _, checked ->
            advancedContainer.visibility = if (checked) View.VISIBLE else View.GONE
        }
        findViewById<Button>(R.id.button_save).setOnClickListener { saveConfig(startAfter = false) }
        findViewById<Button>(R.id.button_import).setOnClickListener { pickConfigFile.launch(arrayOf("*/*")) }
        findViewById<Button>(R.id.button_export).setOnClickListener {
            createConfigFile.launch("puru-config.json")
        }
        findViewById<Button>(R.id.button_start).setOnClickListener { startGateway() }
        findViewById<Button>(R.id.button_stop).setOnClickListener { PuruService.stop(this) }
        findViewById<Button>(R.id.button_clear_log).setOnClickListener {
            PuruRuntime.clearLog(this)
            renderLog()
        }

        showBinaryVersion()
        PuruRuntime.ensureBinDir(this)
        ensureStoragePermission()
    }

    override fun onResume() {
        super.onResume()
        handler.post(refresh)
        // The workspace moves to /sdcard/PURU-AI only once storage access is
        // granted, so it is retried on every resume (and is a no-op after).
        migrateWorkspace()
    }

    override fun onPause() {
        super.onPause()
        handler.removeCallbacks(refresh)
    }

    /** Builds the whole form from [fields], one row per puru setup variable. */
    private fun buildForm() {
        val basicContainer = findViewById<LinearLayout>(R.id.container_fields)
        var section = ""
        for (field in fields) {
            val parent = if (field.advanced) advancedContainer else basicContainer
            if (field.section != section) {
                section = field.section
                parent.addView(sectionTitle(field.section))
            }
            parent.addView(fieldLabel(field))
            if (field.kind == Kind.TOGGLE) {
                val toggle = CheckBox(this).apply {
                    text = field.hint
                    isChecked = field.checkedByDefault
                }
                toggles[field.env] = toggle
                parent.addView(toggle)
            } else {
                val input = EditText(this).apply {
                    hint = field.hint
                    maxLines = 1
                    importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_NO
                    inputType = if (field.kind == Kind.NUMBER) {
                        InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_FLAG_DECIMAL
                    } else {
                        InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                    }
                }
                inputs[field.env] = input
                parent.addView(input)
            }
        }
    }

    private fun sectionTitle(text: String): TextView = TextView(this).apply {
        this.text = text.uppercase()
        setTextColor(ContextCompat.getColor(context, R.color.accent))
        textSize = 14f
        typeface = Typeface.DEFAULT_BOLD
        layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT,
        ).apply { topMargin = dp(24) }
    }

    private fun fieldLabel(field: Field): TextView = TextView(this).apply {
        text = field.label
        setTextColor(
            ContextCompat.getColor(
                context,
                if (field.required) R.color.label_required else R.color.label_optional,
            ),
        )
        textSize = 13f
        typeface = Typeface.DEFAULT_BOLD
        layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT,
        ).apply { topMargin = dp(12) }
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

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
        val missing = missingRequired()
        if (missing.isNotEmpty()) {
            toast(getString(R.string.error_missing_fields, missing.joinToString(", ")))
            return
        }
        val env = collectEnv()

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

    /**
     * Imports a config.json the user picked: the file is copied into app
     * storage, validated and normalized by `puru setup` on that copy, and only
     * then moved over the live config.json — so a broken file never destroys the
     * working setup, and the Go binary still produces the final content.
     */
    private fun importConfig(uri: Uri) {
        val text = try {
            contentResolver.openInputStream(uri)?.use { it.bufferedReader().readText() }
                ?: throw IllegalStateException(uri.lastPathSegment.orEmpty())
        } catch (error: Exception) {
            toast(getString(R.string.import_failed, error.message.orEmpty()))
            return
        }
        if (text.length > MAX_IMPORT_CHARS) {
            toast(getString(R.string.import_failed, "${text.length} chars"))
            return
        }

        val appContext = applicationContext
        val scratch = PuruRuntime.importFile(appContext)
        saving = true
        renderStatus()
        Thread {
            scratch.parentFile?.mkdirs()
            scratch.writeText(text)
            val (code, output) = PuruRuntime.run(
                appContext,
                emptyMap(),
                SETUP_TIMEOUT_SECONDS,
                "setup",
                "--force",
                "--config",
                scratch.absolutePath,
            )
            var imported = false
            if (code == 0) {
                imported = runCatching {
                    scratch.copyTo(PuruRuntime.configFile(appContext), overwrite = true)
                }.isSuccess
            }
            scratch.delete()
            val reason = output.trim().lines().lastOrNull { it.isNotBlank() }.orEmpty()

            runOnUiThread {
                saving = false
                PuruRuntime.appendLog(appContext, "\n$ puru setup (import)\n$output")
                renderStatus()
                renderLog()
                if (!imported) {
                    toast(getString(R.string.import_failed, reason))
                    return@runOnUiThread
                }
                prefillFromConfig()
                toast(getString(R.string.config_imported))
            }
        }.apply {
            isDaemon = true
            start()
        }
    }

    /** Env for `puru setup`; empty text rows are skipped so puru keeps its own value. */
    private fun collectEnv(): Map<String, String> {
        val env = linkedMapOf<String, String>()
        for (field in fields) {
            if (field.kind == Kind.TOGGLE) {
                toggles[field.env]?.let { env[field.env] = if (it.isChecked) "1" else "0" }
                continue
            }
            val value = inputs[field.env]?.text?.toString()?.trim().orEmpty()
            if (value.isNotEmpty()) {
                env[field.env] = value
            }
        }
        return env
    }

    /** Marks empty required rows and returns their labels. */
    private fun missingRequired(): List<String> {
        val missing = mutableListOf<String>()
        for (field in fields.filter { it.required }) {
            val input = inputs[field.env] ?: continue
            if (input.text.toString().trim().isEmpty()) {
                input.error = getString(R.string.error_required_field)
                missing += field.label
            }
        }
        return missing
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

    /**
     * Asks for the classic storage permission: it is what lets the agent, and
     * the shell it spawns, read, write and delete files on /sdcard. Without it
     * everything still works, but only inside the app's own folders.
     */
    private fun ensureStoragePermission() {
        val missing = listOf(
            Manifest.permission.READ_EXTERNAL_STORAGE,
            Manifest.permission.WRITE_EXTERNAL_STORAGE,
        ).filter {
            ContextCompat.checkSelfPermission(this, it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) {
            ActivityCompat.requestPermissions(this, missing.toTypedArray(), REQUEST_STORAGE)
        }
    }

    /**
     * Moves a workspace that still lives in app-internal storage to the shared
     * folder (`/sdcard/PURU-AI/workspace`) and points config.json at it, so the
     * user can read, write and delete the agent's files with a file manager or
     * over USB. Only that one JSON key changes; every other setting stays
     * untouched. The old copy is deleted only after each file arrived with the
     * same size, an existing shared workspace is never overwritten, and nothing
     * happens while storage access is still missing.
     */
    private fun migrateWorkspace() {
        val appContext = applicationContext
        if (!PuruRuntime.hasStorageAccess(appContext)) {
            return
        }
        val target = PuruRuntime.defaultWorkspace(appContext)
        val legacy = PuruRuntime.legacyWorkspace(appContext)
        if (target == legacy) {
            return
        }
        val config = PuruRuntime.configFile(appContext)
        Thread {
            val root = runCatching { JSONObject(config.readText()) }.getOrNull() ?: return@Thread
            if (root.optString("workspace").trimEnd('/') != legacy.trimEnd('/')) {
                return@Thread
            }
            val source = File(legacy)
            val destination = File(target)
            if (destination.exists()) {
                PuruRuntime.appendLog(appContext, "\nworkspace migration skipped: $target already exists\n")
                return@Thread
            }
            if (source.isDirectory && !PuruRuntime.copyTree(source, destination)) {
                PuruRuntime.appendLog(appContext, "\nworkspace migration failed, keeping $legacy\n")
                return@Thread
            }
            root.put("workspace", target)
            if (!runCatching { config.writeText(root.toString(2)) }.isSuccess) {
                PuruRuntime.appendLog(appContext, "\nworkspace migration: cannot write ${config.name}\n")
                return@Thread
            }
            if (source.isDirectory) {
                PuruRuntime.deleteTree(source)
            }
            PuruRuntime.appendLog(
                appContext,
                "\nworkspace moved to $target (restart the bot to use it)\n",
            )
            runOnUiThread { prefillFromConfig() }
        }.apply {
            isDaemon = true
            start()
        }
    }

    /** Writes a copy of config.json to the location the user picked. */
    private fun exportConfig(uri: Uri) {
        val text = runCatching { PuruRuntime.configFile(this).readText() }.getOrNull()
        if (text == null) {
            toast(getString(R.string.export_failed, PuruRuntime.configFile(this).name))
            return
        }
        val error = runCatching {
            val stream = contentResolver.openOutputStream(uri)
                ?: throw IllegalStateException(uri.lastPathSegment.orEmpty())
            stream.use { it.write(text.toByteArray()) }
        }.exceptionOrNull()
        if (error != null) {
            toast(getString(R.string.export_failed, error.message.orEmpty()))
            return
        }
        toast(getString(R.string.config_exported, uri.lastPathSegment.orEmpty()))
    }

    /** Shows the values of an existing config.json, so the form reflects reality. */
    private fun prefillFromConfig() {
        val file = PuruRuntime.configFile(this)
        val root = if (file.isFile) runCatching { JSONObject(file.readText()) }.getOrNull() else null
        val model = root?.optJSONObject("model")
        val search = root?.optJSONObject("web_search")
        val aistudio = search?.optJSONObject("aistudio")
        val exa = search?.optJSONObject("exa")
        val workspace = root?.optString("workspace").orEmpty().ifEmpty {
            PuruRuntime.defaultWorkspace(this)
        }

        inputs["TELEGRAM_BOT_TOKEN"]?.setText(root?.optString("telegram_bot_token").orEmpty())
        inputs["PURU_ALLOWED_USERS"]?.setText(numberList(root?.optJSONArray("telegram_allowed_users")))
        inputs["PURU_BASE_URL"]?.setText(model?.optString("base_url").orEmpty())
        inputs["PURU_MODEL"]?.setText(model?.optString("model").orEmpty())
        inputs["PURU_API_KEY"]?.setText(model?.optString("api_key").orEmpty())
        inputs["PURU_TEMPERATURE"]?.setText(number(model, "temperature"))
        inputs["PURU_WORKSPACE"]?.setText(workspace)
        toggles["PURU_RESTRICT_WORKSPACE"]?.isChecked = root?.optBoolean("restrict_workspace", true) ?: true
        inputs["PURU_TIMEZONE"]?.setText(root?.optString("timezone").orEmpty())
        inputs["PURU_MAX_ITERATIONS"]?.setText(number(root, "max_iterations"))
        inputs["PURU_HISTORY_TOKEN_LIMIT"]?.setText(number(root, "history_token_limit"))
        toggles["PURU_TOOLS_PREVIEW"]?.isChecked = root?.optBoolean("tools_preview", true) ?: true
        inputs["PURU_LOOP_DELAY_SECONDS"]?.setText(number(root, "loop_delay_seconds"))
        inputs["PURU_EXEC_MEMORY_MB"]?.setText(number(root, "exec_memory_mb"))
        inputs["PURU_SKILLS_MODE"]?.setText(root?.optString("skills_mode").orEmpty())
        inputs["PURU_SKILLS_ALLOW"]?.setText(textList(root?.optJSONArray("skills_allow")))
        toggles["PURU_WEBSEARCH_AISTUDIO_ACTIVE"]?.isChecked = aistudio?.optBoolean("active") == true
        inputs["PURU_WEBSEARCH_AISTUDIO_MODEL"]?.setText(aistudio?.optString("model").orEmpty())
        inputs["PURU_WEBSEARCH_AISTUDIO_KEY"]?.setText(aistudio?.optString("api_key").orEmpty())
        toggles["PURU_WEBSEARCH_EXA_ACTIVE"]?.isChecked = exa?.optBoolean("active") == true
        inputs["PURU_WEBSEARCH_EXA_KEY"]?.setText(exa?.optString("api_key").orEmpty())
    }

    /** Formats an optional JSON number as text (0.7 stays 0.7, 500.0 becomes 500). */
    private fun number(parent: JSONObject?, key: String): String {
        if (parent == null || !parent.has(key) || parent.isNull(key)) {
            return ""
        }
        val value = parent.optDouble(key)
        if (value.isNaN()) {
            return ""
        }
        return if (value == floor(value)) value.toLong().toString() else value.toString()
    }

    /** Telegram user IDs, skipping anything that is not a number. */
    private fun numberList(array: JSONArray?): String = joinList(array, digitsOnly = true)

    private fun textList(array: JSONArray?): String = joinList(array, digitsOnly = false)

    private fun joinList(array: JSONArray?, digitsOnly: Boolean): String {
        if (array == null) {
            return ""
        }
        val parts = mutableListOf<String>()
        for (index in 0 until array.length()) {
            val text = array.opt(index)?.toString()?.trim().orEmpty()
            if (text.isEmpty() || text == "null") {
                continue
            }
            if (digitsOnly && text.toLongOrNull() == null) {
                continue
            }
            parts += text
        }
        return parts.joinToString(", ")
    }

    private fun toast(message: String) {
        Toast.makeText(this, message, Toast.LENGTH_SHORT).show()
    }
}
