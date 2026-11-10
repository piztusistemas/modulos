package main

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGruposDe(t *testing.T) {
	for _, tc := range []struct {
		campo string
		quere []string
	}{
		{"eso3b", []string{"eso3b"}},
		{"eso3a, eso3b", []string{"eso3a", "eso3b"}},
		{" eso3a ,eso3b ", []string{"eso3a", "eso3b"}},
		{"eso3a;eso3b", []string{"eso3a", "eso3b"}},
		{"", nil},
		{" , ", nil},
	} {
		if got := gruposDe(tc.campo); !reflect.DeepEqual(got, tc.quere) {
			t.Errorf("gruposDe(%q) = %v, quere %v", tc.campo, got, tc.quere)
		}
	}
}

func TestPertenceAGrupo(t *testing.T) {
	if !pertenceAGrupo("eso3a, eso3b", "eso3b") {
		t.Error("un alumno de «eso3a, eso3b» pertence a eso3b")
	}
	if !pertenceAGrupo("eso3a, eso3b", " ESO3A ") {
		t.Error("a comparación ignora maiúsculas e espazos")
	}
	if pertenceAGrupo("eso3a, eso3b", "eso3") {
		t.Error("«eso3» non é un grupo do alumno: non pode casar por parecido")
	}
	if pertenceAGrupo("eso3b", "") {
		t.Error("un grupo baleiro non casa con nada")
	}
	if !compartenGrupo("eso3a, eso3b", "eso3b, bach1a") {
		t.Error("comparten eso3b")
	}
	if compartenGrupo("eso3a", "eso3b") {
		t.Error("non comparten ningún grupo")
	}
}

// TestGetMyMateriasConVariosGrupos reproduce o fallo real: unha conta matriculada en
// «eso3a, eso3b» non vía a materia de eso3b e Tao dicíalle "Aínda non tes materias
// asignadas", porque o grupo se comparaba como unha soa cadea.
func TestGetMyMateriasConVariosGrupos(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"alueso3":  {Name: "Alu", Surname: "Eso3", Group: "eso3a, eso3b"},
		"chiqui.r": {Name: "Chiqui", Surname: "R", Group: "eso3b"},
	})
	fake.files["dixitalizacion/materias/dix3eso.json"] = mustJSON(t, MateriaDef{
		ID: "dix3eso", Name: "DIX 3ESO", Groups: []string{"eso3a", "eso3b"}, AcademicYear: "2026/2027",
	})
	fake.files["dixitalizacion/materias/tec2eso.json"] = mustJSON(t, MateriaDef{
		ID: "tec2eso", Name: "TEC 2ºESO", Groups: []string{"eso2a"}, AcademicYear: "2026/2027",
	})
	srv := httptest.NewServer(fake.handler("alueso3", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	prof, err := a.loginServer("alueso3", "segredo", srv.URL)
	if err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	if prof.Group != "eso3a, eso3b" {
		t.Fatalf("grupo do perfil = %q", prof.Group)
	}
	materias, err := a.GetMyMaterias()
	if err != nil {
		t.Fatalf("GetMyMaterias: %v", err)
	}
	if len(materias) != 1 || materias[0].ID != "dix3eso" {
		t.Fatalf("materias = %+v, quere só DIX 3ESO", materias)
	}
}
