package main

// オブジェクト / アイテムビーコン

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

const maxObjects = 10 // 登録数の上限 (要望に応じて変更)

type ObjectDef struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"` // object / item
	Name        string  `json:"name"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Sym         string  `json:"sym"` // テーブル + シンボル (2 文字)
	Comment     string  `json:"comment"`
	Via         string  `json:"via"` // RF / IS / BOTH
	IntervalMin int     `json:"intervalMin"`
	Enabled     bool    `json:"enabled"`
	Manual      string  `json:"manual"` // 手動編集したパケット (空なら項目から自動生成)
}

type pendingKill struct {
	def     ObjectDef
	left    int
	next    time.Time
	expires time.Time
}

func newObjectID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// aprsText は英数字・記号 (印字可能 ASCII) のみか。APRS で特別な意味を持つ | ~ は不可。
func aprsText(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7d || c == '|' {
			return false
		}
	}
	return true
}

var manualRe = regexp.MustCompile(`^([A-Za-z0-9-]+)>([A-Za-z0-9-]+(?:,[A-Za-z0-9-]+\*?)*):(.+)$`)

func validateObjects(cfg *Config) error {
	if len(cfg.Objects) > maxObjects {
		return fmt.Errorf("オブジェクトの登録は %d 件までです", maxObjects)
	}
	names := map[string]bool{}
	for i := range cfg.Objects {
		o := &cfg.Objects[i]
		if o.ID == "" {
			o.ID = newObjectID()
		}
		o.Name = strings.TrimRight(o.Name, " ")
		label := o.Name
		if label == "" {
			label = fmt.Sprintf("%d 件目", i+1)
		}
		bad := func(msg string) error { return fmt.Errorf("オブジェクト「%s」: %s", label, msg) }
		switch o.Kind {
		case "object":
			if len(o.Name) < 1 || len(o.Name) > 9 {
				return bad("オブジェクトの名前は 1〜9 文字です")
			}
		case "item":
			if len(o.Name) < 3 || len(o.Name) > 9 {
				return bad("アイテムの名前は 3〜9 文字です")
			}
			if strings.ContainsAny(o.Name, "!_") {
				return bad("アイテムの名前に「!」「_」は使えません")
			}
		default:
			return bad("形式が正しくありません")
		}
		if !aprsText(o.Name) || !aprsText(o.Comment) {
			return bad("名前とコメントは英数字・記号のみ使えます")
		}
		if names[o.Name] {
			return bad("同じ名前が登録されています")
		}
		names[o.Name] = true
		if len(o.Comment) > 43 {
			return bad("コメントが長すぎます (43 文字まで)")
		}
		if len(o.Sym) != 2 || !aprsText(o.Sym) {
			return bad("シンボルが正しくありません")
		}
		if o.Lat < -90 || o.Lat > 90 || o.Lon < -180 || o.Lon > 180 {
			return bad("位置が正しくありません")
		}
		switch o.Via {
		case "RF", "IS", "BOTH":
		default:
			return bad("送信先が正しくありません")
		}
		if o.IntervalMin < 5 {
			return bad("送信間隔は 5 分以上にしてください")
		}
		o.Manual = strings.TrimSpace(o.Manual)
		if o.Manual != "" {
			if !aprsText(o.Manual) {
				return bad("パケットに英数字・記号以外の文字 (日本語など) が含まれています")
			}
			m := manualRe.FindStringSubmatch(o.Manual)
			if m == nil {
				return bad("パケットが「自局コール>宛先,パス:内容」の形式になっていません")
			}
			if !sameCall(m[1], cfg.Callsign) {
				return bad("パケットの送信元は自局 (" + cfg.Callsign + ") である必要があります")
			}
			if _, err := tnc2ToAX25(o.Manual); err != nil {
				return bad("パケットのアドレス部分が正しくありません")
			}
		}
	}
	return nil
}

// objectLines は送信する RF 用・APRS-IS 用の行を作る。
func objectLines(cfg Config, o ObjectDef, killed bool, now time.Time) (rfLine, isLine string) {
	ts := now.UTC().Format("021504") + "z"
	var hdr, info string
	if o.Manual != "" {
		i := strings.IndexByte(o.Manual, ':')
		hdr, info = o.Manual[:i], o.Manual[i+1:]
		b := []byte(info)
		if len(b) >= 18 && b[0] == ';' && allDigits(info[11:17]) {
			copy(b[11:18], ts) // オブジェクトの時刻は送信時に更新
			if killed {
				b[10] = '_'
			}
		} else if len(b) > 0 && b[0] == ')' && killed {
			if j := strings.IndexAny(info[1:], "!_"); j >= 0 {
				b[1+j] = '_'
			}
		}
		info = string(b)
	} else {
		pos := formatLat(o.Lat) + o.Sym[:1] + formatLon(o.Lon) + o.Sym[1:] + o.Comment
		if o.Kind == "item" {
			mark := "!"
			if killed {
				mark = "_"
			}
			info = ")" + o.Name + mark + pos
		} else {
			mark := "*"
			if killed {
				mark = "_"
			}
			info = ";" + padRight(o.Name, 9) + mark + ts + pos
		}
		hdr = cfg.Callsign + ">" + toCall + pathSuffix(cfg.BeaconRFPath)
	}
	src := hdr[:strings.IndexByte(hdr, '>')]
	dst := hdr[strings.IndexByte(hdr, '>')+1:]
	if i := strings.IndexByte(dst, ','); i >= 0 {
		dst = dst[:i]
	}
	return hdr + ":" + info, src + ">" + dst + ",TCPIP*:" + info
}

// sendObject は 1 件送信する。どちらかに送れたら true。
func (a *App) sendObject(o ObjectDef, killed bool) bool {
	cfg := a.Config()
	rfLine, isLine := objectLines(cfg, o, killed, time.Now())
	rf, is := a.transmitLines(rfLine, isLine, o.Via != "IS", o.Via != "RF")
	if !rf && !is {
		return false
	}
	shown := isLine
	if rf {
		shown = rfLine
	}
	if p, err := ParseTNC2(shown); err == nil {
		a.updateStation(p, "LOCAL")
	}
	return true
}

// SendObjectNow は「今すぐ送信」。
func (a *App) SendObjectNow(id string) error {
	for _, o := range a.Config().Objects {
		if o.ID == id {
			if !a.sendObject(o, false) {
				return errors.New("送信できませんでした (TNC / APRS-IS が未接続です)")
			}
			a.objMu.Lock()
			a.objNext[id] = time.Now().Add(time.Duration(o.IntervalMin) * time.Minute)
			a.objMu.Unlock()
			return nil
		}
	}
	return errors.New("オブジェクトが見つかりません (先に保存してください)")
}

// rescheduleObjects は設定変更時に送信予定を組み直し、削除・改名されたものの削除通知を予約する。
func (a *App) rescheduleObjects(old, cur []ObjectDef) {
	now := time.Now()
	a.objMu.Lock()
	defer a.objMu.Unlock()
	byID := map[string]ObjectDef{}
	for _, o := range cur {
		byID[o.ID] = o
	}
	for _, o := range old {
		n, ok := byID[o.ID]
		if o.Enabled && (!ok || n.Name != o.Name || n.Kind != o.Kind) {
			a.objKills = append(a.objKills, pendingKill{def: o, left: 3, next: now.Add(3 * time.Second), expires: now.Add(10 * time.Minute)})
			log.Printf("オブジェクト %s の削除通知を送信します", o.Name)
		}
	}
	next := map[string]time.Time{}
	k := 0
	for _, o := range cur {
		if !o.Enabled {
			continue
		}
		if t, ok := a.objNext[o.ID]; ok {
			next[o.ID] = t
		} else {
			// 起動直後・新規登録は少しずつずらして送信
			next[o.ID] = now.Add(time.Duration(20+k*10) * time.Second)
			k++
		}
	}
	a.objNext = next
}

func (a *App) checkObjects(now time.Time) {
	cfg := a.Config()
	if cfg.Callsign == "" {
		return
	}
	var due []ObjectDef
	a.objMu.Lock()
	for _, o := range cfg.Objects {
		if t, ok := a.objNext[o.ID]; ok && o.Enabled && !now.Before(t) {
			due = append(due, o)
			a.objNext[o.ID] = now.Add(time.Duration(o.IntervalMin) * time.Minute)
		}
	}
	var kills []ObjectDef
	keep := a.objKills[:0]
	for _, k := range a.objKills {
		if now.After(k.expires) || k.left <= 0 {
			continue
		}
		if !now.Before(k.next) {
			kills = append(kills, k.def)
			k.left--
			k.next = now.Add(30 * time.Second)
		}
		keep = append(keep, k)
	}
	a.objKills = keep
	a.objMu.Unlock()

	for i, o := range due {
		if i > 0 {
			time.Sleep(2 * time.Second) // 連続送信を避ける
		}
		a.sendObject(o, false)
	}
	for _, o := range kills {
		a.sendObject(o, true)
	}
}
