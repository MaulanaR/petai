// Package debugapi exposes a localhost-only JSON API for automated QA (enabled by PETAI_DEBUG_ADDR).
package debugapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Backend interface {
	State() any
	MergeConfig(patch []byte) (any, error)
	Trigger(occasion string) (any, error)
	Chat(text string) (any, error)
	Animations() any
	Play(name string) error
	Memories() (any, error)
	OpenUI(what string) error
	Activity(name string) error
}

// Start serves on addr (must be a loopback address). Returns a shutdown func.
func Start(addr string, b Backend, logf func(string, ...any)) (func(), error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("debug API must bind to a loopback address")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/state", func(w http.ResponseWriter, r *http.Request) { reply(w, b.State(), nil) })
	mux.HandleFunc("/debug/animations", func(w http.ResponseWriter, r *http.Request) { reply(w, b.Animations(), nil) })
	mux.HandleFunc("/debug/memories", func(w http.ResponseWriter, r *http.Request) {
		v, err := b.Memories()
		reply(w, v, err)
	})
	mux.HandleFunc("/debug/config", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		v, err := b.MergeConfig(body)
		reply(w, v, err)
	})
	mux.HandleFunc("/debug/trigger", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in struct{ Occasion string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		act, err := b.Trigger(strings.TrimSpace(in.Occasion))
		out := map[string]any{"ok": err == nil, "action": act, "error": ""}
		if err != nil {
			out["error"] = err.Error()
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("/debug/chat", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in struct{ Text string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		act, err := b.Chat(in.Text)
		out := map[string]any{"ok": err == nil, "action": act, "error": ""}
		if err != nil {
			out["error"] = err.Error()
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("/debug/play", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		err := b.Play(in.Name)
		reply(w, map[string]any{"ok": err == nil}, err)
	})
	mux.HandleFunc("/debug/ui", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in struct{ Open string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		err := b.OpenUI(in.Open)
		reply(w, map[string]any{"ok": err == nil}, err)
	})
	mux.HandleFunc("/debug/activity", func(w http.ResponseWriter, r *http.Request) {
		if !post(w, r) {
			return
		}
		var in struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		err := b.Activity(in.Name)
		reply(w, map[string]any{"ok": err == nil}, err)
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logf("debug api: %v", err)
		}
	}()
	logf("debug api listening on %s", addr)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}, nil
}

func post(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
