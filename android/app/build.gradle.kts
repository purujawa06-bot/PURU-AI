plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// APK version/code come from the environment so CI can stamp a release tag.
val versionNameOverride: String = System.getenv("PURU_VERSION_NAME") ?: "1.0"
val versionCodeOverride: Int = (System.getenv("PURU_VERSION_CODE") ?: "1").toInt()

android {
    namespace = "com.purujawa.puru.launcher"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.purujawa.puru.launcher"
        minSdk = 26
        // API 28 on purpose, the same trade-off Termux makes:
        //  - requestLegacyExternalStorage is still honoured, so the agent can
        //    read, write and delete real files under /sdcard (scoped storage
        //    for targetSdk 30+ cannot be bypassed without root, and "All files
        //    access" does not exist on Android 10 — the device this targets).
        //  - apps targeting API 29+ may not execute files from their own data
        //    dir, which would make `puru fetch`-installed tools unusable.
        // Google Play would reject this target; this launcher is sideloaded.
        targetSdk = 28
        versionCode = versionCodeOverride
        versionName = versionNameOverride
    }

    // The bundled puru binary is placed in jniLibs/<abi>/libpuru.so. Legacy
    // packaging extracts it to nativeLibraryDir with exec permission, the only
    // location apps targeting API 29+ may execute from.
    packaging {
        jniLibs {
            useLegacyPackaging = true
            keepDebugSymbols += "**/libpuru.so"
        }
    }

    signingConfigs {
        create("release") {
            val storePath = System.getenv("PURU_KEYSTORE")
            if (!storePath.isNullOrBlank()) {
                storeFile = file(storePath)
                storePassword = System.getenv("PURU_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("PURU_KEY_ALIAS")
                keyPassword = System.getenv("PURU_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // Debuggable on purpose: this launcher is sideloaded and support
            // happens over adb, where `adb shell run-as <pkg> …` is the only way
            // to inspect the app's own storage, run the bundled binary with the
            // app's network policy, or read a crash. It is not published on any
            // store, and USB debugging is already required to install it.
            isDebuggable = true
            // Without a configured keystore the debug key is used, so a plain
            // `gradle assembleRelease` still produces an installable APK.
            signingConfig = if (System.getenv("PURU_KEYSTORE").isNullOrBlank()) {
                signingConfigs.getByName("debug")
            } else {
                signingConfigs.getByName("release")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
}
