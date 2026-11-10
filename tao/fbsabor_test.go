package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeClasico é un servidor en memoria que imita o File Browser CLÁSICO (v2.63): login
// JSON con 403 se falla, token só en X-Auth, rutas na URL (/api/resources/a/b, a barra
// final crea un cartafol), listado en "items" con isDir, descarga por /api/raw e un
// ficheiro que falta responde 404. É o espello de fakeFileBrowser (Quantum).
type fakeClasico struct {
	token, user, pass string
	files             map[string][]byte
	dirs              map[string]bool
	// reqs recolle "MÉTODO ruta" de cada petición, para comprobar que Tao usa as rutas do clásico.
	reqs []string
	// mu serializa o handler: a sondaxe de detección lanza dúas peticións á vez.
	mu sync.Mutex
}

func newFakeClasico(token, user, pass string) *fakeClasico {
	return &fakeClasico{token: token, user: user, pass: pass, files: map[string][]byte{}, dirs: map[string]bool{}}
}

func (f *fakeClasico) handler() http.Handler {
	auth := func(r *http.Request) bool { return r.Header.Get("X-Auth") == f.token }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
		p := r.URL.Path
		switch {
		case p == "/api/login" && r.Method == http.MethodPost:
			var in struct{ Username, Password string }
			json.NewDecoder(r.Body).Decode(&in)
			if in.Username != f.user || in.Password != f.pass {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			io.WriteString(w, f.token)
		case p == "/api/renew":
			if !auth(r) {
				http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
				return
			}
			io.WriteString(w, f.token)
		case strings.HasPrefix(p, "/api/raw/"):
			if !auth(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			rel := strings.Trim(strings.TrimPrefix(p, "/api/raw/"), "/")
			if r.URL.Query().Get("algo") == "zip" {
				var buf bytes.Buffer
				zw := zip.NewWriter(&buf)
				for fp, data := range f.files {
					if strings.HasPrefix(fp, rel+"/") {
						w, _ := zw.Create(path.Base(fp))
						w.Write(data)
					}
				}
				zw.Close()
				w.Write(buf.Bytes())
				return
			}
			data, ok := f.files[rel]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(data)
		case strings.HasPrefix(p, "/api/resources/") || p == "/api/resources":
			if !auth(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			rel := strings.Trim(strings.TrimPrefix(p, "/api/resources"), "/")
			switch r.Method {
			case http.MethodPost:
				if strings.HasSuffix(p, "/") { // barra final = cartafol
					for d := rel; d != "" && d != "."; d = path.Dir(d) {
						f.dirs[d] = true
					}
					return
				}
				data, _ := io.ReadAll(r.Body)
				f.files[rel] = data
			case http.MethodDelete:
				delete(f.files, rel)
				delete(f.dirs, rel)
			case http.MethodGet:
				if !f.dirs[rel] && rel != "" {
					http.NotFound(w, r)
					return
				}
				items := []map[string]any{}
				prefix := rel + "/"
				if rel == "" {
					prefix = ""
				}
				for fp, data := range f.files {
					if rest := strings.TrimPrefix(fp, prefix); strings.HasPrefix(fp, prefix) && !strings.Contains(rest, "/") {
						items = append(items, map[string]any{"name": rest, "isDir": false, "size": len(data)})
					}
				}
				for d := range f.dirs {
					if rest := strings.TrimPrefix(d, prefix); strings.HasPrefix(d, prefix) && rest != "" && !strings.Contains(rest, "/") {
						items = append(items, map[string]any{"name": rest, "isDir": true})
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"items": items})
			}
		default:
			http.NotFound(w, r)
		}
	})
}

// fakeJWTClasico é un JWT co id no formato do clásico (user.id), non no de Quantum.
func fakeJWTClasico(userID int) string {
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]string{"alg": "HS256"}) + "." + enc(map[string]any{"user": map[string]any{"id": userID}}) + ".sig"
}

// A sondaxe distingue os dous servidores pola ruta de renovación que existe: só un
// 401/403 conta. Unha SPA que devolve 200 a todo, un proxy que pide login para todo e
// un servidor apagado non deben pasar por ningún dos dous.
func TestFbSondearSabor(t *testing.T) {
	respostas := func(quantum, clasico int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/auth/renew" && quantum != 0:
				w.WriteHeader(quantum)
			case r.URL.Path == "/api/renew" && clasico != 0:
				w.WriteHeader(clasico)
			default:
				io.WriteString(w, "<!doctype html>") // a SPA: 200 a calquera ruta
			}
		})
	}
	casos := []struct {
		nome    string
		handler http.Handler
		quere   fbSabor
	}{
		{"quantum", respostas(401, 0), fbQuantum},
		{"quantum con 403", respostas(403, 0), fbQuantum},
		{"clásico", respostas(0, 401), fbClasico},
		{"só a SPA", respostas(0, 0), fbSaborIndeterminado},
		{"proxy que pide login a todo", respostas(401, 401), fbSaborIndeterminado},
		{"rutas que existen pero co método mal (405)", respostas(405, 405), fbSaborIndeterminado},
		{"redirección (non se segue)", http.RedirectHandler("/arquivos/", http.StatusMovedPermanently), fbSaborIndeterminado},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			if got := fbSondearSabor(srv.URL); got != c.quere {
				t.Errorf("fbSondearSabor = %v; quere %v", got, c.quere)
			}
		})
	}
	if got := fbSondearSabor("http://127.0.0.1:1"); got != fbSaborIndeterminado {
		t.Errorf("servidor apagado = %v; quere indeterminado", got)
	}
}

// probarRaizFileBrowser (o que usa a autodetección da URL) acepta os dous e lembra cal é.
func TestProbarRaizFileBrowserAceptaOsDous(t *testing.T) {
	fake := newFakeClasico("t", "u", "p")
	clasico := httptest.NewServer(fake.handler())
	defer clasico.Close()
	defer fbEsquecerSabor(clasico.URL)
	if !probarRaizFileBrowser(clasico.URL) {
		t.Fatal("non recoñeceu un File Browser clásico")
	}
	if got := fbSaborDe(clasico.URL); got != fbClasico {
		t.Errorf("fbSaborDe = %v tras recoñecelo; quere clásico", got)
	}
}

// Sen sondaxe concluínte, Quantum é o tipo por defecto, e non se volve sondear en cada
// chamada (unha cola sen conexión non pode gastar 2 s por operación).
func TestFbSaborDeReservaEQuantum(t *testing.T) {
	var sondaxes int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "renew") {
			atomic.AddInt32(&sondaxes, 1)
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	defer fbEsquecerSabor(srv.URL)
	for i := 0; i < 5; i++ {
		if got := fbSaborDe(srv.URL); got != fbQuantum {
			t.Fatalf("fbSaborDe = %v; quere Quantum por defecto", got)
		}
	}
	if n := atomic.LoadInt32(&sondaxes); n != 2 { // unha sondaxe = as dúas rutas
		t.Errorf("fixo %d peticións de sondaxe en 5 chamadas; quere 2 (unha soa sondaxe)", n)
	}
}

// Un fallo pasaxeiro non fai que un clásico xa recoñecido pase a tratarse como Quantum.
func TestFbDetectarSaborMantenOUltimoBo(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	fake := newFakeClasico("t", "u", "p")
	h := fake.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	defer fbEsquecerSabor(srv.URL)
	if got := fbDetectarSabor(srv.URL); got != fbClasico {
		t.Fatalf("primeira sondaxe = %v; quere clásico", got)
	}
	up.Store(false)
	if got := fbDetectarSabor(srv.URL); got != fbClasico {
		t.Errorf("con o servidor caído = %v; quere seguir crendo que é clásico", got)
	}
}

// fbAuth vale para os dous: cada servidor le só a súa cabeceira.
func TestFbAuthEnviaAsDuasCabeceiras(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/", nil)
	fbAuth(req, "tok")
	if req.Header.Get("X-Auth") != "tok" || req.Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("cabeceiras = %v; quere X-Auth e Authorization: Bearer", req.Header)
	}
}

// As URLs que cambian segundo o tipo de servidor.
func TestFbURLsPorSabor(t *testing.T) {
	const base = "http://srv/arquivos"
	defer fbEsquecerSabor(base)

	fbRecordarSabor(base, fbClasico)
	q := func(u string) url.Values { p, _ := url.Parse(u); return p.Query() }
	if got := fbResourceURL(base, "/users/ana/a b.txt", url.Values{"override": {"true"}}); got != base+"/api/resources/users/ana/a%20b.txt?override=true" {
		t.Errorf("subida no clásico = %s", got)
	}
	if got := fbResourceURL(base, "users/ana", url.Values{"isDir": {"true"}}); got != base+"/api/resources/users/ana/" {
		t.Errorf("crear cartafol no clásico = %s (a barra final é o que o distingue dun ficheiro)", got)
	}
	if got := fbResourceURL(base, "users/ana", nil); got != base+"/api/resources/users/ana" {
		t.Errorf("listar/borrar no clásico = %s", got)
	}
	if got := fbDownloadURLs(base, "users/ana/f.txt", false); len(got) != 1 || got[0] != base+"/api/raw/users/ana/f.txt" {
		t.Errorf("descarga no clásico = %v", got)
	}
	if got := fbDownloadURLs(base, "users/ana", true); len(got) != 2 || got[0] != base+"/api/raw/users/ana?algo=zip" || got[1] != base+"/api/raw/users/ana/?algo=zip" {
		t.Errorf("zip no clásico = %v; quere con e sen barra final", got)
	}
	if got := fbUserURL(base, 7); got != base+"/api/users/7" {
		t.Errorf("fbUserURL clásico = %s", got)
	}
	if got := fbURLWeb(base, "dixitalizacion/practicas"); got != base+"/files/dixitalizacion/practicas/" {
		t.Errorf("fbURLWeb clásico = %s", got)
	}

	fbRecordarSabor(base, fbQuantum)
	got := fbResourceURL(base, "users/ana", url.Values{"isDir": {"true"}})
	if v := q(got); !strings.HasPrefix(got, base+"/api/resources?") || v.Get("source") != fbSource || v.Get("path") != "/users/ana" || v.Get("isDir") != "true" {
		t.Errorf("crear cartafol en Quantum = %s", got)
	}
	if d := fbDownloadURLs(base, "users/ana/f.txt", false); len(d) != 1 || !strings.HasPrefix(d[0], base+"/api/resources/download?") || q(d[0]).Get("file") != "/users/ana/f.txt" {
		t.Errorf("descarga en Quantum = %v", d)
	}
	if got := fbUserURL(base, 7); got != base+"/api/users?id=7" {
		t.Errorf("fbUserURL Quantum = %s", got)
	}
	if got := fbURLWeb(base, "dixitalizacion"); got != base+"/files/"+fbSource+"/dixitalizacion/" {
		t.Errorf("fbURLWeb Quantum = %s", got)
	}
}

func TestFbUserIDAmbosFormatos(t *testing.T) {
	if id, err := fbUserID(fakeJWTClasico(9)); err != nil || id != 9 {
		t.Errorf("token do clásico: fbUserID = %d, %v; quere 9", id, err)
	}
	if id, err := fbUserID(fakeJWT(9)); err != nil || id != 9 {
		t.Errorf("token de Quantum: fbUserID = %d, %v; quere 9", id, err)
	}
}

func TestFbUserIsAdminAmbosFormatos(t *testing.T) {
	admin := func(clave string, valor bool) map[string]any {
		return map[string]any{"username": "x", clave: map[string]any{"admin": valor}}
	}
	if !fbUserIsAdmin(admin("perm", true)) || !fbUserIsAdmin(admin("permissions", true)) {
		t.Error("un admin (perm do clásico ou permissions de Quantum) non se recoñece")
	}
	if fbUserIsAdmin(admin("perm", false)) || fbUserIsAdmin(admin("permissions", false)) || fbUserIsAdmin(map[string]any{}) {
		t.Error("un non admin saíu como admin")
	}
}

func TestFbListFormatoClasico(t *testing.T) {
	fake := newFakeClasico("t", "u", "p")
	fake.dirs["d"], fake.dirs["d/sub"] = true, true
	fake.files["d/a.txt"] = []byte("12345")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	defer fbEsquecerSabor(srv.URL)
	fbRecordarSabor(srv.URL, fbClasico)

	a := NewApp()
	a.fbURL, a.fbToken = srv.URL, "t"
	items, err := a.fbList("d")
	if err != nil || len(items) != 2 {
		t.Fatalf("fbList = %+v, %v; quere un cartafol e un ficheiro", items, err)
	}
	for _, it := range items {
		switch it.Name {
		case "sub":
			if !it.IsDir {
				t.Error("sub debería ser cartafol")
			}
		case "a.txt":
			if it.IsDir || it.Size != 5 {
				t.Errorf("a.txt = %+v; quere ficheiro de 5 bytes", it)
			}
		default:
			t.Errorf("entrada inesperada %q", it.Name)
		}
	}
	if _, err := a.fbList("non-existe"); !errors.Is(err, errFBNonAtopado) {
		t.Errorf("fbList dun cartafol que falta = %v; quere errFBNonAtopado (404 do clásico)", err)
	}
}

// O fluxo completo do alumnado (login, backup, cartafol, certificado en zip) contra un
// servidor clásico: mesma experiencia ca contra Quantum, sen configurar nada.
func TestFileBrowserClasicoPushPullRoundTrip(t *testing.T) {
	fake := newFakeClasico("tok-clasico", "alumno", "segredo")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	defer fbEsquecerSabor(srv.URL)

	a := NewApp()
	a.dataDir = t.TempDir()
	prof, err := a.loginServer("alumno", "segredo", srv.URL)
	if err != nil || !prof.Connected {
		t.Fatalf("loginServer = %+v, %v", prof, err)
	}
	if got := fbSaborDe(srv.URL); got != fbClasico {
		t.Fatalf("tras o login o tipo é %v; quere clásico", got)
	}
	if got, err := a.PullBackup(); err != nil || got != "" {
		t.Fatalf("PullBackup sen copia = %q, %v", got, err)
	}
	payload := `[{"id":"1","name":"Tecnoloxía"}]`
	if _, err := a.PushBackup(payload); err != nil {
		t.Fatalf("PushBackup: %v", err)
	}
	fake.mu.Lock()
	_, okBackup := fake.files["users/alumno/"+fbBackupName]
	fake.mu.Unlock()
	if !okBackup {
		t.Fatalf("a copia non está en users/alumno/: %v", fake.files)
	}
	if got, err := a.PullBackup(); err != nil || got != payload {
		t.Fatalf("PullBackup = %q, %v; quere %q", got, err, payload)
	}

	// Certificado: pola descarga directa e, se falla, polo zip do cartafol.
	fake.mu.Lock()
	fake.files["users/alumno/mia.p12"] = []byte("fake-p12")
	fake.mu.Unlock()
	if got, err := a.fbFetchCertFromZip("users/alumno"); err != nil || string(got) != "fake-p12" {
		t.Errorf("fbFetchCertFromZip = %q, %v", got, err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, r := range fake.reqs {
		// A sondaxe de detección pregunta pola ruta de Quantum de propósito (é como se
		// distingue); calquera outra ruta de Quantum sería un erro.
		if r == "POST /api/auth/renew" {
			continue
		}
		if strings.Contains(r, "/api/auth/") || strings.Contains(r, "source=") {
			t.Errorf("Tao usou unha ruta de Quantum contra o clásico: %s", r)
		}
	}
}

func TestFbLoginClasico(t *testing.T) {
	fake := newFakeClasico("tok", "alu", "clave")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	defer fbEsquecerSabor(srv.URL)

	if tok, err := fbLogin(srv.URL, "alu", "clave"); err != nil || tok != "tok" {
		t.Fatalf("fbLogin = %q, %v", tok, err)
	}
	if _, err := fbLogin(srv.URL, "alu", "mala"); !errors.Is(err, errFBBadCredentials) {
		t.Errorf("con contrasinal malo = %v; quere errFBBadCredentials (o clásico responde 403)", err)
	}
}

// Se a sondaxe non concluíu pero o servidor si está (p.ex. un proxy que bloquea as
// rutas de renovación), o login proba Quantum e, ao non existir esa ruta, o clásico.
func TestFbLoginReservaSenSondaxe(t *testing.T) {
	var loginsQuantum, loginsClasico int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			atomic.AddInt32(&loginsQuantum, 1)
			http.NotFound(w, r)
		case "/api/login":
			atomic.AddInt32(&loginsClasico, 1)
			io.WriteString(w, "tok-clasico")
		default:
			http.NotFound(w, r) // as rutas de renovación tampouco respondem: sondaxe sen concluír
		}
	}))
	defer srv.Close()
	defer fbEsquecerSabor(srv.URL)

	tok, err := fbLogin(srv.URL, "alu", "x")
	if err != nil || tok != "tok-clasico" {
		t.Fatalf("fbLogin = %q, %v; quere o token do clásico", tok, err)
	}
	if loginsQuantum != 1 || loginsClasico != 1 {
		t.Errorf("logins Quantum=%d clásico=%d; quere 1 e 1", loginsQuantum, loginsClasico)
	}
	if got := fbSaborDe(srv.URL); got != fbClasico {
		t.Errorf("tras o login fbSaborDe = %v; quere clásico lembrado", got)
	}
}

// A regra de notas privadas do clásico é unha regex DENTRO do usuario, co "regexp.raw"
// que o servidor necesita (sen el rebenta con nil pointer e o alumnado non le nada).
func TestRegraNotasPrivadasClasico(t *testing.T) {
	r := regraNotasPrivadas()
	raw, _ := r["regexp"].(map[string]any)["raw"].(string)
	if r["allow"] != false || r["regex"] != true || raw != notasPrivadasRuleRegex || r["path"] != notasPrivadasRuleRegex {
		t.Fatalf("regra = %v; quere allow=false, regex=true e path/regexp.raw = %q", r, notasPrivadasRuleRegex)
	}
	if notasPrivadasRuleRegex != `^/?dixitalizacion/notas-privadas(/|$)` {
		t.Errorf("notasPrivadasRuleRegex = %q", notasPrivadasRuleRegex)
	}
	// Quitar a regra conserva as outras que puxese un admin á man.
	propia := map[string]any{"path": "/outra", "allow": false}
	kept := withoutNotasPrivadasRule([]any{regraNotasPrivadas(), propia})
	if len(kept) != 1 || kept[0].(map[string]any)["path"] != "/outra" {
		t.Errorf("withoutNotasPrivadasRule = %v; quere só a regra propia", kept)
	}
}
