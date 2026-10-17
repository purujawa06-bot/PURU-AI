# Android ABI binaries

`puru` is bundled per ABI as `libpuru.so` so Android extracts it into the app's
native library directory, the only place an app targeting API 29+ may execute
a file from.

The files are build output and are not committed. Create them before building:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w" -o android/app/src/main/jniLibs/arm64-v8a/libpuru.so ./cmd/puru

CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath \
  -ldflags="-s -w" -o android/app/src/main/jniLibs/armeabi-v7a/libpuru.so ./cmd/puru
```

The `Android APK` workflow does exactly this before running Gradle, so CI
always ships a launcher matching the commit it was built from.
