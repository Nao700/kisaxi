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
