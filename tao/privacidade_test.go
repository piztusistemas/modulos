package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The rule lives on the notas-privadas/ PATH (Quantum access rules), not inside the user.
func TestNotasPrivadasRulePath(t *testing.T) {
	if notasPrivadasRulePath != "/dixitalizacion/notas-privadas" {
		t.Fatalf("notasPrivadasRulePath = %q", notasPrivadasRulePath)
	}
}

func TestFbSetUserPrivacyRuleIsIdempotent(t *testing.T) {
	fake := newFakeQuantumUsers("AdminPass1")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	a := adminAppFor(srv, "AdminPass1")

	// Deny twice (the second one is a 500 "already exists" on the server): both succeed.
	for i := 0; i < 2; i++ {
		if err := a.fbSetUserPrivacyRule("alu1", true); err != nil {
			t.Fatalf("deny (volta %d): %v", i+1, err)
		}
	}
	if !fake.denied(notasPrivadasRulePath, "alu1") || len(fake.rules[notasPrivadasRulePath]) != 1 {
		t.Fatalf("regras = %v; quere unha soa regra para alu1", fake.rules)
	}
	// Another student's rule is not disturbed by lifting alu1's.
	if err := a.fbSetUserPrivacyRule("alu2", true); err != nil {
		t.Fatal(err)
	}
	// Allow twice (the second one is a 404 "entry not found"): both succeed.
	for i := 0; i < 2; i++ {
		if err := a.fbSetUserPrivacyRule("alu1", false); err != nil {
			t.Fatalf("allow (volta %d): %v", i+1, err)
		}
	}
	if fake.denied(notasPrivadasRulePath, "alu1") || !fake.denied(notasPrivadasRulePath, "alu2") {
		t.Errorf("regras = %v; alu1 debe quedar libre e alu2 denegado", fake.rules)
	}

	// A wrong admin password is a real failure, not something to swallow.
	a.fbAdminPass = "mala"
	if err := a.fbSetUserPrivacyRule("alu3", true); err == nil {
		t.Error("cun contrasinal de admin malo debería fallar")
	}
}

// An account that cannot be denied notas-privadas/ must not be left behind.
func TestFbCreateUserFailsClosedWithoutPrivacyRule(t *testing.T) {
	fake := newFakeQuantumUsers("AdminPass1")
	fake.failAccess = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	a := adminAppFor(srv, "AdminPass1")

	err := a.fbCreateUser("alu1", "Clave123")
	if err == nil || !strings.Contains(err.Error(), "notas privadas") {
		t.Fatalf("fbCreateUser = %v; debería fallar por non poder aplicar a regra", err)
	}
	if id, _ := fake.byName("alu1"); id != 0 {
		t.Error("quedou unha conta sen a regra de privacidade: podería ler as notas privadas")
	}
}
