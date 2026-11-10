package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// nomeXanelaPrincipal é o Name da única xanela nativa de Tao (v3 permite
// varias WebviewWindow no mesmo proceso, pero Tao só usa unha — sen xanela
// sen marco, sempre-enriba, agochar-ao-pechar nin bandexa do sistema; Malta
// e Yang son procesos independentes lanzados con exec.Command, non outra
// xanela deste mesmo proceso).
const nomeXanelaPrincipal = "tao"

func main() {
	app := NewApp()

	wailsApp := application.New(application.Options{
		Name: "Tao",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Icon: icon,
		Linux: application.LinuxOptions{
			ProgramName: "Tao",
		},
	})

	xanela := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             nomeXanelaPrincipal,
		Title:            "Tao",
		Width:            1280,
		Height:           860,
		MinWidth:         900,
		MinHeight:        600,
		StartState:       application.WindowStateMaximised,
		BackgroundColour: application.NewRGB(245, 245, 244),
		URL:              "/",
	})
	// Os <input type="file"> do webview tamén abren o explorador non modal
	// (dialogos_gtk3.go), para que nunca bloquee o peche da xanela.
	xanela.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		application.InvokeAsync(engancharSelectorWebview)
	})

	if err := wailsApp.Run(); err != nil {
		println("Error:", err.Error())
	}
}
