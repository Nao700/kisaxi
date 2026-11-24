package main

// バージョン情報と変更履歴。新しい版を出すときは softwareVersion を上げ、changelog の先頭に追記する。

const (
	softwareName    = "KISAXI"
	softwareVersion = "1.0.1"
	developedBy     = "Developed by JR2TTA Nao feat.Claude"
)

type ChangeEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
}

var changelog = []ChangeEntry{
	{
		Version: "1.0.1",
		Date:    "2026-11-24",
		Changes: []string{
			"デジピータで地域エイリアス (KN・NS・AC など) の KNn-N 形式を中継できるように (設定で登録。TRACE を登録すれば TRACEn-N も可)",
			"デジピータの中継対象を、局指定・緯度経度の範囲・ポジションのコメントで絞り込めるように",
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
