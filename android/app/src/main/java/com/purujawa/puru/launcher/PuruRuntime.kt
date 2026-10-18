package com.purujawa.puru.launcher

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Environment
import androidx.core.content.ContextCompat
import java.io.File
import java.nio.file.Files
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

    /** Directory on PATH for tools the user installs with `puru fetch`. */
    fun binDir(context: Context): File = File(context.filesDir, "bin")

    /**
     * App-external storage root (`/sdcard/Android/data/<pkg>/files`) or null
     * when external storage is not mounted. Files there need no permission and
     * the user can read, write and delete them from a file manager or over USB.
     */
    fun externalRoot(context: Context): File? = try {
        context.getExternalFilesDir(null)?.let { if (it.exists() || it.mkdirs()) it else null }
    } catch (error: Exception) {
        null
    }

    /**
     * True when the classic storage permission is granted. The launcher targets
     * API 28 with requestLegacyExternalStorage, so this single permission is
     * what lets the agent (and the shell it spawns) read, write and delete real
     * files under /sdcard.
     */
    fun hasStorageAccess(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.READ_EXTERNAL_STORAGE) ==
            PackageManager.PERMISSION_GRANTED

    /**
     * Root for the agent's files: `/sdcard/PURU-AI` once storage access is
     * granted, so the user sees a normal folder in a file manager or over USB.
     * Without it: app-external files dir, then app-internal storage.
     */
    fun shareRoot(context: Context): File {
        if (hasStorageAccess(context)) {
            val external = Environment.getExternalStorageDirectory()
            if (external != null && external.exists()) {
                return File(external, "PURU-AI")
            }
        }
        return externalRoot(context) ?: File(context.filesDir, ".puru")
    }

    /**
     * Default workspace: the shared folder when storage access exists, with
     * app-internal storage as fallback.
     */
    fun defaultWorkspace(context: Context): String =
        File(shareRoot(context), "workspace").absolutePath

    /** App-internal workspace of older builds, migrated to app-external storage. */
    fun legacyWorkspace(context: Context): String =
        File(context.filesDir, ".puru/workspace").absolutePath

    /**
     * Creates `<filesDir>/bin` and links `puru` there, so the agent's shell can
     * run `puru fetch` / `puru doctor`: the binary itself lives in the native
     * library dir under the name libpuru.so, which is not typeable.
     *
     * An app update extracts the libraries into a new directory, so an existing
     * link is refreshed instead of being left dangling.
     */
    fun ensureBinDir(context: Context) {
        val bin = binDir(context)
        if (!bin.isDirectory && !bin.mkdirs()) {
            return
        }
        val target = binary(context).toPath()
        val link = File(bin, "puru").toPath()
        val current = runCatching { Files.readSymbolicLink(link) }.getOrNull()
        if (current == target) {
            return
        }
        if (Files.exists(link) && !Files.isSymbolicLink(link)) {
            return // never clobber a real file the user put there
        }
        runCatching {
            Files.deleteIfExists(link)
            Files.createSymbolicLink(link, target)
        }
    }

    /**
     * Environment shared by every puru process: HOME keeps `~/.puru` inside app
     * storage, PATH reaches the installed tools, the bundled binary and
     * Android's `sh` (the exec tool runs `sh -c`), SSL_CERT_DIR points at the
     * system certificate stores, and NO_COLOR keeps ANSI escapes out of the
     * plain-text log view.
     */
    fun environment(context: Context): Map<String, String> = mapOf(
        "HOME" to context.filesDir.absolutePath,
        "TMPDIR" to context.cacheDir.absolutePath,
        "PATH" to listOf(
            binDir(context).absolutePath,
            context.applicationInfo.nativeLibraryDir,
            "/system/bin",
            "/system/xbin",
            "/apex/com.android.runtime/bin",
        ).joinToString(":"),
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

    /**
     * Copies a directory tree and verifies every file arrived with the same
     * size, used when the workspace moves to app-external storage. Any error
     * fails the whole copy so the caller can keep the old location.
     */
    fun copyTree(source: File, target: File): Boolean {
        if (!source.isDirectory) {
            return true
        }
        if (!target.isDirectory && !target.mkdirs()) {
            return false
        }
        for (entry in source.listFiles() ?: return true) {
            val destination = File(target, entry.name)
            if (entry.isDirectory) {
                if (!copyTree(entry, destination)) {
                    return false
                }
                continue
            }
            val copied = runCatching { entry.copyTo(destination, overwrite = true) }.getOrNull()
            if (copied == null || copied.length() != entry.length()) {
                return false
            }
        }
        return true
    }

    /** Deletes a directory tree; used once a workspace move has been verified. */
    fun deleteTree(root: File) {
        root.listFiles()?.forEach { entry ->
            if (entry.isDirectory) {
                deleteTree(entry)
            } else {
                entry.delete()
            }
        }
        root.delete()
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
