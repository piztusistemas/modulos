//go:build !(linux && cgo && gtk3)

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Fóra de GTK3 usamos os diálogos de Wails tal cal.

func dialogoNativo(o opcionsDialogo) ([]string, error) {
	if o.gardar {
		dlg := application.Get().Dialog.SaveFile()
		dlg.SetOptions(&application.SaveFileDialogOptions{Title: o.titulo, Filename: o.nome, Filters: o.filtros})
		f, err := dlg.PromptForSingleSelection()
		if err != nil || f == "" {
			return nil, err
		}
		return []string{f}, nil
	}
	dlg := application.Get().Dialog.OpenFile().SetTitle(o.titulo)
	for _, f := range o.filtros {
		dlg.AddFilter(f.DisplayName, f.Pattern)
	}
	if o.multiple {
		return dlg.PromptForMultipleSelection()
	}
	f, err := dlg.PromptForSingleSelection()
	if err != nil || f == "" {
		return nil, err
	}
	return []string{f}, nil
}

func engancharSelectorWebview() {}
