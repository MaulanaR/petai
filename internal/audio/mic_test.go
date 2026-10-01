//go:build mic

package audio

import (
	"context"
	"errors"
	"testing"
	"time"
)

// go test -tags mic ./internal/audio -run Mic -v   (needs a microphone; nothing is stored)
func TestMicOpensAndReportsLevels(t *testing.T) {
	frames := 0
	var maxRMS float64
	_, err := Listen(context.Background(), Options{
		WaitSpeech: 3 * time.Second,
		OnLevel: func(l float64, _ bool) {
			frames++
			if l > maxRMS {
				maxRMS = l
			}
		},
	})
	t.Logf("frames=%d maxRMS=%.0f err=%v", frames, maxRMS, err)
	if frames < 100 {
		t.Fatalf("expected ~150 frames in 3 s, got %d", frames)
	}
	if err != nil && !errors.Is(err, ErrNoSpeech) {
		t.Fatal(err)
	}
}
