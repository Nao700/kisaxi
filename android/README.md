# KISAXI Android Bluetooth ブリッジ (試作・未ビルド)

Android では `/dev/rfcomm*` が使えないため、この小さなアプリが Bluetooth (SPP) を受け持ち、KISS を `127.0.0.1:8075` の TCP に中継する。
KISAXI 本体 (Go) は TNC 種別「KISS Bluetooth」で、`127.0.0.1:8074` の HTTP (`GET /devices` / `POST /connect?addr=`) を呼んで接続する。

使い方: Android の設定で無線機をペアリング → アプリで「起動して管理画面を開く」→ 管理画面の設定で TNC 種別「KISS Bluetooth」を選び、一覧から機器を選んで保存。
Tailscale 越しに見る場合は、別端末のブラウザで `http://<端末のtailnet IP>:8073` を開く。管理画面は無認証なので LAN には出さないこと。

ビルド: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -o android/app/src/main/jniLibs/arm64-v8a/libkisaxi.so ./src` のあと、Android Studio / Gradle で APK を作る。
**この環境には Android SDK がなく、APK のビルドも実機での動作確認も未実施。**
※ kisaxi-debug.keystore は debug 用 (パスワード android)。配布用の署名には使わないこと。
