plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}
android {
    namespace = "jp.kisaxi.bridge"
    compileSdk = 34
    defaultConfig {
        applicationId = "jp.kisaxi.bridge"
        minSdk = 26
        targetSdk = 34
        // CI の実行番号を versionCode にして、毎回「新しい版」として上書き更新できるようにする
        versionCode = System.getenv("GITHUB_RUN_NUMBER")?.toIntOrNull() ?: 1
        versionName = "0.1"
        ndk { abiFilters += "arm64-v8a" }
    }
    // 固定の署名キー (リポジトリ同梱の debug 用)。CI のビルドごとにキーが変わると、アンインストールしないと更新できなくなる
    signingConfigs {
        getByName("debug") {
            storeFile = file("kisaxi-debug.keystore")
            storePassword = "android"
            keyAlias = "androiddebugkey"
            keyPassword = "android"
        }
    }
    packaging { jniLibs { useLegacyPackaging = true } } // libkisaxi.so を実行ファイルとして展開させる
    kotlinOptions { jvmTarget = "17" }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
