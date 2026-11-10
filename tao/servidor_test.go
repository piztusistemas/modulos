package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestCandidatasServidorPrefireOPorto80 comproba a orde de preferencia: para o
// host que xa se usaba, primeiro o porto 80 con /arquivos (a URL curta que se
// quere que vexa o alumnado) e despois as formas antigas.
func TestCandidatasServidorPrefireOPorto80(t *testing.T) {
	got := candidatasServidor("http://tux0.local:8080")
	if len(got) < 4 {
		t.Fatalf("candidatasServidor: esperaba varias candidatas, obtiven %v", got)
	}
	if got[0] != "http://tux0.local/arquivos" {
		t.Errorf("primeira candidata = %q, quería http://tux0.local/arquivos", got[0])
	}
	if !contén(got, "http://tux0.local:8080") {
		t.Errorf("falta a forma antiga http://tux0.local:8080 en %v", got)
	}
	// O host que xa usaba o equipo próbase antes ca localhost.
	if i, j := índice(got, "http://tux0.local/arquivos"), índice(got, "http://localhost/arquivos"); j >= 0 && i > j {
		t.Errorf("localhost próbase antes có host configurado: %v", got)
	}
}

// TestCandidatasServidorConservaRutaPersonalizada: se alguén publicou File
// Browser noutra subruta, esa próbase a primeira (senón perderíase ao detectar).
func TestCandidatasServidorConservaRutaPersonalizada(t *testing.T) {
	got := candidatasServidor("http://servidor.centro.gal/ficheiros")
	if got[0] != "http://servidor.centro.gal/ficheiros" {
		t.Errorf("primeira candidata = %q, quería conservar /ficheiros", got[0])
	}
}

// TestProbarRaizFileBrowser: só vale un 401/403 en POST /api/auth/renew. Un 200 non
// serve — File Browser é unha SPA e devolve a súa páxina a calquera ruta, así
// que unha subruta inexistente tamén respondería 200.
func TestProbarRaizFileBrowser(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/renew" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte("<!doctype html>"))
	}))
	defer api.Close()
	if !probarRaizFileBrowser(api.URL) {
		t.Error("probarRaizFileBrowser: non recoñeceu unha raíz válida")
	}
	if probarRaizFileBrowser(api.URL + "/arquivos") {
		t.Error("probarRaizFileBrowser: aceptou unha subruta que só devolve a SPA")
	}

	apache := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer apache.Close()
	if probarRaizFileBrowser(apache.URL) {
		t.Error("probarRaizFileBrowser: aceptou un servidor web que non é File Browser")
	}
}

func contén(lista []string, v string) bool { return índice(lista, v) >= 0 }

func índice(lista []string, v string) int {
	for i, x := range lista {
		if strings.EqualFold(x, v) {
			return i
		}
	}
	return -1
}

// O nome do servidor propóñense a partir do nome deste equipo: prefixo da aula + 0.
func TestHostsServidorDesde(t *testing.T) {
	casos := []struct {
		hostname string
		quere    []string
	}{
		{"bac17", []string{"bac0.local", "bac00.local"}}, // dous díxitos: tamén bac00
		{"bac01", []string{"bac0.local", "bac00.local"}},
		{"BAC01.local", []string{"bac0.local", "bac00.local"}},
		{"bac17.centro.gal", []string{"bac0.centro.gal", "bac00.centro.gal"}}, // mesmo dominio ca este equipo
		{"tux-3", []string{"tux-0.local"}},                                    // un só díxito: só tux-0
		{"bac0", []string{}},                                                  // este equipo xa é o servidor: cóbreo localhost
		{"portatil", nil},                                                     // sen número final: non hai prefixo de aula
		{"12345", nil},                                                        // só un número
		{"", nil},
	}
	for _, c := range casos {
		got := hostsServidorDesde(c.hostname)
		if len(got) != len(c.quere) {
			t.Errorf("hostsServidorDesde(%q) = %v; quere %v", c.hostname, got, c.quere)
			continue
		}
		for i := range got {
			if got[i] != c.quere[i] {
				t.Errorf("hostsServidorDesde(%q) = %v; quere %v", c.hostname, got, c.quere)
			}
		}
	}
}

// Un host a secas proba primeiro o 80 con /arquivos (a URL curta), despois o 80 raíz e o
// 8080, con e sen /arquivos.
func TestCandidatasDeHostSenPorto(t *testing.T) {
	got := candidatasDeHosts([]string{"bac0.local"}, "http://bac0.local")
	quere := []string{"http://bac0.local/arquivos", "http://bac0.local", "http://bac0.local:8080/arquivos", "http://bac0.local:8080"}
	if len(got) != len(quere) {
		t.Fatalf("candidatas = %v; quere %v", got, quere)
	}
	for i := range quere {
		if got[i] != quere[i] {
			t.Errorf("candidatas = %v; quere %v", got, quere)
			break
		}
	}
}

// resolverServidor completa a subruta que falta e serve igual co clásico e con Quantum;
// unha URL que xa vale queda tal cal; e un porto escrito non se cambia.
func TestResolverServidor(t *testing.T) {
	// File Browser publicado baixo /arquivos (o proxy de Apache), clásico e Quantum.
	baixoArquivos := func(inner http.Handler) *httptest.Server {
		return httptest.NewServer(http.StripPrefix("/arquivos", func() http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inner.ServeHTTP(w, r) })
		}()))
	}
	quantum := baixoArquivos(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/renew" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer quantum.Close()
	clasico := baixoArquivos(newFakeClasico("t", "u", "p").handler())
	defer clasico.Close()
	defer fbEsquecerSabor(quantum.URL + "/arquivos")
	defer fbEsquecerSabor(clasico.URL + "/arquivos")

	for nome, srv := range map[string]*httptest.Server{"quantum": quantum, "clásico": clasico} {
		if got := resolverServidor(srv.URL); got != srv.URL+"/arquivos" {
			t.Errorf("%s: resolverServidor(%q) = %q; quere %q", nome, srv.URL, got, srv.URL+"/arquivos")
		}
		// Sen esquema, como o teclearía o alumnado.
		if got := resolverServidor(strings.TrimPrefix(srv.URL, "http://")); got != srv.URL+"/arquivos" {
			t.Errorf("%s: sen http:// = %q", nome, got)
		}
		// Unha URL completa segue valendo tal cal.
		if got := resolverServidor(srv.URL + "/arquivos/"); got != srv.URL+"/arquivos" {
			t.Errorf("%s: URL completa = %q; quere que non cambie", nome, got)
		}
	}

	// Un servidor que non é File Browser non se "resolve" a nada, e un porto escrito
	// non salta a outro (nin ao 80 nin ao 8080 do mesmo host).
	apache := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer apache.Close()
	if got := resolverServidor(apache.URL); got != "" {
		t.Errorf("resolverServidor dun servidor que non é File Browser = %q; quere vacío", got)
	}
	if got := resolverServidor("127.0.0.1:1"); got != "" {
		t.Errorf("resolverServidor dun porto morto = %q; quere vacío (non saltar a outro porto)", got)
	}
}

// O nome deste mesmo equipo (bac0 en bac0) recoñécese en todas as súas formas, e outros non.
func TestEEsteEquipo(t *testing.T) {
	nome, err := os.Hostname()
	if err != nil || nome == "" {
		t.Skip("sen hostname")
	}
	curto, _, _ := strings.Cut(strings.ToLower(nome), ".")
	for _, h := range []string{nome, curto, strings.ToUpper(curto), curto + ".local"} {
		if !eEsteEquipo(h) {
			t.Errorf("eEsteEquipo(%q) = false; quere true (este equipo é %q)", h, nome)
		}
	}
	for _, h := range []string{"", curto + "-outro.local", "outro." + curto, "192.168.0.27"} {
		if eEsteEquipo(h) {
			t.Errorf("eEsteEquipo(%q) = true; quere false", h)
		}
	}
}

// Co nome deste equipo vaise polo loopback, e un porto escrito consérvase.
func TestResolverServidorNomeDesteEquipo(t *testing.T) {
	nome, err := os.Hostname()
	if err != nil || nome == "" {
		t.Skip("sen hostname")
	}
	curto, _, _ := strings.Cut(strings.ToLower(nome), ".")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/renew" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, porto, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	defer fbEsquecerSabor("http://127.0.0.1:" + porto)
	for _, entrada := range []string{curto + ".local:" + porto, curto + ":" + porto} {
		if got := resolverServidor(entrada); got != "http://127.0.0.1:"+porto {
			t.Errorf("resolverServidor(%q) = %q; quere http://127.0.0.1:%s (o porto escrito consérvase)", entrada, got, porto)
		}
	}
}
