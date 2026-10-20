package main

// バージョン情報と変更履歴。新しい版を出すときは softwareVersion を上げ、changelog の先頭に追記する。

const (
	softwareName    = "APRSIGate"
	softwareVersion = "1.0.4"
	developedBy     = "Developed by JR2TTA Nao feat.Claude"
)

type ChangeEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
}

var changelog = []ChangeEntry{
	{
		Version: "1.0.4",
		Date:    "2026-10-20",
		Changes: []string{
			"位置あいまい (座標の桁を空白で伏せた局。Mic-E 含む) を受信したら、マーカーにカーソルを乗せた間 (ポップアップ表示中も) その範囲をピンク色の四角で地図に表示し、マーカーを範囲の中心に補正",
			"送信パケットの宛先 (TOCALL) を aprs-deviceid 登録済みの APKSXI に変更 (aprs.fi などで機種名が表示される)",
		},
	},
	{
		Version: "1.0.3",
		Date:    "2026-10-14",
		Changes: []string{
			"デジピータ機能を追加 (WIDE1-1・自局コール指定を中継、WIDE2-n の中継は設定で選択)",
			"パケットモニターに中継 (DIGI) の表示と絞り込みを追加",
		},
	},
	{
		Version: "1.0.2",
		Date:    "2026-10-11",
		Changes: []string{
			"オブジェクト / アイテムビーコンを追加 (最大 10 件、RF / APRS-IS / 両方、送信間隔は既定 30 分)",
			"座標は地図クリック・マーカーのドラッグ・直接入力・貼り付け (Google マップ形式・度分秒・北緯◯度) で登録可能",
			"送信するパケットを直接編集できるように変更 (英数字・記号のみ)",
			"削除したオブジェクト / アイテムは削除通知を送信して他局の地図からも消えるように変更",
			"オブジェクトやビーコンの設定変更だけでは TNC / APRS-IS をつなぎ直さないように変更",
			"シンボル欄のバックスラッシュが「¥」と表示される問題を修正",
		},
	},
	{
		Version: "1.0.1",
		Date:    "2026-10-11",
		Changes: []string{
			"RF・APRS-IS で受信したデータ (局・パケット) を一定時間 (既定 1 時間) 保持し、再起動後も引き継ぐように変更",
			"気象局 (WX) 情報欄を追加 (地図上の気象局をクリックすると表示)",
		},
	},
	{
		Version: "1.0.0",
		Date:    "2026-10-11",
		Changes: []string{
			"初版リリース",
			// 以下は初版に含まれる内容。画面には出さない (表示する場合はコメントを外す)
			// "APRS-IS 接続 (自動再接続)、KISS TNC 接続 (シリアル / TCP)",
			// "RF → APRS-IS ゲート、APRS-IS → RF ゲート (ローカル局宛メッセージ)",
			// "RF / APRS-IS ビーコン、メッセージ送受信 (ACK・再送)",
			// "OpenStreetMap による地図表示 (APRS シンボル・軌跡)",
			// "設定変更時の TNC 再接続で「Serial port busy」が出る問題を修正",
			// "コンソールの簡易編集モードを無効化 (ウィンドウのクリックで処理が止まる問題の対策)",
			// "アプリ本体と接続できないときの警告表示を追加",
			// "「このソフトについて」画面を追加",
		},
	},
}
