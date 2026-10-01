package watcher

import (
	"regexp"
	"strings"
)

var (
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	reDigits = regexp.MustCompile(`\d[\d\s\-.]{4,}\d`)
	reURLQ   = regexp.MustCompile(`(https?://[^\s?#]+)[?#][^\s]*`)
	reHexTok = regexp.MustCompile(`\b[A-Fa-f0-9]{24,}\b`)
	reKeyTok = regexp.MustCompile(`\b(sk|pk|rk|ghp|gho|xox[abp])[-_][A-Za-z0-9_\-]{8,}\b`)
	reUser   = regexp.MustCompile(`(?i)([a-z]:[\\/](?:users|documents and settings)[\\/])[^\\/\s]+`)
)

// Redact strips personal identifiers from a window title before it is stored or sent to an AI.
func Redact(title string) string {
	t := reKeyTok.ReplaceAllString(title, "[secret]")
	t = reUser.ReplaceAllString(t, "${1}[user]")
	t = reEmail.ReplaceAllString(t, "[email]")
	t = reURLQ.ReplaceAllString(t, "$1")
	t = reHexTok.ReplaceAllString(t, "[id]")
	t = reDigits.ReplaceAllStringFunc(t, func(m string) string {
		n := 0
		for _, r := range m {
			if r >= '0' && r <= '9' {
				n++
			}
		}
		if n >= 6 {
			return "[num]"
		}
		return m
	})
	t = strings.TrimSpace(t)
	if len(t) > 160 {
		t = t[:160] + "…"
	}
	return t
}

// Blocked reports whether the app or title matches any blocklist entry (case-insensitive substring).
func Blocked(app, title string, blocklist []string) bool {
	a, t := strings.ToLower(app), strings.ToLower(title)
	for _, b := range blocklist {
		b = strings.ToLower(strings.TrimSpace(b))
		if b == "" {
			continue
		}
		if strings.Contains(a, b) || containsWord(t, b) {
			return true
		}
	}
	return false
}

// containsWord matches short entries (≤3 chars, e.g. "bca", "bri") only as whole words
// to avoid false positives like "fabric" → "bri"; longer entries match as substrings.
func containsWord(s, w string) bool {
	if len(w) > 3 {
		return strings.Contains(s, w)
	}
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isAlnum(s[j-1])
		after := j+len(w) >= len(s) || !isAlnum(s[j+len(w)])
		if before && after {
			return true
		}
		i = j + 1
	}
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}
