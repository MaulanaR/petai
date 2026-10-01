package launcher

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"petai/internal/config"
)

// New returns a launcher wired to the Windows shell.
func New() *Launcher {
	return &Launcher{Open: shellOpen, SetClipboard: setClipboard, DocsFolder: DefaultDocsFolder}
}

func shellOpen(target, params string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(os.ExpandEnv(target))
	if err != nil {
		return err
	}
	var p *uint16
	if params != "" {
		p, _ = syscall.UTF16PtrFromString(params)
	}
	return windows.ShellExecute(0, verb, file, p, nil, windows.SW_SHOWNORMAL)
}

// DefaultDocsFolder is Documents\PetAI.
func DefaultDocsFolder() string {
	docs, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if err != nil || docs == "" {
		home, _ := os.UserHomeDir()
		docs = filepath.Join(home, "Documents")
	}
	return filepath.Join(docs, "PetAI")
}

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClip     = user32.NewProc("OpenClipboard")
	procCloseClip    = user32.NewProc("CloseClipboard")
	procEmptyClip    = user32.NewProc("EmptyClipboard")
	procSetClipData  = user32.NewProc("SetClipboardData")
	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procMoveMemory   = kernel32.NewProc("RtlMoveMemory")
)

func setClipboard(text string) error {
	u, err := syscall.UTF16FromString(strings.ReplaceAll(text, "\n", "\r\n"))
	if err != nil {
		return err
	}
	if r, _, e := procOpenClip.Call(0); r == 0 {
		return e
	}
	defer procCloseClip.Call()
	procEmptyClip.Call()
	size := uintptr(len(u) * 2)
	h, _, e := procGlobalAlloc.Call(0x0002 /* GMEM_MOVEABLE */, size)
	if h == 0 {
		return e
	}
	ptr, _, e := procGlobalLock.Call(h)
	if ptr == 0 {
		return e
	}
	procMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&u[0])), size)
	procGlobalUnlock.Call(h)
	if r, _, e := procSetClipData.Call(13 /* CF_UNICODETEXT */, h); r == 0 {
		return e
	}
	return nil
}

func appPath(exe string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("")
		k.Close()
		if err == nil && v != "" {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// Presets are one-click entries for the settings page (Word only when installed).
func Presets() []config.App {
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	out := []config.App{
		{ID: "notepad", Name: "Notepad", Aliases: []string{"catatan", "notes"}, Kind: "program", Target: filepath.Join(win, "system32", "notepad.exe"), Accepts: "txt"},
	}
	if p := appPath("Winword.exe"); p != "" {
		out = append(out, config.App{ID: "word", Name: "Microsoft Word", Aliases: []string{"word", "ms word", "dokumen"}, Kind: "program", Target: p, Accepts: "docx"})
	}
	out = append(out,
		config.App{ID: "google_docs", Name: "Google Docs", Aliases: []string{"gdocs", "google dokumen"}, Kind: "website", Target: "https://docs.new", Accepts: "none", Clipboard: true},
		config.App{ID: "google_search", Name: "Google Search", Aliases: []string{"google", "cari", "search"}, Kind: "website", Target: "https://www.google.com/search?q={query}", Accepts: "none"},
		config.App{ID: "youtube", Name: "YouTube", Aliases: []string{"yt", "video"}, Kind: "website", Target: "https://www.youtube.com/results?search_query={query}", Accepts: "none"},
		config.App{ID: "calculator", Name: "Kalkulator", Aliases: []string{"calculator", "calc", "hitung"}, Kind: "program", Target: "calc.exe", Accepts: "none"},
	)
	return out
}

type Shortcut struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// StartMenuShortcuts lists .lnk files from the user and common Start Menu folders.
func StartMenuShortcuts() []Shortcut {
	roots := []string{
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs`),
	}
	seen := map[string]bool{}
	var out []Shortcut
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".lnk") {
				return nil
			}
			name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			low := strings.ToLower(name)
			if seen[low] || strings.Contains(low, "uninstall") || strings.Contains(low, "readme") {
				return nil
			}
			seen[low] = true
			out = append(out, Shortcut{Name: name, Path: p})
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	if len(out) > 400 {
		out = out[:400]
	}
	return out
}
