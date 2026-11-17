plugins {
    kotlin("multiplatform") version "2.3.21"
    kotlin("plugin.serialization") version "2.3.21"
    id("org.jetbrains.kotlin.plugin.compose") version "2.3.21"
    id("org.jetbrains.compose") version "1.9.3"
    id("com.android.kotlin.multiplatform.library") version "9.3.2"
    id("app.cash.sqldelight") version "2.4.1"
}

kotlin {
    jvmToolchain(21)
    androidLibrary {
        namespace = "com.groovesync.common"
        compileSdk = 36
        minSdk = 26
    }
    jvm()
    sourceSets {
        commonMain.dependencies {
            api("app.cash.sqldelight:runtime:2.4.1")
            api("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
            implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2")
            implementation(compose.runtime)
            implementation(compose.foundation)
            implementation(compose.material3)
            implementation(compose.ui)
        }
        commonTest.dependencies { implementation(kotlin("test")) }
        jvmTest.dependencies { implementation("app.cash.sqldelight:sqlite-driver:2.4.1") }
    }
}

sqldelight {
    databases {
        create("GrooveDb") {
            packageName.set("com.groovesync.common.mirror")
        }
    }
}
