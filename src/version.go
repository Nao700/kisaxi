package main

// バージョン情報と変更履歴。新しい版を出すときは softwareVersion を上げ、changelog の先頭に追記する。

const (
	softwareName    = "KISAXI"
	softwareVersion = "1.0.0"
	developedBy     = "Developed by JR2TTA Nao feat.Claude"
)

type ChangeEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
}

var changelog = []ChangeEntry{
	{
		Version: "1.0.0",
		Date:    "2026-11-23",
		Changes: []string{
			"KISAXI の最初の正式リリースです",
		},
	},
}
