plugins {
    id("com.android.application") version "9.3.2"
    kotlin("plugin.serialization") version "2.3.21"
    id("org.jetbrains.kotlin.plugin.compose") version "2.3.21"
    id("io.github.takahirom.roborazzi") version "1.77.0"
}

android {
    namespace = "com.groovesync.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.groovesync.android"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        buildConfigField("boolean", "ALLOW_SEED", "false")
        // Store artefacts are built with -Pgroovesync.store=true and must not
        // offer the YouTube provider. GitHub Release artefacts keep it.
        val storeBuild = providers.gradleProperty("groovesync.store")
            .map { it.toBoolean() }
            .getOrElse(false)
        buildConfigField("boolean", "YOUTUBE_ENABLED", (!storeBuild).toString())
    }

    buildTypes {
        getByName("debug") {
            buildConfigField("boolean", "ALLOW_SEED", "true")
        }
        create("benchmark") {
            initWith(getByName("release"))
            signingConfig = signingConfigs.getByName("debug")
            matchingFallbacks += "release"
            buildConfigField("boolean", "ALLOW_SEED", "true")
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.documentfile:documentfile:1.1.0")
    implementation(platform("androidx.compose:compose-bom:2024.10.01"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material:material-icons-core")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.work:work-runtime-ktx:2.9.1")
    implementation("androidx.navigation:navigation-compose:2.8.3")
    implementation("androidx.media3:media3-exoplayer:1.4.1")
    implementation("androidx.media3:media3-session:1.4.1")
    implementation("androidx.profileinstaller:profileinstaller:1.4.1")
    implementation("app.cash.sqldelight:android-driver:2.4.1")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
    implementation(project(":common"))

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.robolectric:robolectric:4.13")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("io.github.takahirom.roborazzi:roborazzi:1.77.0")
    testImplementation("io.github.takahirom.roborazzi:roborazzi-compose:1.77.0")
    androidTestImplementation("androidx.compose.ui:ui-test-junit4:1.7.5")
    androidTestImplementation("io.github.takahirom.roborazzi:roborazzi:1.77.0")
    androidTestImplementation("io.github.takahirom.roborazzi:roborazzi-compose:1.77.0")
}
