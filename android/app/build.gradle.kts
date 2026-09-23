import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

// FCM is optional: the plugin is applied only when a google-services.json is present.
if (file("google-services.json").exists()) {
    apply(plugin = "com.google.gms.google-services")
}

// Per-developer overrides (API base URL, feed pack path) without editing this file.
val localProperties = Properties().apply {
    val f = rootProject.file("local.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}

fun prop(key: String, default: String): String =
    (localProperties.getProperty(key)
        ?: project.findProperty(key)?.toString()
        ?: default)

android {
    namespace = "com.afghanistan.news"
    compileSdk = 36
    // Pinned so a build never silently auto-provisions a different toolchain revision.
    buildToolsVersion = "36.0.0"

    defaultConfig {
        applicationId = "com.afghanistan.news"
        // §396: Android 5.0 must keep working — this floor is an acceptance criterion.
        minSdk = 21
        targetSdk = 36
        versionCode = 2
        versionName = "1.1.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        vectorDrawables.useSupportLibrary = true
        resourceConfigurations += listOf("en", "fa", "ps")

        // Both ABIs ship in one APK: 32-bit devices still matter in the Afghan market.
        ndk { abiFilters += listOf("armeabi-v7a", "arm64-v8a") }

        buildConfigField("String", "API_BASE_URL", "\"${prop("apiBaseUrl", "https://api.afghanistan.news/")}\"")
        buildConfigField("String", "FEED_PACK_ASSET", "\"feedpack/afghanistan-global-news-master-v0.2.opml\"")
        buildConfigField("boolean", "PUSH_ENABLED", "true")
    }

    /**
     * Release signing (§release engineering).
     *
     * Order of preference: environment variables (CI), then android/keystore.properties (a local
     * release machine), then the debug key so that `assembleRelease` never breaks for someone who
     * just cloned the repository. A build signed with the debug key is installable but not
     * publishable — the log line below says which one you got.
     */
    // `Properties` is imported at the top: inside android {}, `java` is Gradle's extension.
    val keystoreProperties = Properties()
    run {
        val file = rootProject.file("keystore.properties")
        if (file.exists()) {
            file.inputStream().use { stream -> keystoreProperties.load(stream) }
        }
    }
    fun signingValue(key: String, envName: String): String? =
        System.getenv(envName)?.takeIf { it.isNotBlank() }
            ?: keystoreProperties.getProperty(key)?.takeIf { it.isNotBlank() }

    signingConfigs {
        create("release") {
            val storePath = signingValue("storeFile", "ANDROID_KEYSTORE_FILE")
            if (storePath != null) {
                storeFile = rootProject.file(storePath)
                storePassword = signingValue("storePassword", "ANDROID_KEYSTORE_PASSWORD")
                keyAlias = signingValue("keyAlias", "ANDROID_KEY_ALIAS")
                keyPassword = signingValue("keyPassword", "ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
            isMinifyEnabled = false
            buildConfigField("String", "API_BASE_URL", "\"${prop("apiBaseUrl", "http://10.0.2.2:8080/")}\"")
        }
        release {
            // R8 needs roughly 1.5 GB for this dependency graph (Compose + Firebase + Room).
            // On a small build host pass -PminifyRelease=false to produce a working, signed,
            // unshrunk APK instead of an out-of-memory failure. Default stays true: a release
            // APK should be shrunk.
            val minify = prop("minifyRelease", "true").toBoolean()
            isMinifyEnabled = minify
            isShrinkResources = minify
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            val releaseSigning = signingConfigs.getByName("release")
            if (releaseSigning.storeFile?.exists() == true) {
                signingConfig = releaseSigning
                logger.lifecycle("release build: signed with the project keystore (${releaseSigning.storeFile?.name})")
            } else {
                signingConfig = signingConfigs.getByName("debug")
                logger.lifecycle("release build: NO keystore found — falling back to the debug key (installable, not publishable)")
            }
        }
    }

    compileOptions {
        // java.time, streams and Optional on API 21 through core library desugaring.
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    packaging {
        resources.excludes += setOf(
            "/META-INF/{AL2.0,LGPL2.1}",
            "META-INF/DEPENDENCIES",
            "META-INF/LICENSE*",
        )
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }

    lint {
        // The compatibility gate: anything newer than API 21 is a build failure.
        abortOnError = true
        checkReleaseBuilds = true
        disable += setOf("GradleDependency", "OldTargetApi")
    }
}

ksp {
    // Room emits the schema next to the module so schema changes are reviewable in diffs.
    arg("room.schemaLocation", "$projectDir/schemas")
    arg("room.incremental", "true")
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
        freeCompilerArgs.add("-opt-in=kotlin.RequiresOptIn")
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.appcompat)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.navigation.compose)

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    // Only the core icon set (ArrowBack, Share, ...). The two bookmark glyphs the reading
    // screen needs are bundled as vector drawables instead of pulling in
    // material-icons-extended, which would add ~30k methods to the dex for two icons.
    implementation(libs.compose.material.icons.core)
    implementation(libs.compose.foundation)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    implementation(libs.androidx.room.paging)
    ksp(libs.androidx.room.compiler)

    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.androidx.paging.runtime)
    implementation(libs.androidx.paging.compose)
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.androidx.browser)
    implementation(libs.androidx.swiperefresh)
    implementation(libs.androidx.exifinterface)

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)
    implementation(libs.hilt.navigation.compose)
    implementation(libs.hilt.work)
    ksp(libs.hilt.work.compiler)

    implementation(libs.retrofit)
    implementation(libs.retrofit.moshi)
    implementation(libs.okhttp)
    implementation(libs.okhttp.logging)
    implementation(libs.moshi)
    implementation(libs.moshi.kotlin)
    ksp(libs.moshi.codegen)

    implementation(libs.coil.compose)
    implementation(libs.coil.okhttp)

    // Only the messaging SDK: no analytics, no crash-reporting SDKs (§65 — collect nothing
    // that is not needed to deliver the product).
    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.messaging)

    implementation(libs.kotlinx.coroutines.android)
    coreLibraryDesugaring(libs.desugar.jdk.libs)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.turbine)
    testImplementation(libs.mockk)
    testImplementation(libs.androidx.room.runtime)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.espresso.core)
}

// The bundled OPML feed pack is the bootstrap registry. Keep the asset byte-identical to
// the canonical pack in ../feedpacks and let CI verify the checksum.
val syncFeedPack by tasks.registering(Copy::class) {
    val canonical = rootProject.file("../feedpacks/afghanistan-global-news-master-v0.2.opml")
    onlyIf { canonical.exists() }
    from(canonical)
    into(layout.projectDirectory.dir("src/main/assets/feedpack"))
}

tasks.named("preBuild") { dependsOn(syncFeedPack) }

// Guard: a debug build must never be able to point at the production API by accident.
tasks.register("checkApiBaseUrl") {
    val url = prop("apiBaseUrl", "")
    doLast {
        require(!url.contains("api.afghanistan.news")) {
            "Refusing to build a developer build against the production API; set apiBaseUrl in local.properties."
        }
    }
}
