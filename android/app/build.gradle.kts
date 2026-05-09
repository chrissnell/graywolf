import com.google.protobuf.gradle.proto

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("com.google.protobuf")
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
            proto {
                srcDir("../../proto")
                // Filter: only platform.proto is consumed by the Android build.
                // graywolf.proto lacks `option java_package` and would land
                // in the default Java package matching its proto package
                // (`graywolf`), bloating the APK with unused IPC types.
                // `include("platform.proto")` is silently ignored on the
                // AGP-bridged proto SourceDirectorySet under
                // protobuf-gradle-plugin 0.9.4; explicit `exclude` works.
                exclude("graywolf.proto")
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

    testOptions {
        // android.util.Log etc. are not mocked under the host JVM. Default
        // values keeps the unit-test surface workable without dragging in
        // Robolectric for what are effectively pure-Kotlin protocol tests.
        unitTests.isReturnDefaultValues = true
    }

    buildFeatures {
        // AGP 8 disables BuildConfig generation by default. GraywolfService
        // reads BuildConfig.VERSION_NAME for the Hello frame's serverVersion.
        buildConfig = true
    }
}

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:3.25.3"
    }
    generateProtoTasks {
        all().configureEach {
            builtins {
                create("java") {
                    option("lite")
                }
            }
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("com.github.mik3y:usb-serial-for-android:3.10.0")
    implementation("com.google.protobuf:protobuf-javalite:3.25.3")

    testImplementation("junit:junit:4.13.2")
}
