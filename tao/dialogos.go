package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Exploradores de ficheiros de Tao. Úsanse no canto de application.Get().Dialog
// para que, en Linux, o explorador NON sexa modal: se queda aberto en segundo
// plano, a xanela principal pódese pechar igual (ver dialogos_gtk3.go). Noutras
// plataformas/compilacións redirixen aos diálogos de Wails (dialogos_outros.go).

type opcionsDialogo struct {
	titulo   string
	nome     string // nome proposto (só ao gardar)
	filtros  []application.FileFilter
	gardar   bool
	multiple bool
}

// escollerFicheiros abre o explorador para escoller un ou varios ficheiros.
// Devolve nil se a persoa cancela.
func escollerFicheiros(titulo string) ([]string, error) {
	return dialogoNativo(opcionsDialogo{titulo: titulo, multiple: true})
}

// escollerFicheiro abre o explorador para escoller un só ficheiro. "" se se cancela.
func escollerFicheiro(titulo string, filtros ...application.FileFilter) (string, error) {
	return primeiro(dialogoNativo(opcionsDialogo{titulo: titulo, filtros: filtros}))
}

// gardarFicheiro pregunta onde gardar, propondo nome. "" se se cancela.
func gardarFicheiro(titulo, nome string, filtros ...application.FileFilter) (string, error) {
	return primeiro(dialogoNativo(opcionsDialogo{titulo: titulo, nome: nome, filtros: filtros, gardar: true}))
}

func primeiro(ficheiros []string, err error) (string, error) {
	if err != nil || len(ficheiros) == 0 {
		return "", err
	}
	return ficheiros[0], nil
}
