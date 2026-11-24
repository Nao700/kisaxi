package main

import (
	"errors"
	"strings"
	"testing"

	"go.bug.st/serial"
)

func TestSerialOpenError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&serial.PortError{}, "COM7 は他のプログラムが使用中"},
		{errors.New("x"), "COM7 を開けません"},
	}
	for _, c := range cases {
		if got := serialOpenError("COM7", c.err).Error(); !strings.Contains(got, c.want) {
			t.Errorf("got %q", got)
		}
	}
}
