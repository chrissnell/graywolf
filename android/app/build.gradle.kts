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

val jniLibsDir = file("src/main/jniLibs")
val repoRoot = rootProject.projectDir.parentFile  // android/.. = repo root

val cargoNdkBuild by tasks.registering(Exec::class) {
    group = "build"
    description = "Cross-compile graywolf-modem cdylib for Android via cargo-ndk."
    workingDir = repoRoot
    // Declare inputs/outputs so Gradle's UP-TO-DATE check skips the
    // ~10-30s cargo-ndk launch when nothing changed. cargo's own cache
    // is fast no-op, but Gradle launching the process at all is the
    // long pole on incremental builds.
    inputs.dir(repoRoot.resolve("graywolf-modem/src"))
    inputs.file(repoRoot.resolve("graywolf-modem/Cargo.toml"))
    inputs.file(repoRoot.resolve("Cargo.lock"))
    outputs.dir(jniLibsDir)
    commandLine = listOf(
        "cargo", "ndk",
        "-t", "arm64-v8a",
        "-t", "x86_64",
        "-P", "26",
        "-o", jniLibsDir.absolutePath,
        "build", "--lib", "--release",
        "--manifest-path", "graywolf-modem/Cargo.toml",
    )
}

// armv7 dropped: Go's android/arm target requires cgo + NDK C
// toolchain, and minSdk=28 leaves zero realistic armv7 devices.
//
// android/amd64 also requires cgo (Go's internal linker doesn't
// support it), so x86_64 carries an NDK clang wrapper. arm64
// stays on the internal linker (CGO_ENABLED=0).
data class GoAbi(val goarch: String, val cgo: Boolean, val ccTriple: String?)
val goAbiMatrix = mapOf(
    "arm64-v8a" to GoAbi("arm64", cgo = false, ccTriple = null),
    "x86_64"    to GoAbi("amd64", cgo = true,  ccTriple = "x86_64-linux-android"),
)

// API level for cgo's clang wrapper. Matches minSdk so the Go-built
// .so floor lines up with the AndroidManifest.
val ndkCgoApi = 28

val goCrossCompile by tasks.registering {
    group = "build"
    description = "Cross-compile cmd/graywolf-pocb to libgraywolf.so for each Android ABI."
}

goAbiMatrix.forEach { (abi, info) ->
    val taskName = "goCrossCompile_$abi"
    val task = tasks.register<Exec>(taskName) {
        group = "build"
        workingDir = repoRoot
        environment("GOOS", "android")
        environment("GOARCH", info.goarch)
        environment("CGO_ENABLED", if (info.cgo) "1" else "0")
        environment("GOWORK", "off")
        if (info.cgo) {
            // Resolve the NDK clang wrapper for this ABI. ANDROID_NDK_ROOT
            // (or ANDROID_NDK_HOME) must be set; check-android-toolchain.sh
            // enforces this in preBuild.
            val ndkRoot = System.getenv("ANDROID_NDK_ROOT")
                ?: System.getenv("ANDROID_NDK_HOME")
                ?: error("ANDROID_NDK_ROOT/ANDROID_NDK_HOME unset; needed for $abi cgo")
            val hostTag = when {
                org.gradle.internal.os.OperatingSystem.current().isMacOsX  -> "darwin-x86_64"
                org.gradle.internal.os.OperatingSystem.current().isLinux   -> "linux-x86_64"
                org.gradle.internal.os.OperatingSystem.current().isWindows -> "windows-x86_64"
                else -> error("Unsupported host OS for NDK toolchain")
            }
            val clang = file("$ndkRoot/toolchains/llvm/prebuilt/$hostTag/bin/${info.ccTriple}$ndkCgoApi-clang")
            environment("CC", clang.absolutePath)
        }
        val outDir = jniLibsDir.resolve(abi)
        // Inputs: every Go source under cmd/graywolf-pocb + the module files
        // every package transitively reaches. fileTree("pkg") + go.{mod,sum}
        // is conservative but cheap to fingerprint, and Gradle's UP-TO-DATE
        // check then skips the Go process launch on no-op iterations.
        inputs.files(fileTree(repoRoot.resolve("cmd/graywolf-pocb")) {
            include("**/*.go")
        })
        inputs.files(fileTree(repoRoot.resolve("pkg")) { include("**/*.go") })
        inputs.file(repoRoot.resolve("go.mod"))
        inputs.file(repoRoot.resolve("go.sum"))
        outputs.file(outDir.resolve("libgraywolf.so"))
        doFirst { outDir.mkdirs() }
        commandLine = listOf(
            "go", "build",
            "-o", outDir.resolve("libgraywolf.so").absolutePath,
            "./cmd/graywolf-pocb",
        )
    }
    goCrossCompile.configure { dependsOn(task) }
}

tasks.named("preBuild") {
    dependsOn(cargoNdkBuild)
    dependsOn(goCrossCompile)
}

val checkAndroidToolchain by tasks.registering(Exec::class) {
    group = "verification"
    description = "Verify Android NDK / cargo-ndk / Go / JDK toolchain."
    workingDir = repoRoot
    commandLine = listOf("./scripts/check-android-toolchain.sh")
}

tasks.named("preBuild") {
    dependsOn(checkAndroidToolchain)
}
