// Package logx is a tiny file logger (<data>/logs/petai.log). Never pass secrets to it.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type Logger struct {
	mu sync.Mutex
	l  *log.Logger
	f  *os.File
}

const maxSize = 2 << 20

func Open(path string) *Logger {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	var w io.Writer = io.Discard
	if err == nil {
		w = f
	}
	return &Logger{l: log.New(w, "", log.LstdFlags), f: f}
}

func (lg *Logger) Printf(format string, args ...any) {
	if lg == nil {
		return
	}
	lg.mu.Lock()
	defer lg.mu.Unlock()
	_ = lg.l.Output(2, fmt.Sprintf(format, args...))
}

func (lg *Logger) Close() {
	if lg != nil && lg.f != nil {
		_ = lg.f.Close()
	}
}
