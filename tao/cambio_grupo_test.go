package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestMigrarRexistros: as sesións pasan á materia nova coa data traducida, sen pisar
// unha sesión que xa teña contido no destino, e a orixe queda intacta.
func TestMigrarRexistros(t *testing.T) {
	subjects := []map[string]any{{
		"id": "tec-1a",
		"logs": map[string]any{
			"2026-09-15": map[string]any{"date": "2026-09-15", "content": "práctica 1", "status": "lectivo"},
			"2026-09-17": map[string]any{"date": "2026-09-17", "content": "práctica 2", "status": "lectivo"},
			"2026-09-19": map[string]any{"date": "2026-09-19", "content": "fóra do mapa", "status": "lectivo"},
			"2026-09-22": map[string]any{"date": "2026-09-22", "status": "folga", "_profReprog": true},
		},
		"unitReports": map[string]any{"0": "informe UD1"},
	}, {
		"id": "tec-1b",
		"logs": map[string]any{
			"2026-09-18": map[string]any{"date": "2026-09-18", "content": "xa feita no 1b", "status": "lectivo"},
		},
	}}
	plan := MigracionMateria{From: "tec-1a", To: "tec-1b", Datas: map[string]string{
		"2026-09-15": "2026-09-16",
		"2026-09-17": "2026-09-18",
	}}

	subjects, n := migrarRexistros(subjects, plan)
	if n != 2 {
		t.Fatalf("copiadas %d sesións, quere 2 (a do 15 e a do 19; a do 17 choca cunha feita)", n)
	}
	logs := subjects[1]["logs"].(map[string]any)
	if got := logs["2026-09-16"].(map[string]any); got["content"] != "práctica 1" || got["date"] != "2026-09-16" {
		t.Errorf("sesión traducida = %v", got)
	}
	if got := logs["2026-09-18"].(map[string]any)["content"]; got != "xa feita no 1b" {
		t.Errorf("pisouse unha sesión con contido: %v", got)
	}
	if _, ok := logs["2026-09-19"]; !ok {
		t.Error("unha data sen correspondencia debe conservarse igual")
	}
	if _, ok := logs["2026-09-22"]; ok {
		t.Error("copiouse un marcador prestado do profesor (_profReprog)")
	}
	if got := subjects[1]["unitReports"].(map[string]any)["0"]; got != "informe UD1" {
		t.Errorf("informe de unidade = %v", got)
	}
	if len(subjects[0]["logs"].(map[string]any)) != 4 {
		t.Error("a materia de orixe non pode perder nada")
	}
	// O valor copiado non pode ser o mesmo mapa ca orixe (cambiar a data alí romperíaa).
	if subjects[0]["logs"].(map[string]any)["2026-09-15"].(map[string]any)["date"] != "2026-09-15" {
		t.Error("modificouse a data da sesión de orixe")
	}
}

// TestMigrarRexistrosCreaDestino: se o alumno aínda non tiña nada na materia nova,
// créase o subject.
func TestMigrarRexistrosCreaDestino(t *testing.T) {
	subjects := []map[string]any{{"id": "a", "logs": map[string]any{
		"2026-10-01": map[string]any{"date": "2026-10-01", "content": "x"},
	}}}
	subjects, n := migrarRexistros(subjects, MigracionMateria{From: "a", To: "b"})
	if n != 1 || len(subjects) != 2 || subjects[1]["id"] != "b" {
		t.Fatalf("n=%d subjects=%v", n, subjects)
	}
}

// TestChangeUserGroupContraServidor: o cambio de grupo actualiza o roster e o perfil
// local, e copia backup, traballos e comentarios á materia do grupo novo.
func TestChangeUserGroupContraServidor(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.accounts = []map[string]any{
		{"id": 1, "username": "admin", "permissions": map[string]any{"admin": true}},
		{"id": 2, "username": "ana.vila"},
	}
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana.vila": {Name: "Ana", Surname: "Vila", Group: "bach1a"},
	})
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{})
	fake.files["dixitalizacion/materias/tec-1a.json"] = mustJSON(t, MateriaDef{ID: "tec-1a", Name: "Tecnoloxía", Groups: []string{"bach1a"}})
	fake.files["dixitalizacion/materias/tec-1b.json"] = mustJSON(t, MateriaDef{ID: "tec-1b", Name: "Tecnoloxía", Groups: []string{"bach1b"}})
	fake.mkdirAll("users/ana.vila/traballos/tec-1a/2026-09-15")
	fake.files["users/ana.vila/traballos/tec-1a/2026-09-15/circuito.fzz"] = []byte("fritzing")
	fake.mkdirAll("users/ana.vila/comentarios/tec-1a/2026-09-15")
	fake.files["users/ana.vila/comentarios/tec-1a/2026-09-15/notas.pdf"] = []byte("pdf")
	fake.files["users/ana.vila/dixitalizacion-backup.json"] = mustJSON(t, []map[string]any{{
		"id": "tec-1a", "logs": map[string]any{
			"2026-09-15": map[string]any{"date": "2026-09-15", "content": "práctica 1"},
		},
	}})

	srv := httptest.NewServer(fake.handler("admin", "segredo"))
	defer srv.Close()

	a := &App{dataDir: t.TempDir()}
	a.isAdmin = true
	if err := a.SaveAdminConfig(AdminConfig{ServerURL: srv.URL, FBAdminUser: "admin", FBAdminPass: "segredo"}); err != nil {
		t.Fatalf("SaveAdminConfig: %v", err)
	}
	if err := a.writeProfiles([]storedProfile{{Username: "ana.vila", Name: "Ana", Surname: "Vila", Group: "bach1a", UUID: "u1"}}); err != nil {
		t.Fatalf("writeProfiles: %v", err)
	}

	res, err := a.ChangeUserGroup("ana.vila", " bach1b ", []MigracionMateria{{
		From: "tec-1a", To: "tec-1b", Datas: map[string]string{"2026-09-15": "2026-09-16"},
	}})
	if err != nil {
		t.Fatalf("ChangeUserGroup: %v", err)
	}
	if res.Materias != 1 || res.Rexistros != 1 || res.Ficheiros != 2 || len(res.Avisos) != 0 {
		t.Fatalf("resultado = %+v", res)
	}

	roster := map[string]RosterEntry{}
	json.Unmarshal(fake.files["dixitalizacion/alumnos.json"], &roster)
	if e := roster["ana.vila"]; e.Group != "bach1b" || e.Name != "Ana" {
		t.Errorf("roster = %+v, quere bach1b conservando o nome", e)
	}
	if p, _ := a.perfilLocal("ana.vila"); p.Group != "bach1b" || p.UUID != "u1" {
		t.Errorf("perfil local = %+v, quere bach1b conservando o uuid", p)
	}
	if string(fake.files["users/ana.vila/traballos/tec-1b/2026-09-16/circuito.fzz"]) != "fritzing" {
		t.Error("non se copiou o traballo á materia e data novas")
	}
	if string(fake.files["users/ana.vila/comentarios/tec-1b/2026-09-16/notas.pdf"]) != "pdf" {
		t.Error("non se copiou o comentario do profesor")
	}
	if _, ok := fake.files["users/ana.vila/traballos/tec-1a/2026-09-15/circuito.fzz"]; !ok {
		t.Error("a orixe non se pode borrar")
	}
	var subjects []map[string]any
	json.Unmarshal(fake.files["users/ana.vila/dixitalizacion-backup.json"], &subjects)
	_, despois := migrarRexistros(subjects, MigracionMateria{From: "tec-1a", To: "tec-1b", Datas: map[string]string{"2026-09-15": "2026-09-16"}})
	if despois != 0 {
		t.Error("o backup do servidor non quedou coa sesión copiada (unha segunda pasada aínda copiaría)")
	}

	// Repetilo é inofensivo: non hai nada novo que copiar.
	res, err = a.ChangeUserGroup("ana.vila", "bach1b", []MigracionMateria{{
		From: "tec-1a", To: "tec-1b", Datas: map[string]string{"2026-09-15": "2026-09-16"},
	}})
	if err != nil || res.Rexistros != 0 || res.Ficheiros != 0 {
		t.Fatalf("segunda pasada: res=%+v err=%v", res, err)
	}
}

// TestChangeUserGroupValida rexeita datos que escaparían do cartafol do alumno.
func TestChangeUserGroupValida(t *testing.T) {
	a := &App{dataDir: t.TempDir(), isAdmin: true}
	casos := []struct {
		user, grupo string
		plan        []MigracionMateria
	}{
		{"", "bach1b", nil},
		{"ana", "  ", nil},
		{"director", "bach1b", nil},
		{"ana", "bach1b", []MigracionMateria{{From: "../x", To: "b"}}},
		{"ana", "bach1b", []MigracionMateria{{From: "a", To: "b", Datas: map[string]string{"2026-09-15": "../../x"}}}},
	}
	for _, c := range casos {
		if _, err := a.ChangeUserGroup(c.user, c.grupo, c.plan); err == nil {
			t.Errorf("ChangeUserGroup(%q, %q, %v) debería fallar", c.user, c.grupo, c.plan)
		}
	}
}
