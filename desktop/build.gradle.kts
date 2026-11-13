plugins {
    application
    kotlin("jvm") version "2.3.21"
    kotlin("plugin.serialization") version "2.3.21"
}
kotlin { jvmToolchain(21) }
application { mainClass.set("com.groovesync.client.MainKt") }
dependencies {
    implementation(project(":common"))
    testImplementation(kotlin("test"))
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
}
