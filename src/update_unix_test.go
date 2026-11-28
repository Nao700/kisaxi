//go:build !windows

package main

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// close-on-exec なしで開いたファイル (シリアルポートのライブラリと同じ) が、再起動で引き継がれないこと。
func TestCloseOnExecAll(t *testing.T) {
	fd, err := unix.Open(os.DevNull, unix.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if flags, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); flags&unix.FD_CLOEXEC != 0 {
		t.Fatal("前提: 開いた直後は close-on-exec なしのはず")
	}
	closeOnExecAll()
	if flags, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("close-on-exec が付いていません")
	}
}

func TestCloseInheritedPorts(t *testing.T) {
	// /dev/ 以下のデバイス (ここでは /dev/zero) を開いたままの fd が閉じられ、普通のファイルは残る
	dev, err := unix.Open("/dev/zero", unix.O_RDWR, 0)
	if err != nil {
		t.Skip("/dev/zero を開けません")
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	closeInheritedPorts()
	if _, err := unix.FcntlInt(uintptr(dev), unix.F_GETFD, 0); err == nil {
		unix.Close(dev)
		t.Fatal("デバイスの fd が閉じられていません")
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0); err != nil {
		t.Fatal("普通のファイルの fd まで閉じてしまいました")
	}
}
