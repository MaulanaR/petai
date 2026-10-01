package audio

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winmm             = windows.NewLazySystemDLL("winmm.dll")
	procOpen          = winmm.NewProc("waveInOpen")
	procPrepare       = winmm.NewProc("waveInPrepareHeader")
	procUnprepare     = winmm.NewProc("waveInUnprepareHeader")
	procAddBuffer     = winmm.NewProc("waveInAddBuffer")
	procStart         = winmm.NewProc("waveInStart")
	procReset         = winmm.NewProc("waveInReset")
	procClose         = winmm.NewProc("waveInClose")
	ErrNoSpeech       = errors.New("tidak ada suara terdengar")
	ErrMicUnavailable = errors.New("mikrofon tidak tersedia")
)

const (
	waveMapper = 0xFFFFFFFF
	whdrDone   = 0x1
	bufFrames  = 2 // frames per buffer (40 ms)
	numBufs    = 10
)

type waveFormat struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

type waveHdr struct {
	Data          *byte
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

type Options struct {
	SilenceMs   int
	MaxSeconds  int           // hard cap per utterance (default 45)
	WaitSpeech  time.Duration // give up when nobody speaks (default 30 s)
	OnLevel     func(level float64, speaking bool)
	OnSpeechBeg func()
}

// Listen opens the default microphone, waits for speech and returns the utterance as WAV.
// The device is closed before returning.
func Listen(ctx context.Context, o Options) ([]byte, error) {
	if o.MaxSeconds <= 0 {
		o.MaxSeconds = 45
	}
	if o.WaitSpeech <= 0 {
		o.WaitSpeech = 30 * time.Second
	}
	var h uintptr
	wf := waveFormat{FormatTag: 1, Channels: 1, SamplesPerSec: SampleRate, AvgBytesPerSec: SampleRate * 2, BlockAlign: 2, BitsPerSample: 16}
	if r, _, _ := procOpen.Call(uintptr(unsafe.Pointer(&h)), waveMapper, uintptr(unsafe.Pointer(&wf)), 0, 0, 0); r != 0 {
		return nil, fmt.Errorf("%w (kode %d)", ErrMicUnavailable, r)
	}
	hdrs := make([]waveHdr, numBufs)
	bufs := make([][]byte, numBufs)
	size := uintptr(unsafe.Sizeof(waveHdr{}))
	for i := range hdrs {
		bufs[i] = make([]byte, frameLen*2*bufFrames)
		hdrs[i] = waveHdr{Data: &bufs[i][0], BufferLength: uint32(len(bufs[i]))}
		procPrepare.Call(h, uintptr(unsafe.Pointer(&hdrs[i])), size)
		procAddBuffer.Call(h, uintptr(unsafe.Pointer(&hdrs[i])), size)
	}
	defer func() {
		procReset.Call(h)
		for i := range hdrs {
			procUnprepare.Call(h, uintptr(unsafe.Pointer(&hdrs[i])), size)
		}
		procClose.Call(h)
		runtime.KeepAlive(bufs)
		runtime.KeepAlive(hdrs)
	}()
	procStart.Call(h)

	vad := NewVAD(o.SilenceMs)
	var pcm []int16
	preroll := make([][]int16, 0, 16) // last ~300 ms before speech starts
	began := time.Now()
	next := 0
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
		for atomic.LoadUint32(&hdrs[next].Flags)&whdrDone != 0 {
			n := int(hdrs[next].BytesRecorded) / 2
			samples := make([]int16, n)
			copy(samples, unsafe.Slice((*int16)(unsafe.Pointer(&bufs[next][0])), n))
			hdrs[next].Flags &^= whdrDone
			procAddBuffer.Call(h, uintptr(unsafe.Pointer(&hdrs[next])), size)
			next = (next + 1) % numBufs

			for off := 0; off+frameLen <= len(samples); off += frameLen {
				frame := samples[off : off+frameLen]
				rms := RMS(frame)
				started, ended := vad.Feed(rms)
				if o.OnLevel != nil {
					o.OnLevel(rms, vad.Speaking())
				}
				if !vad.Speaking() && !ended {
					preroll = append(preroll, frame)
					if len(preroll) > 15 {
						preroll = preroll[1:]
					}
				}
				if started {
					for _, f := range preroll {
						pcm = append(pcm, f...)
					}
					preroll = preroll[:0]
					if o.OnSpeechBeg != nil {
						o.OnSpeechBeg()
					}
				}
				if vad.Speaking() || ended {
					pcm = append(pcm, frame...)
				}
				if ended || len(pcm) >= o.MaxSeconds*SampleRate {
					return EncodeWAV(pcm, SampleRate), nil
				}
			}
		}
		if len(pcm) == 0 && time.Since(began) > o.WaitSpeech {
			return nil, ErrNoSpeech
		}
	}
}
