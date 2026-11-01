plugins {
    id("com.android.application") version "9.3.2"
}

android {
    namespace = "com.groovesync.android"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.groovesync.android"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.work:work-runtime-ktx:2.9.1")
}
