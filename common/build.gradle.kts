plugins {
    kotlin("multiplatform") version "2.3.21"
    id("app.cash.sqldelight") version "2.4.1"
}

kotlin {
    jvmToolchain(21)
    jvm()
    sourceSets {
        commonMain.dependencies {
            api("app.cash.sqldelight:runtime:2.4.1")
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
