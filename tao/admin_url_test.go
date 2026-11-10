package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestAdminSessionCorrixeUnhaURLVella reproduce o que bloqueou a un Director real: en
// admin.json quedara gardada a URL coa subruta "/arquivos" (do proxy de Apache) pero o
// servidor reinstalouse sen ela, así que o login contestaba un 404 desconcertante —
// que parece un problema de credenciais e non o é. A sesión de administración ten que
// dar coa URL boa por si soa e deixala gardada.
func TestAdminSessionCorrixeUnhaURLVella(t *testing.T) {
	// A detección proba tamén 127.0.0.1 nos portos 80 e 8080 (onde instala File Browser o
	// playbook): nunha máquina con Piztu instalado atopa ESE servidor real antes có falso
	// deste test, e o test non mide o que debe.
	for _, real := range []string{"http://127.0.0.1", "http://127.0.0.1:8080"} {
		if probarRaizFileBrowser(real+rutaArquivos) || probarRaizFileBrowser(real) {
			t.Skipf("hai un File Browser real en %s que interfire coa detección; proba noutra máquina", real)
		}
	}
	fake := newFakeFileBrowser("tok")
	// O servidor só responde na raíz: calquera cousa baixo /arquivos é 404, igual que
	// un File Browser sen --baseurl.
	inner := fake.handler("admin", "segredo")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/arquivos") {
			http.Error(w, "404 Not Found", http.StatusNotFound)
			return
		}
		// /api/renew é o que usa a detección para recoñecer unha raíz de File Browser.
		if r.URL.Path == "/api/renew" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner(w, r)
	}))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	a.isAdmin = true
	// Gárdase a man a configuración coa URL equivocada (SaveAdminConfig non a
	// aceptaría, precisamente porque non funciona).
	if err := a.gardarAdminConfigDirecto(AdminConfig{
		ServerURL: srv.URL + "/arquivos", FBAdminUser: "admin", FBAdminPass: "segredo",
	}); err != nil {
		t.Fatalf("preparar admin.json: %v", err)
	}

	base, token, _, err := a.fbAdminSession()
	if err != nil {
		t.Fatalf("a sesión de administración tiña que recuperarse soa: %v", err)
	}
	if base != srv.URL {
		t.Errorf("base = %q, quería %q (sen a subruta)", base, srv.URL)
	}
	if token == "" {
		t.Error("non se obtivo token")
	}

	// E a corrección queda gardada, para non repetir a busca en cada operación.
	raw, err := os.ReadFile(a.adminConfigPath())
	if err != nil {
		t.Fatalf("ler admin.json: %v", err)
	}
	var gardada AdminConfig
	json.Unmarshal(raw, &gardada)
	if gardada.ServerURL != srv.URL {
		t.Errorf("admin.json quedou con %q, quería %q", gardada.ServerURL, srv.URL)
	}
}
