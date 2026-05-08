plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.nw5w.graywolf"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.nw5w.graywolf"
        minSdk = 28
        targetSdk = 34
        versionCode = 1
        versionName = "0.0.1-pocb"
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    sourceSets {
        getByName("main") {
            kotlin.srcDirs("src/main/kotlin")
            jniLibs.srcDirs("src/main/jniLibs")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    packaging {
        // N1: keep the lib*.so packaging trick alive so the Go ELF is extracted.
        jniLibs.useLegacyPackaging = true
    }

    buildTypes {
        debug {
            isMinifyEnabled = false
        }
        release {
            // POC-B is debug-only; phase 6 wires release signing.
            isMinifyEnabled = false
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
}
