# KISAXIとは

APRS I-Gate ソフトウェアです。地図には OpenStreetMap を使います。
対応OSはWindows 10/11（互換モード不要）と Linux（x64 / Raspberry Pi）で動作し、ランタイムのインストールは不要な単体実行ファイルです。

## 機能

- **APRS-IS 接続**: サーバー・フィルター指定、切断時の自動再接続
- **TNC 接続 (RF)**
  - KISS シリアル（COM ポート / `/dev/ttyUSB0` など）。TNC を KISS モードに切り替えるコマンドの送信にも対応
  - KISS TCP（Direwolf、UZ7HO Soundmodem など）
- **I-Gate**
  - RF → APRS-IS: `qAR` を付けて送出。パスに `TCPIP` `TCPXX` `NOGATE` `RFONLY` を含むパケットとクエリは送らない。サードパーティパケットは中身を取り出して送る。30 秒以内の重複は送らない
  - APRS-IS → RF: 最近 RF で受信した局（ローカル局）宛のメッセージを、サードパーティ形式で RF へ送出。送信元がローカル局の場合は送らない。1 分あたりの件数に上限あり
- **ビーコン**: RF と APRS-IS それぞれ独立した間隔で送信。手動送信ボタンもあり
- **オブジェクト / アイテム**: 最大 10 件。設定タブで登録し、RF / APRS-IS / 両方に定期送信。座標は地図クリック・ドラッグ・直接入力・貼り付けで指定でき、送信パケットを直接編集することもできる（英数字・記号のみ）。削除すると削除通知を送信する
- **地図 (OpenStreetMap)**: APRS シンボル、コールサインのラベル、移動局の軌跡、ポップアップで詳細（距離・方位・速度・高度・コメント・生パケット）を表示
- **解析**: 通常の位置と圧縮位置、Mic-E、オブジェクト、アイテム、メッセージ、ステータス、NMEA、気象。Shift_JIS のコメントも文字化けせずに表示
- **局リスト**: 検索、RF / IS での絞り込み、距離表示。クリックで地図上へ移動
- **パケットモニター**: RF受信 / IS受信 / 送信 / ゲートを色分けし、絞り込みも可能
- **メッセージ**: 送受信、ACK の自動返信、再送（最大 5 回）
- **ログ**: `logs/YYYYMMDD.log` に日別保存

## 使い方

### Windows

1. `dist\aprsigate-windows-x64.exe` を好きなフォルダに置いて実行
2. ブラウザで管理画面 (http://127.0.0.1:8073/) が自動で開く
3. 「設定」タブでコールサイン・パスコード・位置・TNC を設定し、「保存して適用」を押す

設定は exe と同じフォルダの `aprsigate.json` に保存されます。受信した局とパケットは `aprsigate-data.json` に1分ごとと終了時に保存され、次回起動時に保持時間（既定 60 分）以内のものが復元されます。そのフォルダに書き込めない場合は `%APPDATA%\aprsigate\` に保存されます。
コンソールウィンドウを閉じると終了します。
署名していない exe なので、初回起動時に SmartScreen の警告が出たら「詳細情報」→「実行」を選んでください。

### Linux

```bash
chmod +x aprsigate-linux-x64
./aprsigate-linux-x64
```

- Raspberry Pi では、機種と OS に合わせて次のファイルを使います（`uname -m` が `aarch64` なら 64bit OS です）
  - Pi 3 / 4 / 5、Zero 2 W の 64bit OS: `aprsigate-linux-arm64`
  - Pi 3 / 4、Zero 2 W の 32bit OS: `aprsigate-linux-armv7`
  - Pi Zero / Zero W: `aprsigate-linux-armv6`（armv7 版は動きません）。CPU が非力なため動作が不安定になる可能性があり、使用はおすすめしません
- シリアル TNC を使う場合は、実行ユーザーを `dialout` グループに追加してください: `sudo usermod -aG dialout $USER`
- Bluetooth で無線機 (TH-D74 / TH-D75 / TM-D750) とつなぐ場合は、ペアリングしてから `sudo ./scripts/bt-tnc-setup.sh` を実行します。`/dev/rfcomm0` ができ、再起動後も使えるように登録されるので、TNC を「KISS シリアル」、ポートを `/dev/rfcomm0` にします（無線機側は KISS モードにして、データの通信先を Bluetooth に）。やめるときは `sudo ./scripts/bt-tnc-setup.sh --remove`
  - Windows では、ペアリング後に「その他の Bluetooth 設定」→「COM ポート」に出る「発信」側の COM を選びます
- 別の PC から管理画面を開くときは `-listen 0.0.0.0:8073` を付けて起動します（認証機能はないので、信頼できる LAN 内だけで使ってください）

systemd で常駐させる例 (`/etc/systemd/system/aprsigate.service`、「ユーザー名」は実行するユーザーに置き換え。いまの Raspberry Pi OS には `pi` ユーザーがありません):

```ini
[Unit]
Description=APRS I-Gate
After=network-online.target

[Service]
User=ユーザー名
WorkingDirectory=/home/ユーザー名/aprsigate
ExecStart=/home/ユーザー名/aprsigate/aprsigate-linux-arm64 -nobrowser
Restart=always

[Install]
WantedBy=multi-user.target
```

### 起動オプション

| オプション | 内容 |
|---|---|
| `-config パス` | 設定ファイルの場所を指定 |
| `-listen アドレス:ポート` | 管理画面の待ち受けアドレス（既定 `127.0.0.1:8073`） |
| `-nobrowser` | 起動時にブラウザを開かない |

## 設定のポイント

- **パスコード**: APRS-IS のパスコードを入力します。`-1` にすると受信専用になり、ゲートとビーコン（IS 側）は動きません
- **シンボル**: I-Gate ではテーブル `I` とシンボル `&` の組み合わせが一般的です
- **フィルター**: `m/50` は自局から半径 50 km の局を受信します。ほかに `r/緯度/経度/距離` や `b/コールサイン*` なども使えます
- **Direwolf** と組み合わせる場合は、TNC の接続方式を「KISS TCP」、アドレスを `127.0.0.1:8001` にします
- **UZ7HO Soundmodem** の場合は、Soundmodem 側で KISS サーバー（既定 8100 番ポート）を有効にし、アドレスを `127.0.0.1:8100` にします

RF で送信するにはアマチュア無線局の免許が必要です。送信パス（`WIDE1-1` など）は地域の運用ルールに合わせてください。

## ビルド

Go 1.26 以降が必要です。`tools\go` にポータブル版の Go が入っています。

```powershell
.\build.ps1      # Windows 上で Windows/Linux 全バイナリを dist\ に生成
```

```bash
./build.sh       # Linux 上でビルド
```

## バージョンと変更履歴

現在のバージョンは Ver.1.0.0 です。バージョン情報と変更履歴は、管理画面の「このソフトについて」タブで確認できます。
新しい版を出すときは、`src/version.go` の `softwareVersion` を上げ、`changelog` の先頭に変更内容を追記します。

## ソース構成

| ファイル | 内容 |
|---|---|
| `src/main.go` | 起動処理と HTTP サーバー |
| `src/app.go` | I-Gate 本体（ゲート、ビーコン、メッセージ送信、状態管理） |
| `src/aprsis.go` | APRS-IS クライアント |
| `src/tnc.go` | KISS TNC（シリアル / TCP） |
| `src/ax25.go` | KISS フレーミング、AX.25 と TNC2 形式の相互変換 |
| `src/parse.go` | APRS パケット解析 |
| `src/stations.go` | 局データベース・軌跡 |
| `src/messages.go` | メッセージの ACK と再送 |
| `src/web.go` | REST API と Server-Sent Events |
| `src/web/` | 管理画面（Leaflet + OpenStreetMap） |
| `scripts/bt-tnc-setup.sh` | Bluetooth 無線機を `/dev/rfcommN` として使えるようにする (Linux) |

## クレジット

Developed by JR2TTA Nao feat.Claude

- 地図: © OpenStreetMap contributors（[タイル利用ポリシー](https://operations.osmfoundation.org/policies/tiles/)）
- 地図ライブラリ: [Leaflet](https://leafletjs.com/) 1.9.4 (BSD-2-Clause)
- APRS シンボル画像: [hessu/aprs-symbols](https://github.com/hessu/aprs-symbols)（各シンボルの権利表記は同リポジトリの COPYRIGHT.md を参照）
- シリアル通信: [go.bug.st/serial](https://github.com/bugst/go-serial) (BSD-3-Clause)
