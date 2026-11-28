package main

// バージョン情報と変更履歴。新しい版を出すときは softwareVersion を上げ、changelog の先頭に追記する。

const (
	softwareName    = "KISAXI"
	softwareVersion = "1.1.1"
	developedBy     = "Developed by JR2TTA Nao feat.Claude"
)

type ChangeEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
}

var changelog = []ChangeEntry{
	{
		Version: "1.1.1",
		Date:    "2026-11-28",
		Changes: []string{
			"アプリ内の更新後の再起動で、Bluetooth (rfcomm) の TNC が繋がらないままになることがある問題を修正 (旧プロセスが接続を閉じてから新プロセスを起動し、TNC の再接続も早めに再試行)",
			"機種名の表示に使うデバイス ID (tocall) 情報を最新の aprs-deviceid に更新 (新規 40 件・Mic-E の追加・名称修正を含む)",
		},
	},
	{
		Version: "1.1.0",
		Date:    "2026-11-28",
		Changes: []string{
			"APRS-IS → RF の「送出する局」で、JA1ABC のように SSID なしで指定した局の別 SSID (JA1ABC-2 など) まで RF へ漏れて送出される不具合を修正 (全 SSID を対象にするときは JA1ABC-* と指定)",
			"TNC 設定に KISS パラメーター (Persist / SlotTime / TXTail / Full Duplex) を追加。TXDELAY と同様に接続のたびに TNC へ送信 (空欄なら TNC の設定のまま)",
			"ゲートしたパケットの経路を地図上に線でリアルタイム表示 (局・軌跡にカーソルを合わせるとその経路を表示。地図上部のチェックボックスでオン/オフ)",
			"気象局の設定から CWOP へ気象データを送信できるように (間隔・ID・パスコード・サーバーを設定)",
			"DPRS (D-STAR) 経由の局の受信を除外する設定を追加 (宛先 APDPRS・APDG??・API??? などで判定し、シンボルのオーバーレイ D だけでは除外しない)",
		},
	},
	{
		Version: "1.0.1",
		Date:    "2026-11-24",
		Changes: []string{
			"デジピータで UIflood (KNn-N など地域限定パス) と UItrace (TRACEn-N) の中継に対応 (エイリアスを設定で登録)",
			"APRS-IS から RF への位置情報の送出を、コールサイン名指し・ポジションコメントごとに設定できるように",
			"PHG 付きビーコンのコメント前に / を入れ、TM-D710 などでコメント先頭の 1 文字が欠けるのを解消",
			"シリアルポートを開けないときのエラーを日本語にし、ポート欄に直接入力できる旨を表示",
		},
	},
	{
		Version: "1.0.0",
		Date:    "2026-11-23",
		Changes: []string{
			"APRS I-Gate「KISAXI」を公開しました",
		},
	},
}
