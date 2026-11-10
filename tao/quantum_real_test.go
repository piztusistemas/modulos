package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestQuantumReal exercises the File Browser client against a REAL Quantum server, to
// catch the day the fake (fakeFileBrowser) and the real thing drift apart. It is skipped
// unless PIZTU_QUANTUM_URL is set:
//
//	PIZTU_QUANTUM_URL=http://127.0.0.1:8081/arquivos \
//	PIZTU_QUANTUM_USER=alumno1 PIZTU_QUANTUM_PASS=... go test -run TestQuantumReal .
//
// The account needs read/write on the "arquivos" source and must NOT be an admin (it is
// what a student is), and the server needs sources[].config.useLogicalSize: true. The
// test works under its own folder (tmp-quantum-real/) and removes it, and changes the
// account's password and back again, so it leaves the server as it found it.
func TestQuantumReal(t *testing.T) {
	base := os.Getenv("PIZTU_QUANTUM_URL")
	user, pass := os.Getenv("PIZTU_QUANTUM_USER"), os.Getenv("PIZTU_QUANTUM_PASS")
	if base == "" || user == "" || pass == "" {
		t.Skip("define PIZTU_QUANTUM_URL, PIZTU_QUANTUM_USER e PIZTU_QUANTUM_PASS para probar contra un Quantum real")
	}
	const dir = "tmp-quantum-real"

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer(user, pass, base); err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	t.Cleanup(func() { a.fbDeleteDirect(dir) })
	a.fbDeleteDirect(dir) // leftovers of a previous, interrupted run

	if !probarRaizFileBrowser(base) {
		t.Error("probarRaizFileBrowser non recoñeceu un Quantum real")
	}
	if id, err := fbUserID(a.fbToken); err != nil || id == 0 {
		t.Errorf("fbUserID = %d, %v; quere o id da conta", id, err)
	}

	// A folder must really be a FOLDER (a bare trailing slash makes Quantum create a file),
	// and asking for one that already exists (409) must not be an error.
	sub := dir + "/Cartafol ñ #1&2+3%"
	for i := 0; i < 2; i++ {
		if err := a.fbEnsureDirDirect(sub); err != nil {
			t.Fatalf("fbEnsureDirDirect (volta %d): %v", i+1, err)
		}
	}
	items, err := a.fbList(dir)
	if err != nil || len(items) != 1 || !items[0].IsDir || items[0].Name != "Cartafol ñ #1&2+3%" {
		t.Fatalf("fbList(%s) = %+v, %v; quere un só cartafol", dir, items, err)
	}

	// Upload (new and overwrite), with an awkward name; the listing size must be exact.
	file := sub + "/fich ñ (1).txt"
	if err := a.fbUploadDirect(file, []byte("primeiro")); err != nil {
		t.Fatalf("upload novo: %v", err)
	}
	body := strings.Repeat("0123456789", 1000) // 10000 B: not a multiple of the 4 KB block
	if err := a.fbUploadDirect(file, []byte(body)); err != nil {
		t.Fatalf("upload sobre un existente (override): %v", err)
	}
	if got, err := a.fbRawGet(file); err != nil || string(got) != body {
		t.Fatalf("descarga: %d bytes, %v; quere o contido novo (%d bytes)", len(got), err, len(body))
	}
	if !a.fbXaSubido(file, len(body)) {
		t.Error("fbXaSubido = false para un ficheiro subido co tamaño exacto (falta useLogicalSize no servidor?)")
	}
	if a.fbXaSubido(file, len(body)+1) {
		t.Error("fbXaSubido = true cun tamaño distinto")
	}

	// A missing file is a definitive error, not a retry loop (Quantum answers it with a 500).
	if _, err := a.fbRawGet(dir + "/non-existe.txt"); err == nil {
		t.Error("descargar un ficheiro que non existe debería dar erro")
	}
	if _, err := a.fbList(dir + "/non-existe"); !errors.Is(err, errFBNonAtopado) {
		t.Errorf("fbList dun cartafol que non existe = %v; quere errFBNonAtopado", err)
	}

	// The certificate fallback: the folder as a zip.
	if err := a.fbUploadDirect(sub+"/mia.p12", []byte("fake-p12")); err != nil {
		t.Fatalf("upload do .p12: %v", err)
	}
	if got, err := a.fbFetchCertFromZip(sub); err != nil || string(got) != "fake-p12" {
		t.Errorf("fbFetchCertFromZip = %q, %v", got, err)
	}

	if err := a.fbDeleteDirect(file); err != nil {
		t.Errorf("fbDeleteDirect: %v", err)
	}

	// Password change with characters that need URL-encoding, then back to the original.
	const especial = "Ñandú p+a%s$ 1"
	if err := a.ChangePassword(especial); err != nil {
		t.Fatalf("ChangePassword co contrasinal especial: %v", err)
	}
	if err := a.ChangePassword(pass); err != nil {
		t.Fatalf("ChangePassword de volta ao orixinal (deixou a conta cambiada!): %v", err)
	}
}

// TestQuantumRealAdminUsers exercises the administrator user management (create with
// password + permissions, list, change password, delete) against a REAL Quantum server.
// Skipped unless PIZTU_QUANTUM_URL, PIZTU_QUANTUM_ADMIN_USER and PIZTU_QUANTUM_ADMIN_PASS
// are set. It creates and deletes its own "tmp-real-alu" account, so it leaves the
// server as it found it.
func TestQuantumRealAdminUsers(t *testing.T) {
	base := os.Getenv("PIZTU_QUANTUM_URL")
	admin, adminPass := os.Getenv("PIZTU_QUANTUM_ADMIN_USER"), os.Getenv("PIZTU_QUANTUM_ADMIN_PASS")
	if base == "" || admin == "" || adminPass == "" {
		t.Skip("define PIZTU_QUANTUM_URL, PIZTU_QUANTUM_ADMIN_USER e PIZTU_QUANTUM_ADMIN_PASS para probar contra un Quantum real")
	}
	const alu = "tmp-real-alu"
	const pass = "Ñandú p+a%s$ 1" // needs URL-encoding in X-Password

	a := NewApp()
	a.dataDir = t.TempDir()
	base = normalizeServerURL(base)
	token, err := fbLogin(base, admin, adminPass)
	if err != nil {
		t.Fatalf("login de admin: %v", err)
	}
	a.fbURL, a.fbToken, a.fbAdminPass = base, token, adminPass
	const secret = "dixitalizacion/notas-privadas/tmp-real"
	// Quantum answers 403 when listing a folder whose ONLY content is denied to the
	// account, so the parent needs some other file, as dixitalizacion/ always has in real
	// use (profesores.json, roster...). Without it "o alumno segue vendo o resto" would
	// fail on a server that is fresh.
	const visible = "dixitalizacion/tmp-real-visible.json"
	cleanup := func() {
		a.fbDeleteDirect(visible)
		a.fbSetUserPrivacyRule(alu, false) // the rule outlives the account in Quantum
		if id, err := a.fbUserIDByName(alu); err == nil {
			a.fbDeleteUser(id)
		}
		a.fbDeleteDirect(secret)
	}
	cleanup() // leftovers of an interrupted run
	t.Cleanup(cleanup)

	if err := a.fbCreateUser(alu, pass); err != nil {
		t.Fatalf("fbCreateUser: %v", err)
	}
	// The new account can log in and work in its folder, but is not an admin.
	tok, err := fbLogin(base, alu, pass)
	if err != nil {
		t.Fatalf("o alumno recén creado non pode entrar: %v", err)
	}
	s := NewApp()
	s.dataDir = t.TempDir()
	s.fbURL, s.fbToken = base, tok
	if err := s.fbEnsureDirDirect("users/" + alu); err != nil {
		t.Errorf("o alumno non pode crear o seu cartafol: %v", err)
	}
	if err := s.fbUploadDirect("users/"+alu+"/a.txt", []byte("hola")); err != nil {
		t.Errorf("o alumno non pode subir un ficheiro: %v", err)
	}
	a.fbDeleteDirect("users/" + alu)

	// Privacy: a new student cannot read notas-privadas/ (403 in the whole subtree) but can
	// read the rest; making them a professor (lifting the rule) opens it at once.
	if err := a.fbUploadDirect(visible, []byte("[]")); err != nil {
		t.Fatalf("preparar dixitalizacion: %v", err)
	}
	if err := a.fbEnsureDirDirect(secret); err != nil {
		t.Fatalf("preparar notas-privadas: %v", err)
	}
	if err := a.fbUploadDirect(secret+"/n.json", []byte(`{"nota":"privada"}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.fbRawGet(secret + "/n.json"); err == nil {
		t.Errorf("o alumno leu unha nota privada: %q", got)
	}
	if _, err := s.fbList(secret); err == nil {
		t.Error("o alumno listou notas-privadas")
	}
	if _, err := s.fbList("dixitalizacion"); err != nil {
		t.Errorf("o alumno debe seguir vendo o resto de dixitalizacion: %v", err)
	}
	if err := a.fbSetUserPrivacyRule(alu, false); err != nil {
		t.Fatalf("quitar a regra: %v", err)
	}
	if got, err := s.fbRawGet(secret + "/n.json"); err != nil || string(got) != `{"nota":"privada"}` {
		t.Errorf("o profesor non le a nota: %q, %v", got, err)
	}
	if err := a.fbSetUserPrivacyRule(alu, false); err != nil { // already lifted: still fine
		t.Errorf("quitar unha regra que xa non está: %v", err)
	}
	if err := a.fbSetUserPrivacyRule(alu, true); err != nil {
		t.Fatalf("volver a poñela: %v", err)
	}
	if err := a.fbSetUserPrivacyRule(alu, true); err != nil { // already there: still fine
		t.Errorf("poñer unha regra que xa está: %v", err)
	}
	if _, err := s.fbRawGet(secret + "/n.json"); err == nil {
		t.Error("o alumno volveu a ler a nota tras repoñer a regra")
	}

	users, err := a.fbListUsers()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, u := range users {
		switch u["username"] {
		case alu:
			found = true
			if fbUserIsAdmin(u) {
				t.Error("o alumno saíu como admin")
			}
		case admin:
			if !fbUserIsAdmin(u) {
				t.Error("o admin non se recoñece como admin")
			}
		}
	}
	if !found {
		t.Fatal("o alumno non aparece na lista")
	}

	id, err := a.fbUserIDByName(alu)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.fbAdminSetPassword(id, "outra clave 2"); err != nil {
		t.Fatalf("fbAdminSetPassword: %v", err)
	}
	if _, err := fbLogin(base, alu, "outra clave 2"); err != nil {
		t.Errorf("o novo contrasinal non funciona: %v", err)
	}
	if err := a.fbDeleteUser(id); err != nil {
		t.Fatalf("fbDeleteUser: %v", err)
	}
	if _, err := fbLogin(base, alu, "outra clave 2"); err == nil {
		t.Error("o alumno borrado aínda pode entrar")
	}
}
