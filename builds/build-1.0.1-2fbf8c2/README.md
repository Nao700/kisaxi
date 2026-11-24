# KISAXI 1.0.1

KISAXI は APRS I-Gate ソフトウェアです。単体の実行ファイルで、ランタイムのインストールは不要です。
詳しい使い方は [リポジトリの README](https://github.com/Nao700/kisaxi#readme)、紹介ページは https://nao700.github.io/kisaxi/ を見てください。

## どれを使うか

| ファイル | 使う環境 |
|---|---|
| `KISAXI-windows-x64.exe` | Windows 10/11 (64bit) |
| `KISAXI-linux-x64` | Linux (x64) |
| `KISAXI-linux-arm64` | Raspberry Pi 3 / 4 / 5、Zero 2 W の 64bit OS (`uname -m` が `aarch64`) |
| `KISAXI-linux-armv7` | Raspberry Pi 3 / 4、Zero 2 W の 32bit OS |
| `KISAXI-linux-armv6` | Raspberry Pi Zero / Zero W (動作が不安定になる可能性があり、おすすめしません) |
| `KISAXI-all-v1.0.1.zip` | 上の 5 つをまとめたもの |

## 起動のしかた

- **Windows**: exe を好きなフォルダに置いて実行します。ブラウザで管理画面 (http://127.0.0.1:8073/) が開きます。署名していないため、SmartScreen の警告が出たら「詳細情報」→「実行」を選びます。
- **Linux / Raspberry Pi**: `chmod +x KISAXI-linux-arm64` のあと `./KISAXI-linux-arm64` を実行し、ブラウザで http://127.0.0.1:8073/ を開きます (ファイル名は環境に合わせてください)。シリアル TNC を使うときは `sudo usermod -aG dialout $USER` が必要です。

「設定」タブでコールサイン・パスコード・位置・TNC を設定し、「保存して適用」を押します。設定は実行ファイルと同じフォルダの `kisaxi.json` に保存されます。

## Ver.1.0.1 の変更点

- デジピータで UIflood (KNn-N など地域限定パス) と UItrace (TRACEn-N) の中継に対応 (エイリアスを設定で登録)
- APRS-IS から RF への位置情報の送出を、コールサイン名指し・ポジションコメントごとに設定できるように
- PHG 付きビーコンのコメント前に `/` を入れ、TM-D710 などでコメント先頭の 1 文字が欠けるのを解消
- シリアルポートを開けないときのエラーを日本語にし、ポート欄に直接入力できる旨を表示

## 注意

- APRS は双方向通信が原則です。受信専用の I-Gate の運用はお控えください。
- 管理画面には認証がありません。LAN の外へ公開しないでください。
