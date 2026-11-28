//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// closeInheritedPorts は、更新前の古いプロセス (close-on-exec を付けない版) から引き継いでしまった
// シリアルポートなどのデバイス (/dev/ttyUSB0・/dev/rfcomm0 など) の fd を閉じる。
// 起動の最初、まだ自分でファイルを開く前に呼ぶ (-after-update のとき)。
func closeInheritedPorts() {
	for fd := 3; fd < 1024; fd++ {
		target, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "/dev/") && target != "/dev/null" && target != "/dev/urandom" && target != "/dev/random" {
			unix.Close(fd)
		}
	}
}
