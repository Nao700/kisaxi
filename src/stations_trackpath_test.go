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
