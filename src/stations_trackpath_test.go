package main

import (
	"testing"
	"time"
)

func TestTrackPathAlignsWithTrack(t *testing.T) {
	db := NewStationDB()
	now := time.Now()
	mk := func(raw string) *Packet {
		p, err := ParseTNC2(raw)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	db.Update(mk("JA1AAA>APRS,JA1DIG*,WIDE2-1:!3540.00N/13946.00E>"), "RF", now, 3)
	db.Update(mk("JA1AAA>APRS,JA1DIG*,WIDE2-1:!3541.00N/13946.00E>"), "RF", now, 3)
	db.Update(mk("JA1AAA>APRS,TCPIP*:!3542.00N/13946.00E>"), "IS", now, 3)
	s := db.Update(mk("JA1AAA>APRS:!3543.00N/13946.00E>"), "RF", now, 3)
	if len(s.Track) != 3 || len(s.TrackPath) != 3 {
		t.Fatalf("track=%d path=%d", len(s.Track), len(s.TrackPath))
	}
	if s.TrackPath[0] != "RF,JA1DIG*,WIDE2-1" || s.TrackPath[1] != "" || s.TrackPath[2] != "RF" {
		t.Fatalf("%q", s.TrackPath)
	}
}

func TestGarbledPositionJumpIsHeldBack(t *testing.T) {
	db := NewStationDB()
	now := time.Now()
	mk := func(raw string) *Packet {
		p, err := ParseTNC2(raw)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	db.Update(mk("JR2TTA-10>APRS:!3500.00N/13600.00E>"), "RF", now, 10)
	// 緯度は正しいが経度だけ化けた (136E -> 103E) 位置は採用しない
	s := db.Update(mk("JR2TTA-10>APRS:!3500.00N/10300.00E>"), "RF", now.Add(30*time.Second), 10)
	if s.Lon != 136 || len(s.Track) != 0 {
		t.Fatalf("lon=%v track=%d", s.Lon, len(s.Track))
	}
	s = db.Update(mk("JR2TTA-10>APRS:!3501.00N/13601.00E>"), "RF", now.Add(60*time.Second), 10)
	if len(s.Track) != 2 || s.Track[1][1] < 135 {
		t.Fatalf("track=%v", s.Track)
	}
	// 同じ遠方の位置が 2 回続けば本当の移動として採用する
	db.Update(mk("JR2TTA-10>APRS:!3500.00N/10300.00E>"), "RF", now.Add(90*time.Second), 10)
	s = db.Update(mk("JR2TTA-10>APRS:!3500.00N/10300.00E>"), "RF", now.Add(120*time.Second), 10)
	if s.Lon > 104 {
		t.Fatalf("lon=%v", s.Lon)
	}
}

func TestDropTrackSpikes(t *testing.T) {
	tr := []TrackPoint{{35, 136, 1}, {35, 103, 2}, {35.1, 136.1, 3}}
	got, path := dropTrackSpikes(tr, []string{"a", "b", "c"})
	if len(got) != 2 || len(path) != 2 || path[1] != "c" {
		t.Fatalf("%v %v", got, path)
	}
}
