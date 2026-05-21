import com.google.protobuf.gradle.proto
import java.util.Base64

// Read version metadata from the repo-root VERSION file so Android stays
// in sync with `make bump-point`/`bump-minor`. Format: "X.Y.Z" with an
// optional "-pre" suffix that we drop for versionCode (Play needs a pure
// integer) but keep for versionName.
val graywolfVersionName: String = run {
    val versionFile = rootProject.projectDir.parentFile.resolve("VERSION")
    require(versionFile.exists()) { "VERSION file not found at ${versionFile.absolutePath}" }
    versionFile.readText().trim()
}
val graywolfVersionCode: Int = run {
    val core = graywolfVersionName.substringBefore('-')
    val parts = core.split(".")
    require(parts.size == 3) { "VERSION must be X.Y.Z; got '$graywolfVersionName'" }
    val (major, minor, patch) = parts.map { it.toInt() }
    major * 1_000_000 + minor * 10_000 + patch * 100
}

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("com.google.protobuf")
}

android {
    namespace = "com.nw5w.graywolf"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.nw5w.graywolf"
        minSdk = 28
        targetSdk = 36
        versionCode = graywolfVersionCode
        versionName = graywolfVersionName
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

    // Release signing reads from env vars so CI can inject secrets without
    // a committed keystore. Local release builds: export GRAYWOLF_KEYSTORE_PATH
    // + GRAYWOLF_KEYSTORE_PASSWORD before ./gradlew assembleRelease|bundleRelease.
    // CI passes GRAYWOLF_KEYSTORE_BASE64 (decoded inline) instead.
    val keystorePath = System.getenv("GRAYWOLF_KEYSTORE_PATH")
    val keystoreBase64 = System.getenv("GRAYWOLF_KEYSTORE_BASE64")
    val keystorePassword = System.getenv("GRAYWOLF_KEYSTORE_PASSWORD")
    val keyAlias = System.getenv("GRAYWOLF_KEY_ALIAS") ?: "graywolf-upload"
    val keyPassword = System.getenv("GRAYWOLF_KEY_PASSWORD") ?: keystorePassword

    val resolvedKeystoreFile: java.io.File? = when {
        keystorePath != null -> file(keystorePath)
        keystoreBase64 != null -> {
            val tmp = layout.buildDirectory.file("upload.keystore").get().asFile
            tmp.parentFile.mkdirs()
            tmp.writeBytes(Base64.getDecoder().decode(keystoreBase64))
            tmp
        }
        else -> null
    }

    signingConfigs {
        if (resolvedKeystoreFile != null && keystorePassword != null) {
            create("release") {
                storeFile = resolvedKeystoreFile
                storePassword = keystorePassword
                this.keyAlias = keyAlias
                this.keyPassword = keyPassword!!
            }
        }
    }

    buildTypes {
        debug {
            isMinifyEnabled = false
        }
        release {
            isMinifyEnabled = false
            // No keystore env -> leave signingConfig unset; assembleRelease
            // emits an unsigned APK. PR-build CI never sees secrets and
            // stays green; tag builds inject the keystore env to sign.
            signingConfigs.findByName("release")?.let { signingConfig = it }
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
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.mockito:mockito-core:5.11.0")
    testImplementation("org.mockito.kotlin:mockito-kotlin:5.2.1")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.8.1")
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

// Build the SPA bundle (web/dist) via vite before goCrossCompile picks
// it up for go:embed. web/dist is gitignored; without this task an APK
// built from a clean checkout (or after web/src changes) silently ships
// whatever the developer last vite-built. The login-screen-on-Android
// regression triaged on 2026-05-21 was exactly that bug. Inputs cover
// every file the SPA build reads; outputs are the dist directory.
val webBuild by tasks.registering(Exec::class) {
    group = "build"
    description = "Build the SPA bundle (web/dist) via vite. Required before goCrossCompile."
    workingDir = repoRoot.resolve("web")
    inputs.dir(repoRoot.resolve("web/src"))
    inputs.dir(repoRoot.resolve("web/themes"))
    inputs.dir(repoRoot.resolve("web/public"))
    inputs.file(repoRoot.resolve("web/index.html"))
    inputs.file(repoRoot.resolve("web/package.json"))
    inputs.file(repoRoot.resolve("web/package-lock.json"))
    inputs.file(repoRoot.resolve("web/vite.config.js"))
    inputs.file(repoRoot.resolve("web/svelte.config.js"))
    outputs.dir(repoRoot.resolve("web/dist"))
    commandLine = listOf("npx", "vite", "build")
}

val goCrossCompile by tasks.registering {
    group = "build"
    description = "Cross-compile cmd/graywolf to libgraywolf.so for each Android ABI."
}

goAbiMatrix.forEach { (abi, info) ->
    val taskName = "goCrossCompile_$abi"
    val task = tasks.register<Exec>(taskName) {
        // Ensure web/dist is fresh before Go embeds it. Without this
        // dependency, a developer who edits web/src/ but forgets to
        // run vite build ships a stale SPA bundle inside libgraywolf.so.
        dependsOn(webBuild)
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
            //
            // .takeUnless { it.isNullOrBlank() } rejects both "unset" and
            // "set to empty string". GitHub Actions hit the latter on
            // 2026-05-21 when the workflow used `env: ANDROID_NDK_ROOT:
            // ${{ env.ANDROID_NDK_HOME }}` (evaluated to "" because
            // setup-android exports the var at shell level, not workflow
            // context). A raw `?:` accepted "" and produced a broken
            // clang path "/toolchains/...".
            val ndkRoot = System.getenv("ANDROID_NDK_ROOT").takeUnless { it.isNullOrBlank() }
                ?: System.getenv("ANDROID_NDK_HOME").takeUnless { it.isNullOrBlank() }
                ?: error("ANDROID_NDK_ROOT/ANDROID_NDK_HOME unset or blank; needed for $abi cgo")
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
        // Inputs: every Go source under cmd/graywolf + the module files
        // every package transitively reaches. fileTree("pkg") + go.{mod,sum}
        // is conservative but cheap to fingerprint, and Gradle's UP-TO-DATE
        // check then skips the Go process launch on no-op iterations.
        //
        // web/dist must be in the input set too: cmd/graywolf imports
        // pkg/web which embeds web/dist via go:embed. Without this entry,
        // Gradle's UP-TO-DATE check would skip the Go build when only the
        // SPA bundle changes, shipping a stale embed inside libgraywolf.so.
        inputs.files(fileTree(repoRoot.resolve("cmd/graywolf")) {
            include("**/*.go")
        })
        inputs.files(fileTree(repoRoot.resolve("pkg")) { include("**/*.go") })
        inputs.files(fileTree(repoRoot.resolve("web/dist")))
        inputs.files(fileTree(repoRoot.resolve("web")) {
            include("embed.go")
        })
        inputs.file(repoRoot.resolve("go.mod"))
        inputs.file(repoRoot.resolve("go.sum"))
        outputs.file(outDir.resolve("libgraywolf.so"))
        doFirst { outDir.mkdirs() }
        commandLine = listOf(
            "go", "build",
            "-o", outDir.resolve("libgraywolf.so").absolutePath,
            "./cmd/graywolf",
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
