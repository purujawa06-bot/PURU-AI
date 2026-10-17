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
- The binary is cross-built as `GOOS=android` **with cgo** (the Android NDK
  compiler). Android has no `/etc/resolv.conf`, so the pure-Go resolver cannot
  do DNS there (Go's own `net/conf.go` says "DNS requests don't work on Android,
  so prefer the cgo resolver"); the cgo build resolves names through
  bionic/`getaddrinfo`, exactly like any other Android app.
- `PuruRuntime` gives the child process the environment Android needs:
  `HOME` inside app storage (so `~/.puru` lands there), `PATH` reaching
  `/system/bin/sh` (the exec tool runs `sh -c`), `SSL_CERT_DIR` pointing at the
  system certificate stores, and `NO_COLOR=1` so the log view shows plain text
  instead of ANSI escapes.
- Saving the form runs `puru setup --force` with one environment variable per
  row (`TELEGRAM_BOT_TOKEN`, `PURU_BASE_URL`, `PURU_MODEL`, `PURU_API_KEY`,
  `PURU_WORKSPACE`, `PURU_ALLOWED_USERS`, `PURU_TEMPERATURE`,
  `PURU_RESTRICT_WORKSPACE`, `PURU_TIMEZONE`, `PURU_MAX_ITERATIONS`,
  `PURU_HISTORY_TOKEN_LIMIT`, `PURU_TOOLS_PREVIEW`, `PURU_LOOP_DELAY_SECONDS`,
  `PURU_EXEC_MEMORY_MB`, `PURU_SKILLS_MODE`, `PURU_SKILLS_ALLOW`,
  `PURU_WEBSEARCH_AISTUDIO_*`, `PURU_WEBSEARCH_EXA_*`). The Go binary remains the
  only writer of `config.json`, and a row left empty keeps the current value or
  the wizard default.
- Start saves the form, then starts `PuruService`, which runs
  `puru gateway --config <filesDir>/.puru/config.json` and pipes its output into
  `<filesDir>/puru.log`. Stop sends SIGTERM, so puru cancels its context and
  reaps its own child processes.

## Config form

Rows are declared in one table in `MainActivity.kt` (label, env variable, hint,
required flag, advanced flag), and the UI is built from it, so the form and the
wizard cannot drift apart:

- **Required** rows (red label) are validated before saving: bot token, model
  base URL, model name. `puru setup` refuses to write them empty anyway.
- **Optional** rows are grey; an empty field is not passed to `puru setup`, so
  the value already in `config.json` (or puru's default, shown in the hint)
  stays. Yes/no rows are always written.
- **Advanced settings** stay collapsed behind a checkbox: runtime knobs
  (timezone, iterations, history limit, tool preview, loop delay, shell memory),
  skills, and web search providers.
- **Import config.json** (below Save) opens the Android file picker, so a config
  from this laptop, another phone, or a backup can be dropped straight in. The
  picked file is copied to `<filesDir>/.puru/import.json`, `puru setup --force`
  validates and normalizes it there, and only then is it moved over
  `config.json` — a broken file never destroys the working setup. A config from
  another machine can carry a `workspace` path that does not exist on the phone,
  so check the Workspace row (its hint shows the phone default) before starting.


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
# Needs the Android NDK: the binary must be built for Android with cgo so DNS
# works (see "How it works" above).
NDK="$ANDROID_NDK_HOME"   # e.g. /usr/local/lib/android/sdk/ndk/27.3.13750724
TC="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"
CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC="$TC/aarch64-linux-android21-clang" \
  go build -trimpath -ldflags="-s -w" \
  -o android/app/src/main/jniLibs/arm64-v8a/libpuru.so ./cmd/puru
CGO_ENABLED=1 GOOS=android GOARCH=arm GOARM=7 CC="$TC/armv7a-linux-androideabi21-clang" \
  go build -trimpath -ldflags="-s -w" \
  -o android/app/src/main/jniLibs/armeabi-v7a/libpuru.so ./cmd/puru
cd android
gradle assembleDebug        # or assembleRelease
```

Requirements: JDK 17, Android SDK platform 35 (set `ANDROID_HOME`), Android NDK,
Gradle 8.10+. Without a keystore configured, `assembleRelease` falls back to the
debug key.

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
