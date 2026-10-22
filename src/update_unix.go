//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach は再起動する新しいプロセスを、このプロセスから独立して動かす。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
