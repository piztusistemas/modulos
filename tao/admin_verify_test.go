package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

// fakeFBLoginServer is a minimal File Browser Quantum stand-in that only answers
// POST /api/auth/login: it returns a token for one known account and 401 for anything
// else, mirroring how the real server rejects wrong credentials.
func fakeFBLoginServer(t *testing.T, wantUser, wantPass string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		pass, _ := url.QueryUnescape(r.Header.Get("X-Password")) // Quantum URL-decodes it
		if r.URL.Query().Get("username") == wantUser && pass == wantPass {
			_, _ = w.Write([]byte("fake-token"))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
}

// TestSaveAdminConfigVerifiesCredentials covers the safeguard added to SaveAdminConfig:
// credentials that the server rejects (e.g. the admin user and password swapped) block
// the save; correct credentials go through; and a server that cannot be reached does
// NOT block the save (so the panel is still configurable while File Browser is down).
func TestSaveAdminConfigVerifiesCredentials(t *testing.T) {
	srv := fakeFBLoginServer(t, "admin", "Morancelle(42)")
	defer srv.Close()

	newAdminApp := func() *App {
		a := &App{dataDir: t.TempDir()}
		a.isAdmin = true // skip the Director login; requireAdmin only checks this flag
		return a
	}

	// Swapped user/password: the server rejects them, so the save must fail and leave
	// no admin.json behind.
	t.Run("credenciais intercambiadas rexéitanse", func(t *testing.T) {
		a := newAdminApp()
		err := a.SaveAdminConfig(AdminConfig{
			ServerURL:   srv.URL,
			FBAdminUser: "Morancelle(42)", // password typed into the user field
			FBAdminPass: "admin",
		})
		if err == nil {
			t.Fatal("esperábase erro ao gardar credenciais que o servidor rexeita")
		}
		if _, statErr := os.Stat(a.adminConfigPath()); statErr == nil {
			t.Fatal("non se debería ter escrito admin.json cando as credenciais son rexeitadas")
		}
	})

	// Correct credentials: the save succeeds and admin.json is written.
	t.Run("credenciais correctas gárdanse", func(t *testing.T) {
		a := newAdminApp()
		err := a.SaveAdminConfig(AdminConfig{
			ServerURL:   srv.URL,
			FBAdminUser: "admin",
			FBAdminPass: "Morancelle(42)",
		})
		if err != nil {
			t.Fatalf("SaveAdminConfig con credenciais correctas: %v", err)
		}
		if _, statErr := os.Stat(a.adminConfigPath()); statErr != nil {
			t.Fatalf("esperábase admin.json escrito: %v", statErr)
		}
	})

	// Server unreachable: verification cannot run, but that must not block the save.
	t.Run("servidor inaccesible non bloquea o gardado", func(t *testing.T) {
		a := newAdminApp()
		err := a.SaveAdminConfig(AdminConfig{
			ServerURL:   "http://127.0.0.1:1", // nothing listening here
			FBAdminUser: "admin",
			FBAdminPass: "Morancelle(42)",
		})
		if err != nil {
			t.Fatalf("un servidor inaccesible non debería bloquear o gardado: %v", err)
		}
		if _, statErr := os.Stat(a.adminConfigPath()); statErr != nil {
			t.Fatalf("esperábase admin.json escrito aínda co servidor caído: %v", statErr)
		}
	})
}

// A locked account cannot be verified, so nothing is saved (a wrong password could
// otherwise be persisted silently).
func TestSaveAdminConfigContaBloqueadaNonGarda(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"status":429,"message":"too many failed authentication attempts"}`))
	}))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	a.isAdmin = true
	err := a.SaveAdminConfig(AdminConfig{ServerURL: srv.URL, FBAdminUser: "admin", FBAdminPass: "x"})
	if !errors.Is(err, errFBContaBloqueada) {
		t.Fatalf("SaveAdminConfig = %v; quere errFBContaBloqueada", err)
	}
	if _, statErr := os.Stat(a.adminConfigPath()); statErr == nil {
		t.Error("gardou admin.json aínda que non puido verificar o contrasinal")
	}
}
