// Package tray provides the notification-area icon and menu (the app has no taskbar window).
package tray

import (
	"runtime"

	"github.com/energye/systray"
)

type Actions struct {
	ToggleVisible func() (visible bool)
	OpenChat      func()
	OpenVoice     func()
	OpenSettings  func()
	TogglePause   func() (paused bool)
	HideHour      func()
	Quit          func()
}

// Start runs the tray on its own locked OS thread.
func Start(icon []byte, a Actions) {
	go func() {
		runtime.LockOSThread()
		systray.Run(func() { onReady(icon, a) }, nil)
	}()
}

func Stop() { systray.Quit() }

func onReady(icon []byte, a Actions) {
	systray.SetIcon(icon)
	systray.SetTitle("PetAI")
	systray.SetTooltip("PetAI — desktop pet by Maulana Rahman")
	systray.SetOnClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })
	systray.SetOnRClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })
	systray.SetOnDClick(func(systray.IMenu) { a.OpenSettings() })

	show := systray.AddMenuItem("Sembunyikan pet", "Tampilkan / sembunyikan pet")
	voiceItem := systray.AddMenuItem("Ngobrol pakai suara…", "Mode voice (Ctrl+Alt+V) — bila model mendukung audio")
	chat := systray.AddMenuItem("Ajak ngobrol (ketik)…", "Buka chat teks (Ctrl+Alt+P)")
	settings := systray.AddMenuItem("Pengaturan…", "Buka pengaturan")
	systray.AddSeparator()
	pause := systray.AddMenuItemCheckbox("Pause pengamatan", "Hentikan sementara pengamatan aktivitas", false)
	hide := systray.AddMenuItem("Sembunyikan 1 jam", "Pet istirahat selama 1 jam")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Keluar", "Tutup PetAI")

	show.Click(func() {
		if a.ToggleVisible() {
			show.SetTitle("Sembunyikan pet")
		} else {
			show.SetTitle("Tampilkan pet")
		}
	})
	chat.Click(a.OpenChat)
	voiceItem.Click(a.OpenVoice)
	settings.Click(a.OpenSettings)
	pause.Click(func() {
		if a.TogglePause() {
			pause.Check()
		} else {
			pause.Uncheck()
		}
	})
	hide.Click(func() {
		a.HideHour()
		show.SetTitle("Tampilkan pet")
	})
	quit.Click(a.Quit)
}
