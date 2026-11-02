//go:build !android

package main

// RTL-SDR の読み取り。USB の制御は librtlsdr (rtlsdr.dll / librtlsdr.so) に任せ、実行時に読み込んで使う。
// 外部の実行ファイルは使わず、KISAXI のプロセスの中で IQ を直接受け取る。
// ライブラリが無い場合は、その旨を画面に表示する。cgo は使わない。

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var rtlLibNames = map[string][]string{
	"windows": {"rtlsdr.dll"},
	"linux":   {"librtlsdr.so.2", "librtlsdr.so.0", "librtlsdr.so"},
	"darwin":  {"librtlsdr.dylib", "/opt/homebrew/lib/librtlsdr.dylib", "/usr/local/lib/librtlsdr.dylib"},
}

type rtlAPI struct {
	getDeviceCount func() int32
	getDeviceName  func(idx uint32) string
	open           func(dev *uintptr, idx uint32) int32
	closeDev       func(dev uintptr) int32
	setSampleRate  func(dev uintptr, rate uint32) int32
	setCenterFreq  func(dev uintptr, freq uint32) int32
	setFreqCorr    func(dev uintptr, ppm int32) int32
	setGainMode    func(dev uintptr, manual int32) int32
	setGain        func(dev uintptr, tenthsDB int32) int32
	getGains       func(dev uintptr, gains *int32) int32
	setAGCMode     func(dev uintptr, on int32) int32
	resetBuffer    func(dev uintptr) int32
	readSync       func(dev uintptr, buf unsafe.Pointer, n int32, nRead *int32) int32
}

var (
	rtlOnce sync.Once
	rtlLib  *rtlAPI
	rtlErr  error
)

func loadRTL() (*rtlAPI, error) {
	rtlOnce.Do(func() {
		names := rtlLibNames[runtime.GOOS]
		if len(names) == 0 {
			rtlErr = fmt.Errorf("%s では RTL-SDR を使えません", runtime.GOOS)
			return
		}
		var h uintptr
		var err error
		for _, n := range names {
			if h, err = openLib(n); err == nil {
				break
			}
		}
		if err != nil {
			rtlErr = fmt.Errorf("RTL-SDR のライブラリ (%s) が見つかりません。KISAXI と同じフォルダに置いてください (%v)", names[0], err)
			return
		}
		a := &rtlAPI{}
		reg := func(fn any, name string) {
			if rtlErr == nil {
				defer func() {
					if r := recover(); r != nil {
						rtlErr = fmt.Errorf("RTL-SDR のライブラリに %s がありません", name)
					}
				}()
				purego.RegisterLibFunc(fn, h, name)
			}
		}
		reg(&a.getDeviceCount, "rtlsdr_get_device_count")
		reg(&a.getDeviceName, "rtlsdr_get_device_name")
		reg(&a.open, "rtlsdr_open")
		reg(&a.closeDev, "rtlsdr_close")
		reg(&a.setSampleRate, "rtlsdr_set_sample_rate")
		reg(&a.setCenterFreq, "rtlsdr_set_center_freq")
		reg(&a.setFreqCorr, "rtlsdr_set_freq_correction")
		reg(&a.setGainMode, "rtlsdr_set_tuner_gain_mode")
		reg(&a.setGain, "rtlsdr_set_tuner_gain")
		reg(&a.getGains, "rtlsdr_get_tuner_gains")
		reg(&a.setAGCMode, "rtlsdr_set_agc_mode")
		reg(&a.resetBuffer, "rtlsdr_reset_buffer")
		reg(&a.readSync, "rtlsdr_read_sync")
		if rtlErr == nil {
			rtlLib = a
		}
	})
	return rtlLib, rtlErr
}

type rtlStream struct {
	api  *rtlAPI
	dev  uintptr
	mu   sync.Mutex // done の保護
	rd   sync.Mutex // 読み取り中は閉じない
	done bool
}

// openRTLSDR は設定の番号の RTL-SDR を開き、928.9 MHz / 2.048 MS/s で受信を始める。
func openRTLSDR(cfg Config) (iqStream, string, error) {
	api, err := loadRTL()
	if err != nil {
		return nil, "", err
	}
	n := int(api.getDeviceCount())
	if n <= 0 {
		return nil, "", errors.New("RTL-SDR が見つかりません (USB に接続されているか、ドライバ (WinUSB) が入っているか確認してください)")
	}
	if cfg.WXSDRDevice >= n {
		return nil, "", fmt.Errorf("RTL-SDR の番号 %d はありません (%d 台見つかりました)", cfg.WXSDRDevice, n)
	}
	var dev uintptr
	if r := api.open(&dev, uint32(cfg.WXSDRDevice)); r != 0 || dev == 0 {
		return nil, "", fmt.Errorf("RTL-SDR を開けません (コード %d)。他のソフトが使用中の可能性があります", r)
	}
	name := api.getDeviceName(uint32(cfg.WXSDRDevice))
	fail := func(what string, r int32) (iqStream, string, error) {
		api.closeDev(dev)
		return nil, "", fmt.Errorf("RTL-SDR の設定に失敗しました: %s (コード %d)", what, r)
	}
	if r := api.setSampleRate(dev, uint32(rfSampleRate)); r != 0 {
		return fail("サンプルレート", r)
	}
	if cfg.WXSDRPPM != 0 {
		api.setFreqCorr(dev, int32(cfg.WXSDRPPM)) // 既に同じ値なら -2 が返るが問題ない
	}
	if r := api.setCenterFreq(dev, uint32(rfCenter)); r != 0 {
		return fail("中心周波数", r)
	}
	if cfg.WXSDRGain > 0 {
		api.setGainMode(dev, 1)
		api.setGain(dev, int32(cfg.WXSDRGain*10))
	} else {
		api.setGainMode(dev, 0)
	}
	api.setAGCMode(dev, 0)
	api.resetBuffer(dev)
	if name == "" {
		name = "RTL-SDR"
	}
	return &rtlStream{api: api, dev: dev}, fmt.Sprintf("%s #%d", name, cfg.WXSDRDevice), nil
}

func (s *rtlStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return 0, errors.New("閉じています")
	}
	s.mu.Unlock()
	s.rd.Lock()
	defer s.rd.Unlock()
	if len(p) > 1<<18 {
		p = p[:1<<18]
	}
	var n int32
	if r := s.api.readSync(s.dev, unsafe.Pointer(&p[0]), int32(len(p)), &n); r != 0 {
		return int(n), fmt.Errorf("RTL-SDR の読み取りに失敗しました (コード %d)。USB が外れた可能性があります", r)
	}
	return int(n), nil
}

func (s *rtlStream) Close() error {
	s.mu.Lock()
	already := s.done
	s.done = true
	s.mu.Unlock()
	if !already {
		s.rd.Lock() // 読み取り中の rtlsdr_read_sync が戻るのを待ってから閉じる
		s.api.closeDev(s.dev)
		s.rd.Unlock()
	}
	return nil
}
