//go:build android

package main

import "errors"

func openRTLSDR(cfg Config) (iqStream, string, error) {
	return nil, "", errors.New("Android では RTL-SDR の直接受信にまだ対応していません")
}
