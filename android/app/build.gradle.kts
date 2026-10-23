plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "org.phorepoc.phesms"
    compileSdk = 34
    defaultConfig {
        applicationId = "org.phorepoc.phesms"
        minSdk = 26
        targetSdk = 34
        versionCode = 1
        versionName = "0.1"
    }
    buildTypes {
        release { isMinifyEnabled = false }
    }
    sourceSets {
        getByName("main").java.srcDirs("src/main/kotlin")
        getByName("test").java.srcDirs("src/test/kotlin")
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    lint { abortOnError = false }
}

dependencies {
    // no libraries at run time: the platform's JobScheduler, SmsManager, HttpURLConnection and org.json
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")   // the platform's org.json is a stub in JVM tests
}
