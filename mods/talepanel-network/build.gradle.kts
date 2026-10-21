plugins {
    java
}

// Pinned Hytale server build to compile against. The server is provided at
// runtime, so it is compileOnly; bump it when a new release changes the API.
val hytaleServerVersion = "0.6.8"

repositories {
    mavenCentral()
    maven("https://maven.hytale.com/release")
}

dependencies {
    compileOnly("com.hypixel.hytale:Server:$hytaleServerVersion")
    // Gson ships inside HytaleServer.jar, so the mod must not bundle its own.
    compileOnly("com.google.code.gson:gson:2.11.0")
    testImplementation("com.google.code.gson:gson:2.11.0")
    testImplementation("org.junit.jupiter:junit-jupiter:5.10.2")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

// Hytale servers require Java 25 and HytaleServer.jar is Java 25 bytecode, so
// compile for 25. Build with JAVA_HOME pointing at a JDK 25.
tasks.withType<JavaCompile>().configureEach {
    options.release.set(25)
}

tasks.jar {
    archiveFileName.set("talepanel-network.jar")
}

tasks.test {
    useJUnitPlatform()
    testLogging { events("passed", "failed", "skipped") }
}
