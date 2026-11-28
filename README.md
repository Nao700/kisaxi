# KISAXI

**KISAXI** is free APRS I-Gate software (single executable with a web UI) for Windows and Linux, including Raspberry Pi. The documentation below is in Japanese.

KISAXI は APRS I-Gate ソフトウェアです。地図には OpenStreetMap を使います。
Windows 10/11（互換モード不要）と Linux（x64 / arm64 / armv7 / armv6、Raspberry Pi を含む）で動作し、ランタイムのインストールは不要な単体実行ファイルです。

- 紹介ページ: https://nao700.github.io/kisaxi/
- ダウンロード: [Releases の最新版](https://github.com/Nao700/kisaxi/releases/latest)（`KISAXI-windows-x64.exe`、`KISAXI-linux-x64`、`KISAXI-linux-arm64`、`KISAXI-linux-armv7`、`KISAXI-linux-armv6`、全部入りの `KISAXI-all-v*.zip`）
- 開発の応援: KISAXI は無料で使えます。ドネーションは [GitHub Sponsors](https://github.com/sponsors/Nao700) から
- APRSは双方向通信を原則としております。受信専用のI-GATE等の運用はお控えください。

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
- **ウェザーステーション (Davis / rtl_433)**: Vantage Pro2 / Vue のコンソールにつないだ USB・シリアルのデータロガー (6510USB / 6510SER)、または WeatherLinkIP から LOOP プロトコルで現在値を読み取り、自局とは別の SSID（既定は自局コール-13）の位置付き APRS WX パケットとして APRS-IS / RF に送信。「気象」タブに現在値・本日の極値・グラフ（6時間 / 24時間 / 7日）を表示し、履歴は再起動後も引き継ぐ。TNC 用とは別のシリアルポートを指定する。WeatherLink など他のソフトが同じロガーを使っていると読み取れない。接続方式で「rtl_433」を選ぶと、別途インストールした [rtl_433](https://github.com/merbanan/rtl_433) と RTL-SDR で 433MHz 帯の簡易気象センサー（Fine Offset・Acurite・Bresser など）を受信して同じ気象局にできる（β版・実機未確認。model / id でセンサーを絞り込める）
- **更新**: 設定の「更新」で、新しい版の確認と自動入れ替え（チェック欄でオン・オフ、既定オフ）に対応。オフのときも画面上部の表示から手動で更新できる。systemd で動かすときは `Restart=always` にしておくと更新後に自動で立ち上がる
- **ログ**: `logs/YYYYMMDD.log` に日別保存

## 使い方

### Windows

1. [Releases](https://github.com/Nao700/kisaxi/releases/latest) から `KISAXI-windows-x64.exe` をダウンロードし、好きなフォルダに置いて実行
2. ブラウザで管理画面 (http://127.0.0.1:8073/) が自動で開く
3. 「設定」タブでコールサイン・パスコード・位置・TNC を設定し、「保存して適用」を押す

設定は exe と同じフォルダの `kisaxi.json` に保存されます。受信した局とパケットは `kisaxi-data.json` に1分ごとと終了時に保存され、次回起動時に保持時間（既定 60 分）以内のものが復元されます。そのフォルダに書き込めない場合は `%APPDATA%\aprsigate\` に保存されます。
コンソールウィンドウを閉じると終了します。
署名していない exe なので、初回起動時に SmartScreen の警告が出たら「詳細情報」→「実行」を選んでください。

### Linux

```bash
chmod +x KISAXI-linux-x64   # Releases からダウンロードしたファイル
./KISAXI-linux-x64
```

- Raspberry Pi では、機種と OS に合わせて次のファイルを使います（`uname -m` が `aarch64` なら 64bit OS です）
  - Pi 3 / 4 / 5、Zero 2 W の 64bit OS: `KISAXI-linux-arm64`
  - Pi 3 / 4、Zero 2 W の 32bit OS: `KISAXI-linux-armv7`
  - Pi Zero / Zero W: `KISAXI-linux-armv6`（armv7 版は動きません）。CPU が非力なため動作が不安定になる可能性があり、使用はおすすめしません
- シリアル TNC を使う場合は、実行ユーザーを `dialout` グループに追加してください: `sudo usermod -aG dialout $USER`
- Bluetooth で無線機 (TH-D74 / TH-D75 / TM-D750) とつなぐ場合は、ペアリングしてから `sudo ./scripts/bt-tnc-setup.sh` を実行します。`/dev/rfcomm0` ができ、再起動後も使えるように登録されるので、TNC を「KISS シリアル」、ポートを `/dev/rfcomm0` にします（無線機側は KISS モードにして、データの通信先を Bluetooth に）。やめるときは `sudo ./scripts/bt-tnc-setup.sh --remove`
  - Windows では、ペアリング後に「その他の Bluetooth 設定」→「COM ポート」に出る「発信」側の COM を選びます
- 同じ LAN の別の PC やスマホから管理画面を開くとき（認証機能はないので、信頼できる LAN 内だけで使ってください）
  - **Tailscale の利用をおすすめします。** Pi で `sudo tailscale serve --bg 8073` を実行すれば、待ち受けを変えずに、Tailscale を入れた端末だけから家の中でも外出先でも `https://<Piの名前>.<tailnet名>.ts.net/` で安全に開けます。下の方法は Tailscale を使えないときの代わりです
  1. `-listen 0.0.0.0:8073` を付けて起動します（例: `./KISAXI-linux-arm64 -nobrowser -listen 0.0.0.0:8073`）。毎回付ける代わりに、止めた状態で `kisaxi.json` の `httpListen` を `"0.0.0.0:8073"` にしても同じです
  2. Pi などで `hostname -I` を実行し、先頭のアドレス（例: `192.168.1.20`）を確かめます
  3. 別の端末のブラウザで `http://192.168.1.20:8073/` を開きます。IP アドレスで開けば設定の保存もできます
  - Raspberry Pi OS は標準ではファイアウォールがありません。ufw を入れている場合は `sudo ufw allow 8073/tcp` を実行します
  - 管理画面には APRS-IS のパスコードも表示され、同じ LAN の誰でも開けてしまいます
- デスクトップのアイコンから起動したい場合（Raspberry Pi OS など）は、`scripts/kisaxi-start.sh` と `scripts/desktop-setup.sh`（あればアイコン用に `assets/kisaxi_app_icon.png` も）を実行ファイルと同じフォルダに置き、一度だけ `bash desktop-setup.sh` を実行します。デスクトップとメニューの「インターネット」に KISAXI のアイコンができ、ダブルクリックで端末の画面つきで起動します（止めるときはその画面で `Ctrl+C` か画面を閉じる）。機種に合う実行ファイルは自動で選ばれ、すでに動いている（systemd で常駐中など）ときは管理画面だけを開きます。ファイルマネージャーで「実行しますか」と聞かれたら「実行」を選びます。消すときは `bash desktop-setup.sh --remove`

systemd で常駐させる例 (`/etc/systemd/system/kisaxi.service`、「ユーザー名」は実行するユーザーに置き換え。いまの Raspberry Pi OS には `pi` ユーザーがありません):

```ini
[Unit]
Description=KISAXI
After=network-online.target

[Service]
User=ユーザー名
WorkingDirectory=/home/ユーザー名/kisaxi
ExecStart=/home/ユーザー名/kisaxi/KISAXI-linux-arm64 -nobrowser
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

現在のバージョンは Ver.1.1.0 です。バージョン情報と変更履歴は、管理画面の「このソフトについて」タブで確認できます。
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
| `src/davis.go` | Davis ウェザーステーション (LOOP プロトコル、シリアル / TCP) |
| `src/davisrf.go`, `src/rtlsdr.go` | Davis 屋外センサーの無線 (RTL-SDR) 直接受信と復調 |
| `src/rtl433.go` | rtl_433 の JSON から気象データを読み取る (β版) |
| `src/wxstation.go` | 自局の気象データの保持・履歴・APRS WX パケット送信 |
| `src/web.go` | REST API と Server-Sent Events |
| `src/web/` | 管理画面（Leaflet + OpenStreetMap） |
| `scripts/bt-tnc-setup.sh` | Bluetooth 無線機を `/dev/rfcommN` として使えるようにする (Linux) |
| `scripts/kisaxi-start.sh` | ダブルクリック起動用のスクリプト。機種に合う実行ファイルを選んで起動する (Linux) |
| `scripts/desktop-setup.sh` | デスクトップとメニューに起動アイコンを作る (Linux) |

## クレジット

Developed by JR2TTA Nao feat.Claude

- 地図: © OpenStreetMap contributors（[タイル利用ポリシー](https://operations.osmfoundation.org/policies/tiles/)）
- 地図ライブラリ: [Leaflet](https://leafletjs.com/) 1.9.4 (BSD-2-Clause)
- APRS シンボル画像: [hessu/aprs-symbols](https://github.com/hessu/aprs-symbols)（各シンボルの権利表記は同リポジトリの COPYRIGHT.md を参照）
- シリアル通信: [go.bug.st/serial](https://github.com/bugst/go-serial) (BSD-3-Clause)
