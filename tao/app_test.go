package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenFile hands the path to the system, so the tests stop at its guard: what is
// checked here is that it refuses what cannot be opened, and says why. Going further
// would pop a PDF reader open on whoever runs the suite.
func TestOpenFileRejectsWhatItCannotOpen(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()

	cases := []struct {
		name, path, wants string
	}{
		{"ruta baleira", "", "non se indicou"},
		{"só espazos", "   ", "non se indicou"},
		{"ficheiro inexistente", filepath.Join(dir, "non-existe.pdf"), "non se atopou"},
		{"un cartafol", dir, "cartafol"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := a.OpenFile(c.path)
			if err == nil {
				t.Fatalf("esperábase un erro para %q", c.path)
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Errorf("erro = %q, esperábase que mencionase %q", err, c.wants)
			}
		})
	}
}

func TestCheckOpenableAcceptsAnExistingFile(t *testing.T) {
	pdf := filepath.Join(t.TempDir(), "informe asinado.pdf") // con espazos, coma os reais
	if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := checkOpenable("  " + pdf + "  ")
	if err != nil {
		t.Fatalf("un ficheiro existente non debe ser rexeitado: %v", err)
	}
	if got != pdf {
		t.Errorf("ruta = %q, quere %q (sen espazos arredor)", got, pdf)
	}
}
