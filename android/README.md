# PURU-AI launcher for Android

Self-contained APK that runs `puru gateway` (the Telegram bot) on the phone: no
Termux, no root. The Kotlin app bundles the same Go binary the CLI releases
ship, starts it as a foreground service, and gives you a config form, start/stop
buttons, and a log view.

```
android/
  app/src/main/java/com/purujawa/puru/launcher/
    MainActivity.kt   config form, start/stop, log polling
    PuruService.kt    foreground service that owns `puru gateway`
    PuruRuntime.kt    paths, environment, and process helpers
  app/src/main/jniLibs/<abi>/libpuru.so   built binary (not committed)
```

## How it works

- The Go binary is packaged as `lib/<abi>/libpuru.so`, so Android extracts it
  into the app's native library directory. That is the only location an app
  targeting API 29+ may execute a file from; executables inside the writable
  data directory are blocked by the platform.
- `PuruRuntime` gives the child process the environment Android needs:
  `HOME` inside app storage (so `~/.puru` lands there), `PATH` reaching
  `/system/bin/sh` (the exec tool runs `sh -c`), and `SSL_CERT_DIR` pointing at
  the system certificate stores (a Go binary built for linux does not look
  there on its own).
- Saving the form runs `puru setup --force` with `TELEGRAM_BOT_TOKEN`,
  `PURU_BASE_URL`, `PURU_API_KEY`, `PURU_MODEL`, and `PURU_WORKSPACE`. The Go
  binary remains the only writer of `config.json`, and blank fields keep the
  current value or the wizard default.
- Start saves the form, then starts `PuruService`, which runs
  `puru gateway --config <filesDir>/.puru/config.json` and pipes its output into
  `<filesDir>/puru.log`. Stop sends SIGTERM, so puru cancels its context and
  reaps its own child processes.

App data (and therefore the bot memory and workspace) lives in the app's
private storage: `config.json` in `<filesDir>/.puru/`, the workspace in
`<filesDir>/.puru/workspace`, logs in `<filesDir>/puru.log`.

## Build

CI builds the APK on every change under `android/` or the Go sources:
**Actions → Android APK** (artifact `puru-launcher-apk`). The workflow is also
a reusable workflow: the Release workflow calls it for the new tag, so
`puru-launcher-<tag>.apk` also lands on the GitHub release (a tag pushed with
`GITHUB_TOKEN` cannot trigger workflows by itself). A manual run accepts a tag
input to stamp the version and attach the APK to that release.

Every run also force-pushes a short build report (APK contents, merged
manifest, Gradle tail) to the `debug-android-build` branch, because job logs
are not readable from outside without admin rights.

Locally:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" \
  -o android/app/src/main/jniLibs/arm64-v8a/libpuru.so ./cmd/puru
cd android
gradle assembleDebug        # or assembleRelease
```

Requirements: JDK 17, Android SDK platform 35 (set `ANDROID_HOME`), Gradle 8.10+.
Without a keystore configured, `assembleRelease` falls back to the debug key.

## Signing

The APK must be signed with a stable key, otherwise a later build cannot be
installed over an older one (Android rejects the different signature and your
app data would have to be wiped).

- CI generates a keystore on first use and caches it, which is enough for
  personal sideloading.
- For a permanent key, create one and store it as repository secrets:

```sh
keytool -genkeypair -v -keystore puru-launcher.jks -alias puru \
  -keyalg RSA -keysize 2048 -validity 36500
base64 -w0 puru-launcher.jks > puru-launcher.jks.base64
```

`ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`,
`ANDROID_KEY_PASSWORD` — the workflow picks them up automatically when present.

## Known limits

- `arm64-v8a` and `armeabi-v7a` only; there is no x86 build, so emulators are
  not supported.
- Android may still stop the service on aggressive battery-saving ROMs; the
  launch notification is the app's lifecycle, and the log shows whether the bot
  stayed up.
- The media/tool features that need extra binaries (`ffmpeg`, `git`, ...) are
  limited to what the phone ships in `/system/bin`.
