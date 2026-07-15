plugins {
    java
}

repositories {
    mavenCentral()
}

dependencies {
    implementation("com.google.code.gson:gson:2.11.0")
    testImplementation("org.junit.jupiter:junit-jupiter:5.10.2")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

// Build with the JDK that runs Gradle (JDK 25 is installed) but emit Java-21
// bytecode via --release, so the eventual mod loads on a Java-21 server without
// forcing a toolchain download. Do NOT add a java.toolchain block: only JDK 25
// is installed, so a toolchain pinned to 21 would trigger an auto-provision
// download (or fail offline).
tasks.withType<JavaCompile>().configureEach {
    options.release.set(21)
}

tasks.test {
    useJUnitPlatform()
    testLogging { events("passed", "failed", "skipped") }
}
