plugins { kotlin("multiplatform") version "2.3.21" }
kotlin {
    jvm()
    sourceSets {
        commonMain.dependencies { }
        commonTest.dependencies { implementation(kotlin("test")) }
    }
}
