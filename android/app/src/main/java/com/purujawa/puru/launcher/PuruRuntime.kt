package com.purujawa.puru.launcher

import android.content.Context
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * Owns every path, environment variable, and child process the bundled puru
 * binary needs on Android.
 *
 * The binary ships as `jniLibs/<abi>/libpuru.so` so the platform extracts it
 * into the app's native library directory: that is the only place an app
 * targeting API 29+ is allowed to execute a file from.
 */
object PuruRuntime {

    /** File name of the bundled binary inside the APK native library folder. */
    const val BINARY_NAME = "libpuru.so"

    /** Log file is truncated once it grows past this size. */
    private const val MAX_LOG_BYTES = 512L * 1024L

    private val logLock = Any()

    fun binary(context: Context): File =
        File(context.applicationInfo.nativeLibraryDir, BINARY_NAME)

    fun configFile(context: Context): File =
        File(context.filesDir, ".puru/config.json")

    /**
     * Scratch file for `puru setup` while importing a config.json: the wizard
     * normalizes it there, and only a valid result is copied over configFile.
     */
    fun importFile(context: Context): File =
        File(context.filesDir, ".puru/import.json")

    fun logFile(context: Context): File =
        File(context.filesDir, "puru.log")

    /** Default workspace inside app storage; mirrors the Go side's config dir. */
    fun defaultWorkspace(context: Context): String =
        File(context.filesDir, ".puru/workspace").absolutePath

    /**
     * Environment shared by every puru process: HOME keeps `~/.puru` inside app
     * storage, PATH reaches Android's `sh` (the exec tool runs `sh -c`),
     * SSL_CERT_DIR points at the system certificate stores, and NO_COLOR keeps
     * ANSI escapes out of the plain-text log view.
     */
    fun environment(context: Context): Map<String, String> = mapOf(
        "HOME" to context.filesDir.absolutePath,
        "TMPDIR" to context.cacheDir.absolutePath,
        "PATH" to "/system/bin:/system/xbin:/apex/com.android.runtime/bin",
        "SSL_CERT_DIR" to "/system/etc/security/cacerts:/apex/com.android.conscrypt/cacerts",
        "PURU_NON_INTERACTIVE" to "1",
        "NO_COLOR" to "1",
    )

    /** Starts puru with [args], the shared environment, and merged output. */
    fun process(context: Context, extraEnv: Map<String, String>, vararg args: String): Process {
        val command = listOf(binary(context).absolutePath) + args
        val builder = ProcessBuilder(command)
        builder.directory(context.filesDir)
        builder.environment().putAll(environment(context))
        builder.environment().putAll(extraEnv)
        builder.redirectErrorStream(true)
        return builder.start()
    }

    /**
     * Runs puru to completion and returns exit code plus combined output.
     *
     * A process that cannot start at all is reported as output instead of
     * throwing, so the UI can show the reason in the log view.
     */
    fun run(
        context: Context,
        extraEnv: Map<String, String>,
        timeoutSeconds: Long,
        vararg args: String,
    ): Pair<Int, String> {
        val process = try {
            process(context, extraEnv, *args)
        } catch (error: Exception) {
            return 1 to "cannot run ${binary(context).absolutePath}: ${error.message}"
        }
        val output = try {
            process.inputStream.bufferedReader().use { it.readText() }
        } catch (error: Exception) {
            "reading puru output failed: ${error.message}"
        }
        if (!process.waitFor(timeoutSeconds, TimeUnit.SECONDS)) {
            process.destroyForcibly()
            return 1 to "$output\n(timed out after ${timeoutSeconds}s)"
        }
        return process.exitValue() to output
    }

    /** Appends [text] to the shared log file. */
    fun appendLog(context: Context, text: String) {
        val file = logFile(context)
        synchronized(logLock) {
            if (file.length() > MAX_LOG_BYTES) {
                file.writeText("")
            }
            file.appendText(text)
        }
    }

    /** Returns the last [lines] lines of the log file. */
    fun tailLog(context: Context, lines: Int): String {
        val file = logFile(context)
        if (!file.isFile) {
            return ""
        }
        return synchronized(logLock) {
            file.readLines().takeLast(lines).joinToString("\n")
        }
    }

    fun clearLog(context: Context) {
        synchronized(logLock) {
            logFile(context).writeText("")
        }
    }
}
