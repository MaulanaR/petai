package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"petai/internal/paths"
	"petai/internal/win"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

const overlayTitle = "PetAI Overlay"

func main() {
	app := NewApp()
	// Remember who had focus at launch so the overlay can hand it back (it must never keep focus).
	app.launchFG = win.ForegroundWindow()

	err := wails.Run(&options.App{
		Title:            overlayTitle,
		Width:            800,
		Height:           600,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		StartHidden:      true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "petai-desktop-pet-7f3c1e2a",
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			BackdropType:                      windows.None,
			DisableFramelessWindowDecorations: true,
			DisableWindowIcon:                 true,
			DisablePinchZoom:                  true,
			ZoomFactor:                        1.0,
			WebviewUserDataPath:               filepath.Join(paths.DataDir(), "webview"),
		},
	})
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}
