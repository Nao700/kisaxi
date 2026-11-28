//go:build !windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// detach は再起動する新しいプロセスを、このプロセスから独立して動かす。
// あわせて、開いているファイル (シリアルポートなど) を新しいプロセスへ引き継がないようにする。
// シリアルポートのライブラリは close-on-exec を付けずに開くため、そのままだと新しいプロセスが
// 旧プロセスの /dev/rfcomm0 を持ち続け、排他 (TIOCEXCL) のせいで新しい open が「使用中」になる。
func detach(cmd *exec.Cmd) {
	closeOnExecAll()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// closeOnExecAll は標準入出力以外の開いているファイル全部に close-on-exec を付ける。
func closeOnExecAll() {
	for fd := 3; fd < 4096; fd++ {
		unix.CloseOnExec(fd)
	}
}
