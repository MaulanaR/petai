package audio

import (
	"encoding/binary"
	"testing"
)

func TestVADStartEnd(t *testing.T) {
	v := NewVAD(600)
	feed := func(rms float64, ms int) (started, ended bool) {
		for i := 0; i < ms/FrameMs; i++ {
			s, e := v.Feed(rms)
			started = started || s
			ended = ended || e
		}
		return
	}
	if s, e := feed(80, 1000); s || e {
		t.Fatal("background noise must not start speech")
	}
	if s, _ := feed(2500, 100); s {
		t.Fatal("too short to start")
	}
	if s, _ := feed(2500, 300); !s || !v.Speaking() {
		t.Fatal("speech should start")
	}
	if _, e := feed(90, 400); e {
		t.Fatal("pause shorter than silence window must not end")
	}
	if _, e := feed(2400, 300); e {
		t.Fatal("still speaking")
	}
	if _, e := feed(90, 700); !e || v.Speaking() {
		t.Fatal("speech should end after silence")
	}
}

func TestEncodeWAV(t *testing.T) {
	b := EncodeWAV([]int16{1, -1, 300}, SampleRate)
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || len(b) != 44+6 {
		t.Fatalf("bad header len=%d", len(b))
	}
	if binary.LittleEndian.Uint32(b[24:28]) != SampleRate || binary.LittleEndian.Uint32(b[40:44]) != 6 {
		t.Fatal("bad rate or data size")
	}
}

func TestRMS(t *testing.T) {
	if RMS([]int16{3, -3, 3, -3}) != 3 {
		t.Fatal("rms")
	}
}
