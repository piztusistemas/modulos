package main

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestCalcularOrfos: só se marcan como orfos os rexistros sen conta en File Browser,
// e nunca as contas protexidas (director / admin de File Browser).
func TestCalcularOrfos(t *testing.T) {
	inf := calcularOrfos(datosOrfos{
		contas: []string{"admin", "ana.vila", "PROF1"},
		roster: map[string]RosterEntry{
			"ana.vila":  {Name: "Ana", Surname: "Vila", Group: "eso4a"},
			"xoan.rei":  {Name: "Xoán", Surname: "Rei", Group: "eso4a"},
			"lois.faro": {Name: "Lois", Surname: "Faro", Group: "eso3b"},
		},
		uuids:      map[string]string{"ana.vila": "u1", "xoan.rei": "u2"},
		profesores: []string{"prof1", "prof.vello"},
		perfis:     []storedProfile{{Username: "ana.vila"}, {Username: "xoan.rei"}},
		sesions:    map[string]string{"pc01.json": "ana.vila", "pc02.json": "xoan.rei", "pc03.json": ""},
		cartafoles: []string{"ana.vila", "lois.faro"},
		protexidos: []string{"director", "admin"},
	})

	if got, want := inf.Roster, []string{"lois.faro", "xoan.rei"}; !reflect.DeepEqual(got, want) {
		t.Errorf("roster orfo = %v, quere %v", got, want)
	}
	if got, want := inf.UUID, []string{"xoan.rei"}; !reflect.DeepEqual(got, want) {
		t.Errorf("uuid orfo = %v, quere %v", got, want)
	}
	// "prof1" existe como conta con outra capitalización: non é orfo.
	if got, want := inf.Profesores, []string{"prof.vello"}; !reflect.DeepEqual(got, want) {
		t.Errorf("profesores orfos = %v, quere %v", got, want)
	}
	if got, want := inf.PerfisLocais, []string{"xoan.rei"}; !reflect.DeepEqual(got, want) {
		t.Errorf("perfís orfos = %v, quere %v", got, want)
	}
	// pc03.json non declara usuario (equipo anonimizado): non conta como orfo.
	if got, want := inf.Sesions, []string{"pc02.json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sesións orfas = %v, quere %v", got, want)
	}
	if got, want := inf.Cartafoles, []string{"lois.faro"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cartafoles orfos = %v, quere %v", got, want)
	}
	if inf.Total != 7 {
		t.Errorf("total = %d, quere 7", inf.Total)
	}
	if inf.Contas != 3 {
		t.Errorf("contas = %d, quere 3", inf.Contas)
	}
}

// TestCalcularOrfosSenOrfos: cun servidor coherente non hai nada que purgar.
func TestCalcularOrfosSenOrfos(t *testing.T) {
	inf := calcularOrfos(datosOrfos{
		contas:     []string{"ana.vila"},
		roster:     map[string]RosterEntry{"ana.vila": {Name: "Ana"}},
		uuids:      map[string]string{"ana.vila": "u1"},
		cartafoles: []string{"ana.vila"},
		protexidos: []string{"director"},
	})
	if inf.Total != 0 {
		t.Fatalf("total = %d, quere 0 (informe: %+v)", inf.Total, inf)
	}
}

// TestPurgarOrfosContraServidor percorre o circuíto completo contra un File Browser
// falso: dúas fichas de alumnado das que só unha ten conta, un profesor que xa non
// existe, un marcador de sesión pegado no mapa e dous cartafoles persoais. Comproba
// que se limpa o que sobra, que non se toca o que si ten conta, e que os cartafoles
// só se borran cando se pide explicitamente.
func TestPurgarOrfosContraServidor(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.accounts = []map[string]any{
		{"username": "admin", "permissions": map[string]any{"admin": true}},
		{"username": "ana.vila"},
	}
	fake.dirs["dixitalizacion/sesions"] = true
	fake.dirs["users"] = true
	fake.dirs["users/ana.vila"] = true
	fake.dirs["users/xoan.rei"] = true
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana.vila": {Name: "Ana", Surname: "Vila", Group: "eso4a"},
		"xoan.rei": {Name: "Xoán", Surname: "Rei", Group: "eso4a"},
	})
	fake.files["dixitalizacion/alumnos-uuid.json"] = mustJSON(t, map[string]string{"ana.vila": "u1", "xoan.rei": "u2"})
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof.vello"})
	fake.files["dixitalizacion/sesions/pc01.json"] = mustJSON(t, sesionTao{Equipo: "pc01", Usuario: "xoan.rei", Conectado: true})
	fake.files["users/xoan.rei/dixitalizacion-backup.json"] = []byte(`[]`)

	srv := httptest.NewServer(fake.handler("admin", "segredo"))
	defer srv.Close()

	a := &App{dataDir: t.TempDir()}
	a.isAdmin = true // salta o login de Director; requireAdmin só mira este flag
	if err := a.SaveAdminConfig(AdminConfig{ServerURL: srv.URL, FBAdminUser: "admin", FBAdminPass: "segredo"}); err != nil {
		t.Fatalf("SaveAdminConfig: %v", err)
	}
	// Perfil local do alumno que xa non ten conta.
	if err := a.writeProfiles([]storedProfile{{Username: "ana.vila"}, {Username: "xoan.rei"}}); err != nil {
		t.Fatalf("writeProfiles: %v", err)
	}

	// Consulta: non debe modificar nada.
	inf, err := a.ListarOrfos()
	if err != nil {
		t.Fatalf("ListarOrfos: %v", err)
	}
	if inf.Total != 6 { // roster + uuid + profesor + perfil local + sesión + cartafol
		t.Fatalf("total = %d, quere 6 (informe: %+v)", inf.Total, inf)
	}
	if _, ok := fake.files["dixitalizacion/sesions/pc01.json"]; !ok {
		t.Fatal("ListarOrfos non pode borrar nada, e borrou o marcador de sesión")
	}

	// Purga sen cartafoles.
	inf, err = a.PurgarOrfos(false)
	if err != nil {
		t.Fatalf("PurgarOrfos: %v", err)
	}
	if inf.Total != 5 || len(inf.Cartafoles) != 0 {
		t.Fatalf("informe da purga = %+v, quere 5 rexistros e ningún cartafol", inf)
	}

	roster := map[string]RosterEntry{}
	if err := json.Unmarshal(fake.files["dixitalizacion/alumnos.json"], &roster); err != nil {
		t.Fatalf("alumnos.json: %v", err)
	}
	if _, quedou := roster["xoan.rei"]; quedou {
		t.Error("xoan.rei segue no roster despois de purgar")
	}
	if _, falta := roster["ana.vila"]; !falta {
		t.Error("ana.vila (con conta real) desapareceu do roster")
	}

	uuids := map[string]string{}
	json.Unmarshal(fake.files["dixitalizacion/alumnos-uuid.json"], &uuids)
	if _, quedou := uuids["xoan.rei"]; quedou {
		t.Error("xoan.rei segue en alumnos-uuid.json")
	}
	var profes []string
	json.Unmarshal(fake.files["dixitalizacion/profesores.json"], &profes)
	if len(profes) != 0 {
		t.Errorf("profesores.json = %v, quere baleiro", profes)
	}
	if _, quedou := fake.files["dixitalizacion/sesions/pc01.json"]; quedou {
		t.Error("o marcador de sesión de xoan.rei segue no servidor")
	}
	perfis, _ := a.loadProfiles()
	if len(perfis) != 1 || perfis[0].Username != "ana.vila" {
		t.Errorf("perfís locais = %+v, quere só ana.vila", perfis)
	}
	// O cartafol persoal NON se toca mentres non se pida.
	if !fake.dirs["users/xoan.rei"] {
		t.Error("borrouse users/xoan.rei sen pedilo")
	}

	// Segunda pasada pedindo tamén os cartafoles.
	inf, err = a.PurgarOrfos(true)
	if err != nil {
		t.Fatalf("PurgarOrfos(true): %v", err)
	}
	if inf.Total != 1 || len(inf.Cartafoles) != 1 || inf.Cartafoles[0] != "xoan.rei" {
		t.Fatalf("segunda purga = %+v, quere só o cartafol de xoan.rei", inf)
	}
	if fake.dirs["users/xoan.rei"] {
		t.Error("users/xoan.rei segue no servidor")
	}
	if !fake.dirs["users/ana.vila"] {
		t.Error("borrouse o cartafol de ana.vila, que ten conta")
	}
}
