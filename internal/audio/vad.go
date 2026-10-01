// Package audio records one spoken utterance from the microphone (16 kHz mono PCM) using a simple
// adaptive energy voice-activity detector, and encodes it as WAV. Audio only lives in memory.
package audio

import (
	"bytes"
	"encoding/binary"
	"math"
)

const (
	SampleRate = 16000
	FrameMs    = 20
	frameLen   = SampleRate * FrameMs / 1000
)

// VAD decides when speech starts and ends from per-frame RMS values.
type VAD struct {
	SilenceMs int // trailing silence that ends an utterance
	StartMs   int // sustained voice needed to start

	floor      float64
	calibrated int
	speaking   bool
	aboveMs    int
	belowMs    int
}

func NewVAD(silenceMs int) *VAD {
	if silenceMs <= 0 {
		silenceMs = 1000
	}
	return &VAD{SilenceMs: silenceMs, StartMs: 150}
}

func (v *VAD) thresholds() (start, end float64) {
	return math.Max(v.floor*3, 350), math.Max(v.floor*1.8, 220)
}

// Feed processes one frame's RMS and reports transitions.
func (v *VAD) Feed(rms float64) (started, ended bool) {
	if v.calibrated < 10 { // first 200 ms: learn the noise floor
		if v.calibrated == 0 || rms < v.floor {
			v.floor = rms
		}
		v.calibrated++
		return false, false
	}
	start, end := v.thresholds()
	if !v.speaking {
		if rms > start {
			v.aboveMs += FrameMs
			if v.aboveMs >= v.StartMs {
				v.speaking, v.belowMs = true, 0
				return true, false
			}
		} else {
			v.aboveMs = 0
			v.floor = 0.95*v.floor + 0.05*rms
		}
		return false, false
	}
	if rms < end {
		v.belowMs += FrameMs
		if v.belowMs >= v.SilenceMs {
			v.speaking = false
			return false, true
		}
	} else {
		v.belowMs = 0
	}
	return false, false
}

func (v *VAD) Speaking() bool { return v.speaking }

// RMS of a frame of 16-bit samples.
func RMS(frame []int16) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, s := range frame {
		f := float64(s)
		sum += f * f
	}
	return math.Sqrt(sum / float64(len(frame)))
}

// EncodeWAV wraps 16-bit mono PCM in a RIFF/WAVE container.
func EncodeWAV(pcm []int16, rate int) []byte {
	var b bytes.Buffer
	dataLen := uint32(len(pcm) * 2)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, 36+dataLen)
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, struct {
		Size          uint32
		Format        uint16
		Channels      uint16
		Rate          uint32
		ByteRate      uint32
		BlockAlign    uint16
		BitsPerSample uint16
	}{16, 1, 1, uint32(rate), uint32(rate * 2), 2, 16})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, dataLen)
	_ = binary.Write(&b, binary.LittleEndian, pcm)
	return b.Bytes()
}
