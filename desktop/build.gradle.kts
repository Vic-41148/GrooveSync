@file:OptIn(org.jetbrains.compose.ExperimentalComposeLibrary::class)

plugins {
    application
    kotlin("jvm") version "2.3.21"
    kotlin("plugin.serialization") version "2.3.21"
    id("org.jetbrains.kotlin.plugin.compose") version "2.3.21"
    id("org.jetbrains.compose") version "1.9.3"
}
kotlin { jvmToolchain(21) }
application { mainClass.set("com.groovesync.client.MainKt") }
dependencies {
    implementation(project(":common"))
    testImplementation(kotlin("test"))
    testImplementation(compose.uiTest)
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2")
    implementation("app.cash.sqldelight:sqlite-driver:2.4.1")
    implementation(compose.desktop.currentOs)
    implementation("com.github.hypfvieh:dbus-java-core:5.2.2")
    implementation("com.github.hypfvieh:dbus-java-transport-native-unixsocket:5.2.2")
    runtimeOnly("org.slf4j:slf4j-nop:2.0.16")
}

tasks.register<JavaExec>("mirrorBenchmark") {
    group = "verification"
    description = "Runs the local library mirror benchmark with a 50k-track fixture."
    classpath = sourceSets["main"].runtimeClasspath
    mainClass.set("com.groovesync.client.MirrorBenchmarkKt")
}
