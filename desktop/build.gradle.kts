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
    implementation("app.cash.sqldelight:sqlite-driver:2.4.1")
}

tasks.register<JavaExec>("mirrorBenchmark") {
    group = "verification"
    description = "Runs the local library mirror benchmark with a 50k-track fixture."
    classpath = sourceSets["main"].runtimeClasspath
    mainClass.set("com.groovesync.client.MirrorBenchmarkKt")
}
