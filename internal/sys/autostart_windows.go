// Package sys holds small OS integrations: autostart, global hotkey.
package sys

import (
	"os"
	"runtime"

	"golang.org/x/sys/windows/registry"

	"petai/internal/win"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "PetAI"

// SetAutostart adds/removes PetAI from the current user's Run key (only when the user opts in).
func SetAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runName, `"`+exe+`"`)
}

func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runName)
	return err == nil
}

// Hotkey registers a global hotkey on a dedicated OS thread and calls fn when pressed.
// The returned stop function unregisters it.
func Hotkey(mods, vk uint32, fn func()) (stop func()) {
	tidCh := make(chan uint32, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		const id = 0x5045 // "PE"
		ok := win.RegisterHotKey(id, mods, vk)
		tidCh <- win.CurrentThreadID()
		if !ok {
			return
		}
		defer win.UnregisterHotKey(id)
		var m win.Msg
		for win.GetMessage(&m) > 0 {
			if m.Message == win.WM_HOTKEY {
				fn()
			}
		}
	}()
	tid := <-tidCh
	return func() { win.PostThreadMessage(tid, win.WM_QUIT) }
}
