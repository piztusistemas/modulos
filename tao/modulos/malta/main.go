package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailslinux "github.com/wailsapp/wails/v2/pkg/options/linux"
)

// Malta — módulo externo DE Tao (tao/modulos_tao.go), non de Piztu: xera
// informes IA comparando os contidos traballados coa programación oficial
// da materia. NON ten interface propia para isto — Tao lánzao cunha tarefa
// xa empaquetada (ContextoTarefaMalta, tarefa.go) e Malta procesa e remata
// sen amosar ningunha xanela, devolvendo o resultado por chamada de
// retorno. A única interface que amosa Malta é a configuración da IA
// (apertura normal dende ⚙ → Módulos en Tao, ver contexto.go/settings.go).
//
//go:embed all:frontend/src
var assets embed.FS

func main() {
	// Arranque autónomo: se Tao lanzou Malta cunha tarefa (TAO_TAREFA_MALTA),
	// procesámola e saímos SEN chamar a wails.Run — nunca se amosa ningunha
	// xanela neste camiño (ver tarefa.go).
	if ctxTarefa, ok := lerContextoTarefaMalta(); ok {
		ExecutarTarefaHeadless(ctxTarefa)
		return
	}
	// Mesmo camiño headless, pero para a redacción do informe asinado
	// (TAO_TAREFA_MALTA_ASINADO) — ver redaccion.go.
	if ctxRedaccion, ok := lerContextoTarefaRedaccion(); ok {
		ExecutarTarefaRedaccionHeadless(ctxRedaccion)
		return
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Malta",
		Width:  1100,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Mesma escala de grises ca Piztu/Tao/Yang/Pancho (ver DESIGN.md).
		BackgroundColour: &options.RGBA{R: 245, G: 245, B: 244, A: 255},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		Linux: &wailslinux.Options{
			ProgramName: "Malta",
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
