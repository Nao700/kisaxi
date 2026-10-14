package main

// APRS メッセージ (送受信・ACK・再送)

import (
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	ID       int64  `json:"id"`
	Time     int64  `json:"time"`
	Dir      string `json:"dir"` // in / out
	From     string `json:"from"`
	To       string `json:"to"`
	Text     string `json:"text"`
	MsgNo    string `json:"msgNo"`
	Via      string `json:"via"`
	Acked    bool   `json:"acked"`
	Rejected bool   `json:"rejected"`
	Tries    int    `json:"tries"`
	Failed   bool   `json:"failed"`

	nextTry time.Time
}

const maxMsgTries = 5

type MsgStore struct {
	mu     sync.Mutex
	list   []*Message
	nextID int64
	nextNo int
	seen   map[string]time.Time
}

func NewMsgStore() *MsgStore {
	return &MsgStore{nextNo: rand.Intn(900) + 1, seen: map[string]time.Time{}}
}

func (s *MsgStore) add(m *Message) *Message {
	s.nextID++
	m.ID = s.nextID
	s.list = append(s.list, m)
	if len(s.list) > 1000 {
		s.list = s.list[len(s.list)-1000:]
	}
	c := *m
	return &c
}

func (s *MsgStore) All() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.list))
	for i, m := range s.list {
		out[i] = *m
	}
	return out
}

// NewOutgoing は送信メッセージを登録する。
func (s *MsgStore) NewOutgoing(from, to, text string) *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	no := strconv.Itoa(s.nextNo)
	s.nextNo++
	if s.nextNo > 99999 {
		s.nextNo = 1
	}
	return s.add(&Message{Time: time.Now().UnixMilli(), Dir: "out", From: from, To: to, Text: text, MsgNo: no})
}

// Incoming は自局宛メッセージを登録する。重複なら nil を返す。
func (s *MsgStore) Incoming(p *Packet, via string) *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, t := range s.seen {
		if now.Sub(t) > 30*time.Minute {
			delete(s.seen, k)
		}
	}
	key := strings.ToUpper(p.Src) + "|" + p.MsgID + "|" + p.MsgText
	if _, dup := s.seen[key]; dup {
		return nil
	}
	s.seen[key] = now
	return s.add(&Message{Time: now.UnixMilli(), Dir: "in", From: p.Src, To: p.MsgTo, Text: displayText(p.MsgText), MsgNo: p.MsgID, Via: via})
}

// Ack は ACK/REJ を受けて該当する送信メッセージを更新する。
func (s *MsgStore) Ack(from, msgNo string, rej bool) *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.list) - 1; i >= 0; i-- {
		m := s.list[i]
		if m.Dir == "out" && m.MsgNo == msgNo && sameCall(m.To, from) {
			if m.Acked || m.Rejected {
				return nil
			}
			if rej {
				m.Rejected = true
			} else {
				m.Acked = true
			}
			c := *m
			return &c
		}
	}
	return nil
}

// MarkSent は送信試行を記録する。
func (s *MsgStore) MarkSent(id int64, via string) *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.list {
		if m.ID == id {
			m.Tries++
			m.Via = via
			m.nextTry = time.Now().Add(time.Duration(30<<uint(m.Tries-1)) * time.Second)
			c := *m
			return &c
		}
	}
	return nil
}

// Due は再送が必要なメッセージと、再送上限に達したメッセージを返す。
func (s *MsgStore) Due(now time.Time) (retry []Message, failed []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.list {
		if m.Dir != "out" || m.Acked || m.Rejected || m.Failed || m.Tries == 0 || now.Before(m.nextTry) {
			continue
		}
		if m.Tries >= maxMsgTries {
			m.Failed = true
			failed = append(failed, *m)
		} else {
			retry = append(retry, *m)
		}
	}
	return
}

func msgBody(to, text, msgNo string) string {
	b := ":" + padRight(to, 9) + ":" + text
	if msgNo != "" {
		b += "{" + msgNo
	}
	return b
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

// sanitizeMsgText はメッセージ本文に使えない文字を除去する。
func sanitizeMsgText(t string) string {
	t = strings.Map(func(r rune) rune {
		switch r {
		case '|', '~', '{', '\r', '\n':
			return -1
		}
		return r
	}, t)
	if len(t) > 67 {
		// UTF-8 の文字境界で切る
		cut := 67
		for cut > 0 && (t[cut]&0xC0) == 0x80 {
			cut--
		}
		t = t[:cut]
	}
	return strings.TrimSpace(t)
}
