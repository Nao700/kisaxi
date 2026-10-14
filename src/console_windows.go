package main

import "golang.org/x/sys/windows"

func init() {
	// Windows のコンソール (既定は CP932) で日本語ログが文字化けしないよう UTF-8 に切り替える
	windows.SetConsoleOutputCP(65001) // CP_UTF8

	// 簡易編集モードを無効化する。有効だとウィンドウをクリックして「選択」状態になった間
	// コンソール出力が止まり、ログ出力で I-Gate の処理全体が停止してしまう。
	const enableQuickEdit, enableExtendedFlags = 0x0040, 0x0080
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		windows.SetConsoleMode(h, (mode&^enableQuickEdit)|enableExtendedFlags)
	}
}
