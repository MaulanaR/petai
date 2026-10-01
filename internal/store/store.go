// Package store persists activity sessions, habit memories and chat history in SQLite.
package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS activity_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app TEXT NOT NULL,
  title TEXT NOT NULL,
  start_ts INTEGER NOT NULL,
  end_ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_activity_start ON activity_sessions(start_ts);
CREATE TABLE IF NOT EXISTS memories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  content TEXT NOT NULL,
  source TEXT NOT NULL,
  confidence REAL NOT NULL DEFAULT 0.6,
  created_ts INTEGER NOT NULL,
  updated_ts INTEGER NOT NULL,
  last_used_ts INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS chat_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role TEXT NOT NULL,
  text TEXT NOT NULL,
  ts INTEGER NOT NULL
);
`

type Store struct {
	db *sql.DB
	mu sync.Mutex // serializes writes
}

type Memory struct {
	ID         int64   `json:"id"`
	Kind       string  `json:"kind"`
	Content    string  `json:"content"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	CreatedAt  int64   `json:"createdAt"`
	UpdatedAt  int64   `json:"updatedAt"`
}

type ChatMessage struct {
	Role string `json:"role"` // user | pet
	Text string `json:"text"`
	TS   int64  `json:"ts"`
}

type AppUsage struct {
	App     string  `json:"app"`
	Minutes float64 `json:"minutes"`
}

type Stats struct {
	Days          int         `json:"days"`
	TopApps       []AppUsage  `json:"topApps"`
	TodayTopApps  []AppUsage  `json:"todayTopApps"`
	HourHistogram [24]float64 `json:"hourHistogram"` // minutes per local hour over the window
	TypicalStart  string      `json:"typicalStart"`  // HH:MM median first activity per day
	ActiveDays    int         `json:"activeDays"`
}

var MemoryKinds = []string{"habit", "preference", "fact", "goal"}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---- meta ----

func (s *Store) GetMeta(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---- activity ----

// StartSession opens a new activity session and returns its id.
func (s *Store) StartSession(app, title string, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.db.Exec(`INSERT INTO activity_sessions(app,title,start_ts,end_ts) VALUES(?,?,?,?)`, app, title, at.Unix(), at.Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// TouchSession extends a session's end time.
func (s *Store) TouchSession(id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE activity_sessions SET end_ts=? WHERE id=?`, at.Unix(), id)
	return err
}

// PurgeActivity deletes sessions older than retention.
func (s *Store) PurgeActivity(before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.db.Exec(`DELETE FROM activity_sessions WHERE end_ts < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// ActivityStats summarizes the last `days` days locally (no AI involved).
func (s *Store) ActivityStats(now time.Time, days int) (Stats, error) {
	st := Stats{Days: days}
	since := now.AddDate(0, 0, -days)
	rows, err := s.db.Query(`SELECT app, start_ts, end_ts FROM activity_sessions WHERE end_ts >= ? ORDER BY start_ts`, since.Unix())
	if err != nil {
		return st, err
	}
	defer rows.Close()
	total := map[string]float64{}
	today := map[string]float64{}
	firstByDay := map[string]time.Time{}
	y, m, d := now.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	for rows.Next() {
		var app string
		var a, b int64
		if err := rows.Scan(&app, &a, &b); err != nil {
			return st, err
		}
		start, end := time.Unix(a, 0).In(now.Location()), time.Unix(b, 0).In(now.Location())
		mins := end.Sub(start).Minutes()
		if mins <= 0 {
			continue
		}
		total[app] += mins
		if !end.Before(dayStart) {
			today[app] += mins
		}
		st.HourHistogram[start.Hour()] += mins
		key := start.Format("2006-01-02")
		if f, ok := firstByDay[key]; !ok || start.Before(f) {
			firstByDay[key] = start
		}
	}
	st.TopApps = top(total, 8)
	st.TodayTopApps = top(today, 5)
	st.ActiveDays = len(firstByDay)
	if len(firstByDay) > 0 {
		var mins []int
		for _, t := range firstByDay {
			mins = append(mins, t.Hour()*60+t.Minute())
		}
		sort.Ints(mins)
		med := mins[len(mins)/2]
		st.TypicalStart = time.Date(0, 1, 1, med/60, med%60, 0, 0, time.UTC).Format("15:04")
	}
	return st, rows.Err()
}

func top(m map[string]float64, n int) []AppUsage {
	out := make([]AppUsage, 0, len(m))
	for k, v := range m {
		out = append(out, AppUsage{App: k, Minutes: float64(int(v*10)) / 10})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Minutes > out[j].Minutes })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// ---- memories ----

func validKind(k string) bool {
	for _, v := range MemoryKinds {
		if v == k {
			return true
		}
	}
	return false
}

func (s *Store) AddMemory(kind, content, source string, confidence float64, at time.Time) (int64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, errors.New("empty memory")
	}
	if len(content) > 300 {
		content = content[:300]
	}
	if !validKind(kind) {
		kind = "fact"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// De-duplicate identical content.
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM memories WHERE lower(content)=lower(?)`, content).Scan(&id); err == nil {
		_, err = s.db.Exec(`UPDATE memories SET updated_ts=?, confidence=min(1.0, confidence+0.1) WHERE id=?`, at.Unix(), id)
		return id, err
	}
	r, err := s.db.Exec(`INSERT INTO memories(kind,content,source,confidence,created_ts,updated_ts) VALUES(?,?,?,?,?,?)`,
		kind, content, source, confidence, at.Unix(), at.Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) UpdateMemory(id int64, kind, content string, at time.Time) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("empty memory")
	}
	if len(content) > 300 {
		content = content[:300]
	}
	if !validKind(kind) {
		kind = "fact"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.db.Exec(`UPDATE memories SET kind=?, content=?, updated_ts=? WHERE id=?`, kind, content, at.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return errors.New("memory not found")
	}
	return nil
}

func (s *Store) DeleteMemory(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM memories WHERE id=?`, id)
	return err
}

func (s *Store) Memories() ([]Memory, error) {
	rows, err := s.db.Query(`SELECT id,kind,content,source,confidence,created_ts,updated_ts FROM memories ORDER BY updated_ts DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Kind, &m.Content, &m.Source, &m.Confidence, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TopMemories returns the n most relevant memories (confidence, then recency) and marks them used.
func (s *Store) TopMemories(n int, at time.Time) ([]Memory, error) {
	rows, err := s.db.Query(`SELECT id,kind,content,source,confidence,created_ts,updated_ts FROM memories
		ORDER BY confidence DESC, updated_ts DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	out := []Memory{}
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Kind, &m.Content, &m.Source, &m.Confidence, &m.CreatedAt, &m.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	s.mu.Lock()
	for _, m := range out {
		_, _ = s.db.Exec(`UPDATE memories SET last_used_ts=? WHERE id=?`, at.Unix(), m.ID)
	}
	s.mu.Unlock()
	return out, nil
}

// ---- chat ----

func (s *Store) AppendChat(role, text string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`INSERT INTO chat_messages(role,text,ts) VALUES(?,?,?)`, role, text, at.Unix()); err != nil {
		return err
	}
	// Keep the table bounded.
	_, err := s.db.Exec(`DELETE FROM chat_messages WHERE id NOT IN (SELECT id FROM chat_messages ORDER BY id DESC LIMIT 500)`)
	return err
}

// RecentChat returns the last n messages in chronological order.
func (s *Store) RecentChat(n int) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT role,text,ts FROM chat_messages ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var c ChatMessage
		if err := rows.Scan(&c.Role, &c.Text, &c.TS); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// ---- wipe ----

func (s *Store) WipeMemories() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM memories`)
	return err
}

// WipeAll removes every stored personal datum (activity, memories, chat, meta).
func (s *Store) WipeAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range []string{`DELETE FROM activity_sessions`, `DELETE FROM memories`, `DELETE FROM chat_messages`, `DELETE FROM meta`} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`VACUUM`)
	return err
}
