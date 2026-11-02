plugins { kotlin("multiplatform") version "2.3.21" }
kotlin {
    jvmToolchain(21)
    jvm()
    sourceSets {
        commonMain.dependencies { }
        commonTest.dependencies { implementation(kotlin("test")) }
    }
}
