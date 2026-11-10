package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newLoggedProfessorApp(t *testing.T) (*App, *fakeFileBrowser) {
	t.Helper()
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	t.Cleanup(srv.Close)

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	return a, fake
}

func TestAPIWhoamiSenSesion(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quere 200 (whoami nunca dá 403)", rec.Code)
	}
	var resp respostaWhoamiAPI
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Role != "ningunha" {
		t.Fatalf("role = %q, quere ningunha", resp.Role)
	}
}

func TestAPIWhoamiComoProfesor(t *testing.T) {
	a, _ := newLoggedProfessorApp(t)
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	var resp respostaWhoamiAPI
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Role != "profesor" || !resp.Conectado {
		t.Fatalf("resp = %+v, quere role profesor e conectado", resp)
	}
}

func TestAPIWhoamiComoAdmin(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	if err := a.ConfigurarDirector("segredo123"); err != nil {
		t.Fatalf("ConfigurarDirector: %v", err)
	}
	if _, err := a.AdminLogin(directorUser, "segredo123"); err != nil {
		t.Fatalf("AdminLogin: %v", err)
	}
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	var resp respostaWhoamiAPI
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Role != "administrador" {
		t.Fatalf("role = %q, quere administrador", resp.Role)
	}
}

func TestAPIMateriasEsixeRol(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	s := novoAPIServidor(a)

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/materias", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/materias", strings.NewReader(`{"id":"x"}`)),
		httptest.NewRequest(http.MethodDelete, "/api/v1/materias/x", nil),
	} {
		req.Header.Set("Authorization", "Bearer "+s.token)
		rec := httptest.NewRecorder()
		s.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status = %d, quere 403 sen sesión de rol", req.Method, req.URL.Path, rec.Code)
		}
	}
}

func TestAPIListMateriasComoProfesor(t *testing.T) {
	a, fake := newLoggedProfessorApp(t)
	fake.files["dixitalizacion/materias/mat1.json"] = mustJSON(t, MateriaDef{
		ID: "mat1", Name: "Tecnoloxía", Groups: []string{"eso4a"},
	})
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/materias", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quere 200", rec.Code)
	}
	var materias []MateriaDef
	if err := json.NewDecoder(rec.Body).Decode(&materias); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(materias) != 1 || materias[0].ID != "mat1" {
		t.Fatalf("materias = %+v, quere [mat1]", materias)
	}
}

func TestAPISaveEDeleteMateriaComoProfesor(t *testing.T) {
	a, fake := newLoggedProfessorApp(t)
	s := novoAPIServidor(a)

	corpo := `{"id":"proba","name":"Proba API","groups":["eso4a"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/materias", strings.NewReader(corpo))
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if _, ok := fake.files["dixitalizacion/materias/proba.json"]; !ok {
		t.Fatal("SaveMateria non escribiu no fake File Browser")
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/materias/proba", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec = httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if _, ok := fake.files["dixitalizacion/materias/proba.json"]; ok {
		t.Fatal("DeleteMateria non borrou do fake File Browser")
	}

	// Auditoría: unha liña por POST e por DELETE, nada por GET.
	raw, err := os.ReadFile(filepath.Join(a.dataDir, nomeFicheiroAuditoriaAPI))
	if err != nil {
		t.Fatalf("ler auditoria_api.jsonl: %v", err)
	}
	liñas := 0
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			liñas++
		}
	}
	if liñas != 2 {
		t.Fatalf("liñas de auditoría = %d, quere 2 (POST + DELETE)", liñas)
	}
}

func TestAPISaveMateriaCorpoInvalido(t *testing.T) {
	a, _ := newLoggedProfessorApp(t)
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/materias", strings.NewReader(`{non-e-json`))
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quere 400", rec.Code)
	}
}

func TestAPIUsuarioEsixeRol(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usuarios/alumno", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quere 403 sen sesión de rol", rec.Code)
	}
}

func TestAPIUsuarioComoProfesorResolveDesdeRoster(t *testing.T) {
	a, fake := newLoggedProfessorApp(t)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"alumno": {Name: "Ana", Surname: "Pérez", Group: "eso4a"},
	})
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usuarios/alumno", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	var resp respostaUsuarioAPI
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username != "alumno" || resp.Name != "Ana" || resp.Surname != "Pérez" {
		t.Fatalf("resp = %+v, quere Ana Pérez", resp)
	}

	// É unha consulta auditada (dato persoal), a diferenza do resto de GETs.
	raw, err := os.ReadFile(filepath.Join(a.dataDir, nomeFicheiroAuditoriaAPI))
	if err != nil {
		t.Fatalf("ler auditoria_api.jsonl: %v", err)
	}
	if !strings.Contains(string(raw), "/api/v1/usuarios/alumno") {
		t.Fatalf("a consulta de usuario debía quedar auditada: %s", raw)
	}
}

func TestAPIUsuarioFallbackAPerfilLocal(t *testing.T) {
	a, _ := newLoggedProfessorApp(t)
	// Sen entrada en alumnos.json (o roster compartido), pero si en profiles.json
	// (a referencia local desta máquina) — cobre profesorado que aínda non está
	// no roster de alumnado.
	if err := a.saveIdentity("m.pardinas", "Marisa", "Pardiñas"); err != nil {
		t.Fatalf("saveIdentity: %v", err)
	}
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usuarios/m.pardinas", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	var resp respostaUsuarioAPI
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "Marisa" || resp.Surname != "Pardiñas" {
		t.Fatalf("resp = %+v, quere Marisa Pardiñas", resp)
	}
}

func TestAPIUsuarioNonAtopado(t *testing.T) {
	a, _ := newLoggedProfessorApp(t)
	s := novoAPIServidor(a)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usuarios/non-existe", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quere 404", rec.Code)
	}
}
