package main

import "testing"

// TestLoginRequiresServer checks that authentication only works against a File
// Browser server: there is no local/demo mode any more, so an empty server address
// (or missing credentials) is rejected before contacting the network.
func TestLoginRequiresServer(t *testing.T) {
	a := &App{dataDir: t.TempDir()}

	if _, err := a.Login("m_pardinas", "segredo", ""); err == nil {
		t.Fatal("agardábase erro ao entrar sen servidor de ficheiros")
	}
	if _, err := a.Login("", "", "http://192.168.0.22:8080"); err == nil {
		t.Fatal("agardábase erro ao entrar sen credenciais")
	}
	if a.profile != nil {
		t.Fatal("non se debía gardar ningunha sesión")
	}
}
