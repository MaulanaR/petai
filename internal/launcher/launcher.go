// Package launcher opens apps from the user's whitelist on the AI's request. The AI only names an
// app id plus an optional search query / document; paths and arguments always come from the user's
// own configuration.
package launcher

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"petai/internal/config"
	"petai/internal/docx"
)

type Doc struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type Request struct {
	AppID string `json:"app_id"`
	Query string `json:"query"`
	Doc   *Doc   `json:"document"`
}

type Result struct {
	App       string `json:"app"`
	File      string `json:"file"`
	URL       string `json:"url"`
	Clipboard bool   `json:"clipboard"`
}

var ErrUnknownApp = errors.New("aplikasi tidak ada di daftar")

// Launcher holds the OS hooks (replaced in tests).
type Launcher struct {
	// Open runs target (exe, .lnk, URL, file or folder) with optional parameters.
	Open         func(target, params string) error
	SetClipboard func(text string) error
	DocsFolder   func() string
	Now          func() time.Time
}

// Find returns the app with the given id (case-insensitive).
func Find(apps []config.App, id string) (config.App, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, a := range apps {
		if strings.ToLower(a.ID) == id {
			return a, true
		}
	}
	return config.App{}, false
}

func (l *Launcher) Launch(apps []config.App, docsFolder string, req Request) (Result, error) {
	app, ok := Find(apps, req.AppID)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownApp, req.AppID)
	}
	res := Result{App: app.Name}
	switch app.Kind {
	case "website":
		u, err := BuildURL(app.Target, req.Query)
		if err != nil {
			return res, err
		}
		res.URL = u
		if req.Doc != nil && app.Clipboard && l.SetClipboard != nil {
			if err := l.SetClipboard(PlainText(*req.Doc)); err == nil {
				res.Clipboard = true
			}
		}
		return res, l.Open(u, "")
	case "path":
		return res, l.Open(app.Target, "")
	default: // program
		params := strings.TrimSpace(app.Args)
		if req.Doc != nil && (app.Accepts == "docx" || app.Accepts == "txt") {
			folder := docsFolder
			if strings.TrimSpace(folder) == "" && l.DocsFolder != nil {
				folder = l.DocsFolder()
			}
			file, err := l.writeDoc(folder, app.Accepts, *req.Doc)
			if err != nil {
				return res, err
			}
			res.File = file
			params = strings.TrimSpace(params + ` "` + file + `"`)
		} else if req.Doc != nil && app.Clipboard && l.SetClipboard != nil {
			if err := l.SetClipboard(PlainText(*req.Doc)); err == nil {
				res.Clipboard = true
			}
		}
		return res, l.Open(app.Target, params)
	}
}

// BuildURL fills {query} (URL-encoded) and only allows http(s) URLs.
func BuildURL(target, query string) (string, error) {
	u := strings.ReplaceAll(target, "{query}", url.QueryEscape(strings.TrimSpace(query)))
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return "", fmt.Errorf("URL tidak valid: %s", target)
	}
	return u, nil
}

var badName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// SafeName turns a document title into a safe file name (without extension).
func SafeName(title string) string {
	s := strings.TrimSpace(badName.ReplaceAllString(title, " "))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if r := []rune(s); len(r) > 80 {
		s = strings.TrimSpace(string(r[:80]))
	}
	if s == "" {
		s = "Dokumen PetAI"
	}
	return s
}

func (l *Launcher) writeDoc(folder, kind string, d Doc) (string, error) {
	if strings.TrimSpace(folder) == "" {
		return "", errors.New("folder dokumen belum diatur")
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	name := SafeName(d.Title) + " " + now().Format("2006-01-02 1504")
	var data []byte
	var err error
	if kind == "docx" {
		name += ".docx"
		data, err = docx.Build(d.Title, d.Content)
		if err != nil {
			return "", err
		}
	} else {
		name += ".txt"
		data = []byte(strings.ReplaceAll(PlainText(d), "\n", "\r\n"))
	}
	path := filepath.Join(folder, name)
	if rel, err := filepath.Rel(folder, path); err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("nama file tidak aman")
	}
	for i := 2; fileExists(path); i++ {
		path = strings.TrimSuffix(filepath.Join(folder, name), filepath.Ext(name)) + fmt.Sprintf(" (%d)", i) + filepath.Ext(name)
	}
	return path, os.WriteFile(path, data, 0o644)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// PlainText renders a document without markdown markers (for .txt and the clipboard).
func PlainText(d Doc) string {
	var b strings.Builder
	title := strings.TrimSpace(d.Title)
	if title != "" {
		b.WriteString(title + "\n")
	}
	first := true // the AI often repeats the title as the first line
	blank := true
	for _, line := range strings.Split(strings.ReplaceAll(d.Content, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			blank = true
			continue
		}
		if first && strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "#")), title) {
			first = false
			continue
		}
		first = false
		if blank || strings.HasPrefix(t, "#") {
			b.WriteString("\n")
		}
		blank = false
		switch {
		case strings.HasPrefix(t, "#"):
			b.WriteString(strings.ToUpper(strings.TrimSpace(strings.TrimLeft(t, "#"))) + "\n")
		case t == "-" || t == "*" || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			b.WriteString("• " + strings.TrimSpace(t[1:]) + "\n")
		default:
			b.WriteString(strings.ReplaceAll(t, "**", "") + "\n")
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}
