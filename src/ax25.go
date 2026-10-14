package main

// KISS フレーミングと AX.25 UI フレーム <-> TNC2 形式テキストの相互変換

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	kissFEND  = 0xC0
	kissFESC  = 0xDB
	kissTFEND = 0xDC
	kissTFESC = 0xDD
)

var errNotAPRS = errors.New("not an APRS UI frame")

// kissEncode はデータフレームを KISS でエスケープして返す。
func kissEncode(cmd byte, frame []byte) []byte {
	out := make([]byte, 0, len(frame)+8)
	out = append(out, kissFEND, cmd)
	for _, b := range frame {
		switch b {
		case kissFEND:
			out = append(out, kissFESC, kissTFEND)
		case kissFESC:
			out = append(out, kissFESC, kissTFESC)
		default:
			out = append(out, b)
		}
	}
	return append(out, kissFEND)
}

// kissDecoder はバイトストリームから KISS フレーム(コマンドバイト付き)を切り出す。
type kissDecoder struct {
	buf []byte
	esc bool
}

func (d *kissDecoder) Feed(data []byte) [][]byte {
	var frames [][]byte
	for _, b := range data {
		switch {
		case b == kissFEND:
			if len(d.buf) > 0 {
				frames = append(frames, d.buf)
				d.buf = nil
			}
			d.esc = false
		case d.esc:
			d.esc = false
			switch b {
			case kissTFEND:
				d.buf = append(d.buf, kissFEND)
			case kissTFESC:
				d.buf = append(d.buf, kissFESC)
			default:
				d.buf = append(d.buf, b)
			}
		case b == kissFESC:
			d.esc = true
		default:
			d.buf = append(d.buf, b)
		}
		if len(d.buf) > 2048 {
			d.buf = nil
		}
	}
	return frames
}

type ax25Addr struct {
	Call string
	SSID int
	H    bool
}

func (a ax25Addr) String() string {
	if a.SSID == 0 {
		return a.Call
	}
	return fmt.Sprintf("%s-%d", a.Call, a.SSID)
}

func isAlnumUpper(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func decodeAddr(b []byte) (ax25Addr, error) {
	var sb strings.Builder
	for i := 0; i < 6; i++ {
		c := b[i] >> 1
		if c == ' ' {
			continue
		}
		if !isAlnumUpper(c) {
			return ax25Addr{}, errors.New("invalid address character")
		}
		sb.WriteByte(c)
	}
	if sb.Len() == 0 {
		return ax25Addr{}, errors.New("empty address")
	}
	return ax25Addr{Call: sb.String(), SSID: int(b[6]>>1) & 0x0F, H: b[6]&0x80 != 0}, nil
}

// ax25ToTNC2 は AX.25 UI フレームを "SRC>DST,PATH:info" 形式に変換する。
func ax25ToTNC2(frame []byte) (string, error) {
	if len(frame) < 16 {
		return "", errors.New("short frame")
	}
	var addrs []ax25Addr
	i := 0
	for {
		if i+7 > len(frame) {
			return "", errors.New("truncated address field")
		}
		a, err := decodeAddr(frame[i : i+7])
		if err != nil {
			return "", err
		}
		addrs = append(addrs, a)
		last := frame[i+6]&0x01 != 0
		i += 7
		if last {
			break
		}
		if len(addrs) >= 10 {
			return "", errors.New("too many addresses")
		}
	}
	if len(addrs) < 2 || i+2 > len(frame) {
		return "", errors.New("bad frame")
	}
	ctl, pid := frame[i], frame[i+1]
	if ctl&^0x10 != 0x03 || pid != 0xF0 {
		return "", errNotAPRS
	}
	info := frame[i+2:]
	// 末尾の CR/LF は除去
	for len(info) > 0 && (info[len(info)-1] == '\r' || info[len(info)-1] == '\n') {
		info = info[:len(info)-1]
	}
	if len(info) == 0 {
		return "", errors.New("empty info")
	}

	dst, src, digis := addrs[0], addrs[1], addrs[2:]
	lastH := -1
	for k, d := range digis {
		if d.H {
			lastH = k
		}
	}
	var sb strings.Builder
	sb.WriteString(src.String())
	sb.WriteByte('>')
	sb.WriteString(dst.String())
	for k, d := range digis {
		sb.WriteByte(',')
		sb.WriteString(d.String())
		if k == lastH {
			sb.WriteByte('*')
		}
	}
	sb.WriteByte(':')
	sb.Write(info)
	return sb.String(), nil
}

func encodeAddr(s string, last, hbit bool) ([]byte, error) {
	call, ssid := strings.ToUpper(s), 0
	if i := strings.IndexByte(call, '-'); i >= 0 {
		n, err := strconv.Atoi(call[i+1:])
		if err != nil || n < 0 || n > 15 {
			return nil, fmt.Errorf("invalid SSID in %q", s)
		}
		call, ssid = call[:i], n
	}
	if len(call) == 0 || len(call) > 6 {
		return nil, fmt.Errorf("invalid callsign %q", s)
	}
	b := make([]byte, 7)
	for k := 0; k < 6; k++ {
		c := byte(' ')
		if k < len(call) {
			c = call[k]
			if !isAlnumUpper(c) {
				return nil, fmt.Errorf("invalid callsign %q", s)
			}
		}
		b[k] = c << 1
	}
	b[6] = 0x60 | byte(ssid<<1)
	if hbit {
		b[6] |= 0x80
	}
	if last {
		b[6] |= 0x01
	}
	return b, nil
}

// tnc2ToAX25 は "SRC>DST,PATH:info" を AX.25 UI フレームに変換する。
func tnc2ToAX25(pkt string) ([]byte, error) {
	colon := strings.IndexByte(pkt, ':')
	if colon < 0 {
		return nil, errors.New("no info separator")
	}
	hdr, info := pkt[:colon], pkt[colon+1:]
	gt := strings.IndexByte(hdr, '>')
	if gt <= 0 {
		return nil, errors.New("no source")
	}
	parts := strings.Split(hdr[gt+1:], ",")
	digis := parts[1:]
	if len(digis) > 8 {
		return nil, errors.New("too many digipeaters")
	}
	starIdx := -1
	for k, d := range digis {
		if strings.HasSuffix(d, "*") {
			starIdx = k
		}
	}

	frame := make([]byte, 0, 7*(2+len(digis))+2+len(info))
	a, err := encodeAddr(parts[0], false, true) // 宛先: C ビット = 1 (コマンド)
	if err != nil {
		return nil, err
	}
	frame = append(frame, a...)
	a, err = encodeAddr(hdr[:gt], len(digis) == 0, false)
	if err != nil {
		return nil, err
	}
	frame = append(frame, a...)
	for k, d := range digis {
		a, err = encodeAddr(strings.TrimSuffix(d, "*"), k == len(digis)-1, k <= starIdx)
		if err != nil {
			return nil, err
		}
		frame = append(frame, a...)
	}
	frame = append(frame, 0x03, 0xF0)
	frame = append(frame, info...)
	return frame, nil
}
