package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func stubHandler(chamado *bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*chamado = true
		w.WriteHeader(http.StatusOK)
	}
}

func TestApiAutorizado(t *testing.T) {
	casos := []struct {
		nome    string
		profile *Profile
		isAdmin bool
		quere   bool
	}{
		{"sen sesión", nil, false, false},
		{"profesor", &Profile{Role: "profesor"}, false, true},
		{"alumno", &Profile{Role: "alumno"}, false, false},
		{"admin sen profile", nil, true, true},
		{"profesor e admin", &Profile{Role: "profesor"}, true, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			a := NewApp()
			a.profile = c.profile
			a.isAdmin = c.isAdmin
			if got := a.apiAutorizado(); got != c.quere {
				t.Fatalf("apiAutorizado() = %v, quere %v", got, c.quere)
			}
		})
	}
}

func TestAPIAutenticarRexeitaSenToken(t *testing.T) {
	a := NewApp()
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	rec := httptest.NewRecorder()
	s.autenticar(false, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, quere 401", rec.Code)
	}
	if chamado {
		t.Fatal("o handler non debía chamarse")
	}
}

func TestAPIAutenticarRexeitaTokenIncorrecto(t *testing.T) {
	a := NewApp()
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer non-e-o-token")
	rec := httptest.NewRecorder()
	s.autenticar(false, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, quere 401", rec.Code)
	}
	if chamado {
		t.Fatal("o handler non debía chamarse")
	}
}

func TestAPIAutenticarAceptaTokenValidoSenRol(t *testing.T) {
	a := NewApp()
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.autenticar(false, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusOK || !chamado {
		t.Fatalf("status = %d, chamado = %v; quere 200 e chamado", rec.Code, chamado)
	}
}

func TestAPIAutenticarRexeitaSenSesionDeRol(t *testing.T) {
	a := NewApp()
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/materias", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.autenticar(true, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quere 403", rec.Code)
	}
	if chamado {
		t.Fatal("o handler non debía chamarse")
	}
}

func TestAPIAutenticarRexeitaAlumno(t *testing.T) {
	a := NewApp()
	a.profile = &Profile{Role: "alumno"}
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/materias", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.autenticar(true, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quere 403 (sesión de alumno nunca debe pasar)", rec.Code)
	}
	if chamado {
		t.Fatal("o handler non debía chamarse")
	}
}

func TestAPIAutenticarAceptaProfesor(t *testing.T) {
	a := NewApp()
	a.profile = &Profile{Role: "profesor"}
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/materias", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.autenticar(true, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusOK || !chamado {
		t.Fatalf("status = %d, chamado = %v; quere 200 e chamado", rec.Code, chamado)
	}
}

func TestAPIAutenticarAceptaAdmin(t *testing.T) {
	a := NewApp()
	a.isAdmin = true
	s := novoAPIServidor(a)
	var chamado bool
	req := httptest.NewRequest(http.MethodGet, "/api/v1/materias", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec := httptest.NewRecorder()
	s.autenticar(true, stubHandler(&chamado))(rec, req)
	if rec.Code != http.StatusOK || !chamado {
		t.Fatalf("status = %d, chamado = %v; quere 200 e chamado", rec.Code, chamado)
	}
}

func TestAPIRecuperarCapturaPanic(t *testing.T) {
	a := NewApp()
	s := novoAPIServidor(a)
	handler := s.recuperar(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, quere 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, quere application/json (nunca HTML)", ct)
	}
}
