package main

import (
	"os/exec"
	"syscall"
)

// detach は再起動する新しいプロセスを、新しいコンソールウィンドウで独立して動かす。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
}

// closeInheritedPorts は Windows では何もしない (ハンドルは引き継がれない)。
func closeInheritedPorts() {}
