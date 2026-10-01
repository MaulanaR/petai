// Package paths resolves PetAI's data directory.
package paths

import (
	"os"
	"path/filepath"
)

// DataDir returns PETAI_DATA_DIR or %APPDATA%\PetAI, creating it if needed.
func DataDir() string {
	dir := os.Getenv("PETAI_DATA_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "."
		}
		dir = filepath.Join(base, "PetAI")
	}
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func Sub(parts ...string) string {
	p := filepath.Join(append([]string{DataDir()}, parts...)...)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	return p
}
