package main

import (
	"encoding/csv"
	"strings"
	"testing"
)

// TestCSVDeAltas: o CSV que acompaña á copia ten que poder cargarse sen tocalo na
// alta masiva do panel, así que ten que falar o mesmo idioma que parseUsersCSV.
func TestCSVDeAltas(t *testing.T) {
	roster := map[string]RosterEntry{
		"ana.perez":  {Name: "Ana", Surname: "Pérez Ríos", Group: "eso4a"},
		"m_pardinas": {Name: "Martín", Surname: "Pardiñas", Group: ""},
	}
	texto, filas := csvDeAltas([]string{"ana.perez", "m_pardinas"}, roster, "Vimianzo(26)")
	if filas != 2 {
		t.Fatalf("filas = %d, quería 2", filas)
	}

	usuarios, err := parseUsersCSV(texto)
	if err != nil {
		t.Fatalf("o CSV xerado non o entende a alta masiva: %v", err)
	}
	if len(usuarios) != 2 {
		t.Fatalf("parseUsersCSV devolveu %d usuarios: %+v", len(usuarios), usuarios)
	}
	u := usuarios[0]
	if u.Username != "ana.perez" || u.Firstname != "Ana" || u.Lastname != "Pérez Ríos" ||
		u.Group != "eso4a" || u.Password != "Vimianzo(26)" {
		t.Errorf("primeira fila mal: %+v", u)
	}

	// Sen contrasinal, a columna queda baleira (énchese no momento de restaurar).
	texto, _ = csvDeAltas([]string{"ana.perez"}, roster, "")
	r := csv.NewReader(strings.NewReader(texto))
	filasCSV, _ := r.ReadAll()
	if filasCSV[1][1] != "" {
		t.Errorf("esperábase a columna de contrasinal baleira, hai %q", filasCSV[1][1])
	}
	if filasCSV[0][0] != "username" || filasCSV[0][1] != "password" {
		t.Errorf("cabeceira inesperada: %v", filasCSV[0])
	}
}

// TestRutaCSVDeAltas: o CSV vai a carón do zip, co mesmo nome.
func TestRutaCSVDeAltas(t *testing.T) {
	got := rutaCSVDeAltas("/home/usuario/Escritorio/copia-sitio-2026-09-17.zip")
	want := "/home/usuario/Escritorio/copia-sitio-2026-09-17-altas.csv"
	if got != want {
		t.Errorf("rutaCSVDeAltas = %q, quería %q", got, want)
	}
}
