package main

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strings"
	"testing"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
	"sync/atomic"
	"time"
)

// fakeFileBrowser is a minimal in-memory stand-in for a File Browser QUANTUM server
// (v1.5.6), implementing just the endpoints the app uses: POST /api/auth/login and
// /api/auth/renew, /api/resources (create dir, upload, list, delete), /api/resources/download
// and /api/users. It reproduces the quirks the client has to cope with, all checked against
// a real server: paths travel in the query (?source=&path=), a folder needs isDir=true (a
// bare trailing slash creates an empty FILE), duplicates answer 409, a listing splits
// "files" from "folders", a missing download is a 500 (not a 404), and X-Password is
// URL-encoded.
type fakeFileBrowser struct {
	token    string
	userID   int               // id served by /api/users and expected in ?id=
	password string            // current account password (updated by PUT /api/users)
	files    map[string][]byte // remote path -> content
	dirs     map[string]bool   // remote path (no trailing slash) -> exists
	// accounts is the list served by GET /api/users (the administrator listing).
	// Left nil, the endpoint answers with an empty list.
	accounts    []map[string]any
	newPassword string // last password set through PUT /api/users
}

func newFakeFileBrowser(token string) *fakeFileBrowser {
	return &fakeFileBrowser{
		token:  token,
		userID: 1,
		files:  map[string][]byte{},
		dirs:   map[string]bool{},
	}
}

// mkdirAll records dir and every parent as existing, like the real MkdirAll.
func (f *fakeFileBrowser) mkdirAll(dir string) {
	for dir != "" && dir != "." {
		f.dirs[dir] = true
		dir = path.Dir(dir)
	}
}

// listing builds Quantum's answer for a directory: a "files" array and a "folders"
// array, with no per-entry isDir.
func (f *fakeFileBrowser) listing(dir string) map[string]any {
	files, folders := []map[string]any{}, []map[string]any{}
	prefix := dir + "/"
	if dir == "" {
		prefix = ""
	}
	for p, data := range f.files {
		if rest := strings.TrimPrefix(p, prefix); strings.HasPrefix(p, prefix) && rest != "" && !strings.Contains(rest, "/") {
			files = append(files, map[string]any{"name": rest, "size": len(data), "type": "blob"})
		}
	}
	for d := range f.dirs {
		if rest := strings.TrimPrefix(d, prefix); strings.HasPrefix(d, prefix) && rest != "" && !strings.Contains(rest, "/") {
			folders = append(folders, map[string]any{"name": rest, "size": 4096, "type": "directory"})
		}
	}
	return map[string]any{"name": path.Base("/" + dir), "type": "directory", "path": "/" + dir + "/", "source": fbSource, "files": files, "folders": folders}
}

func (f *fakeFileBrowser) handler(user, pass string) http.HandlerFunc {
	f.password = pass
	authorised := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer "+f.token }
	// notFound is what Quantum says for a missing file on /download: a 500.
	notFound := func(w http.ResponseWriter, p string) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"status":500,"message":"could not resolve symlinks for /srv/%s: lstat /srv/%s: no such file or directory"}`, p, p)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost:
			// Quantum URL-decodes X-Password, so a raw "ñ" or "%" would not match.
			pw, err := url.QueryUnescape(r.Header.Get("X-Password"))
			if q.Get("username") != user || err != nil || pw != f.password {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"status":401,"message":"user unauthorized"}`)
				return
			}
			io.WriteString(w, f.token)

		case r.URL.Path == "/api/auth/renew" && r.Method == http.MethodPost:
			if !authorised(r) {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"status":401,"message":"no token present in request"}`)
				return
			}
			io.WriteString(w, f.token)

		case r.URL.Path == "/api/resources/download":
			if !authorised(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if q.Get("source") != fbSource {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			p := strings.Trim(q.Get("file"), "/")
			if q.Get("algo") == "zip" {
				if !f.dirs[p] {
					notFound(w, p)
					return
				}
				zw := zip.NewWriter(w)
				for fp, data := range f.files {
					if strings.HasPrefix(fp, p+"/") {
						fw, _ := zw.Create(strings.TrimPrefix(fp, p+"/"))
						fw.Write(data)
					}
				}
				zw.Close()
				return
			}
			data, ok := f.files[p]
			if !ok {
				notFound(w, p)
				return
			}
			w.Write(data)

		case r.URL.Path == "/api/resources":
			if !authorised(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if q.Get("source") != fbSource {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			p := strings.Trim(q.Get("path"), "/")
			switch r.Method {
			case http.MethodPost:
				if q.Get("isDir") == "true" {
					if _, isFile := f.files[p]; isFile || f.dirs[p] {
						w.WriteHeader(http.StatusConflict)
						return
					}
					f.mkdirAll(p)
					w.WriteHeader(http.StatusOK)
					return
				}
				// Anything else is a FILE, whatever the trailing slash says.
				if _, exists := f.files[p]; exists && q.Get("override") != "true" {
					w.WriteHeader(http.StatusConflict)
					return
				}
				data, _ := io.ReadAll(r.Body)
				f.files[p] = data
				f.mkdirAll(path.Dir(p)) // the server creates the missing parents itself
				w.WriteHeader(http.StatusOK)
			case http.MethodGet:
				if p != "" && !f.dirs[p] {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				json.NewEncoder(w).Encode(f.listing(p))
			case http.MethodDelete:
				if _, ok := f.files[p]; ok {
					delete(f.files, p)
					w.WriteHeader(http.StatusOK)
					return
				}
				// Directory: the real server removes it with everything inside.
				if f.dirs[p] {
					delete(f.dirs, p)
					for fp := range f.files {
						if strings.HasPrefix(fp, p+"/") {
							delete(f.files, fp)
						}
					}
					for d := range f.dirs {
						if strings.HasPrefix(d, p+"/") {
							delete(f.dirs, d)
						}
					}
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}

		case r.URL.Path == "/api/users":
			if !authorised(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Without ?id= Quantum lists every account (the administrator listing).
			if !q.Has("id") {
				accounts := f.accounts
				if accounts == nil {
					accounts = []map[string]any{}
				}
				json.NewEncoder(w).Encode(accounts)
				return
			}
			if q.Get("id") != fmt.Sprint(f.userID) {
				w.WriteHeader(http.StatusForbidden) // only self is addressable
				return
			}
			switch r.Method {
			case http.MethodGet:
				json.NewEncoder(w).Encode(map[string]any{
					"id":          f.userID,
					"username":    user,
					"scopes":      []map[string]any{{"name": fbSource, "scope": "/users/" + user}},
					"locale":      "gl",
					"permissions": map[string]any{"admin": false},
				})
			case http.MethodPut:
				var body struct {
					Which []string `json:"which"`
					Data  struct {
						Password string `json:"password"`
					} `json:"data"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				if len(body.Which) != 1 || body.Which[0] != "password" || body.Data.Password == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				// Quantum requires the actor's current password (URL-encoded) in X-Password.
				if cur, err := url.QueryUnescape(r.Header.Get("X-Password")); err != nil || cur != f.password {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, "the current password is incorrect")
					return
				}
				f.newPassword = body.Data.Password
				f.password = body.Data.Password // subsequent logins use the new one
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestFileBrowserPushPullRoundTrip(t *testing.T) {
	fake := newFakeFileBrowser("tok-123")
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir() // so loadProfiles simply finds nothing

	prof, err := a.loginServer("alumno", "segredo", srv.URL)
	if err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	if !prof.Connected || prof.Username != "alumno" {
		t.Fatalf("perfil inesperado: %+v", prof)
	}

	// No backups yet.
	got, err := a.PullBackup()
	if err != nil {
		t.Fatalf("PullBackup (baleiro): %v", err)
	}
	if got != "" {
		t.Fatalf("esperábase copia baleira, obtívose %q", got)
	}

	// Push a backup, then pull it back.
	payload := `[{"id":"1","name":"Tecnoloxía"}]`
	name, err := a.PushBackup(payload)
	if err != nil {
		t.Fatalf("PushBackup: %v", err)
	}
	if name != fbBackupName {
		t.Fatalf("nome de copia inesperado: %q", name)
	}
	// The backup lands in the student's own folder, users/<login>.
	if _, ok := fake.files["users/alumno/"+fbBackupName]; !ok {
		t.Fatalf("a copia non se gardou en users/alumno/; ficheiros: %v", fake.files)
	}

	got, err = a.PullBackup()
	if err != nil {
		t.Fatalf("PullBackup: %v", err)
	}
	if got != payload {
		t.Fatalf("round-trip: esperábase %q, obtívose %q", payload, got)
	}
}

func TestFileBrowserFetchesServerCert(t *testing.T) {
	fake := newFakeFileBrowser("tok-123")
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	// Seed the student's server folder with a certificate.
	certBytes := []byte("fake-p12-content")
	fake.dirs["users/alumno"] = true
	fake.files["users/alumno/miña-firma.p12"] = certBytes

	a := NewApp()
	a.dataDir = t.TempDir()

	prof, err := a.loginServer("alumno", "segredo", srv.URL)
	if err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	if !prof.HasCert || prof.CertPath == "" {
		t.Fatalf("esperábase certificado do servidor, perfil: %+v", prof)
	}
	got, err := os.ReadFile(prof.CertPath)
	if err != nil {
		t.Fatalf("lendo o certificado descargado: %v", err)
	}
	if string(got) != string(certBytes) {
		t.Fatalf("contido do certificado inesperado: %q", got)
	}
}

func TestFileBrowserLoginWrongPassword(t *testing.T) {
	fake := newFakeFileBrowser("tok-123")
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()

	if _, err := a.loginServer("alumno", "malo", srv.URL); err == nil {
		t.Fatal("esperábase erro de credenciais")
	}
}

// fakeJWT builds a minimal JWT (header.payload.signature) whose payload carries the
// given File Browser user id (Quantum's top-level "belongsTo" claim), so fbUserID can extract it just like a real token.
func fakeJWT(userID int) string {
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	header := enc(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload := enc(map[string]any{"belongsTo": userID})
	return header + "." + payload + ".signature"
}

func TestChangePassword(t *testing.T) {
	fake := newFakeFileBrowser(fakeJWT(7))
	fake.userID = 7 // must match the id encoded in the JWT above
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()

	if _, err := a.loginServer("alumno", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	// The current password (taken from the session, i.e. what was used at login) is
	// sent to the server; a stale/wrong one must be rejected with File Browser's
	// "current password is incorrect" response and change nothing.
	a.fbPassword = "malo"
	if err := a.ChangePassword("novo1234"); err == nil {
		t.Fatal("esperábase erro co contrasinal actual incorrecto")
	}
	if fake.newPassword != "" {
		t.Fatalf("non se debía cambiar o contrasinal, obtívose %q", fake.newPassword)
	}

	// With the right current password (as stored at login) the change goes through.
	a.fbPassword = "segredo"
	if err := a.ChangePassword("novo1234"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if fake.newPassword != "novo1234" {
		t.Fatalf("o servidor non recibiu o novo contrasinal; obtívose %q", fake.newPassword)
	}
	// The session must now track the new password for a future change.
	if a.fbPassword != "novo1234" {
		t.Fatalf("a sesión non actualizou o contrasinal; ten %q", a.fbPassword)
	}
}

func TestRegenerateCertificate(t *testing.T) {
	fake := newFakeFileBrowser(fakeJWT(3))
	fake.userID = 3
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("alumno", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	// A stale certificate already in the folder must be removed.
	fake.dirs["users/alumno"] = true
	fake.files["users/alumno/vello.p12"] = []byte("vello")

	prof, err := a.RegenerateCertificate("Brais", "Castro Rodriguez")
	if err != nil {
		t.Fatalf("RegenerateCertificate: %v", err)
	}
	if prof.Name != "Brais" || prof.Surname != "Castro Rodriguez" {
		t.Fatalf("o perfil non recolleu o nome real: %+v", prof)
	}
	if !prof.HasCert || prof.CertPath == "" {
		t.Fatalf("perfil sen certificado: %+v", prof)
	}
	if _, ok := fake.files["users/alumno/vello.p12"]; ok {
		t.Fatal("o certificado vello debería terse borrado")
	}

	const newRemote = "users/alumno/brais_castrorodriguez.p12"
	pfx, ok := fake.files[newRemote]
	if !ok {
		t.Fatalf("non se atopou o novo certificado en %q", newRemote)
	}
	// The uploaded archive must open with the login password and carry the name.
	_, cert, _, err := pkcs12.DecodeChain(pfx, "segredo")
	if err != nil {
		t.Fatalf("o .p12 subido non abre co contrasinal do login: %v", err)
	}
	if cert.Subject.CommonName != "Brais Castro Rodriguez" {
		t.Fatalf("CN inesperado no certificado subido: %q", cert.Subject.CommonName)
	}
}

func TestChangePasswordNotConnected(t *testing.T) {
	a := NewApp()
	if err := a.ChangePassword("b1234"); err == nil {
		t.Fatal("esperábase erro sen conexión ao servidor")
	}
}

func TestFbUserID(t *testing.T) {
	if id, err := fbUserID(fakeJWT(42)); err != nil || id != 42 {
		t.Fatalf("fbUserID = %d, %v; quere 42, nil", id, err)
	}
	if _, err := fbUserID("non-e-un-jwt"); err == nil {
		t.Fatal("esperábase erro cun token non válido")
	}
}

func TestNormalizeServerURL(t *testing.T) {
	cases := map[string]string{
		"192.168.0.22:8080":         "http://192.168.0.22:8080",
		"http://192.168.0.22:8080/": "http://192.168.0.22:8080",
		"https://files.example.gal": "https://files.example.gal",
		"  ":                        "",
	}
	for in, want := range cases {
		if got := normalizeServerURL(in); got != want {
			t.Errorf("normalizeServerURL(%q) = %q, quere %q", in, got, want)
		}
	}
}

// TestFbListReintentaTrasEOF reproduce o fallo visto en produción: a primeira petición
// morre sen resposta ("EOF", unha conexión keep-alive que o servidor xa pechara) e o
// listado quedaba baleiro, o que Tao interpretaba como "non hai materias". fbList ten
// que reintentar, coma fbRawGet.
func TestFbListReintentaTrasEOF(t *testing.T) {
	var intentos int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&intentos, 1) == 1 {
			// Pecha a conexión de golpe, sen escribir resposta: o cliente ve EOF.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("o servidor de proba non permite pechar a conexión a man")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"files": []fbEntry{{Name: "dix3eso.json"}}, "folders": []fbEntry{}})
	}))
	defer srv.Close()

	// The EOF is meant for the LIST request, not for the flavor probe that would otherwise
	// be the first thing to reach the server.
	fbRecordarSabor(srv.URL, fbQuantum)
	defer fbEsquecerSabor(srv.URL)
	a := NewApp()
	a.fbURL, a.fbToken = srv.URL, "tok"
	items, err := a.fbList("dixitalizacion/materias")
	if err != nil {
		t.Fatalf("fbList: %v", err)
	}
	if len(items) != 1 || items[0].Name != "dix3eso.json" {
		t.Fatalf("items = %+v, quere a materia listada no segundo intento", items)
	}
	if got := atomic.LoadInt32(&intentos); got != 2 {
		t.Fatalf("intentos = %d, quere 2 (o primeiro fallou con EOF)", got)
	}
}

// Quantum answers 429 + Retry-After after ~8 logins from one IP (and the Apache proxy
// makes the whole classroom one IP): fbLogin must wait as told and retry, not fail.
func TestFbLoginWaitsOutTooManyRequests(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			http.NotFound(w, r) // the flavor probe: inconclusive, so fbLogin tries Quantum
			return
		}
		calls++
		if calls <= 2 {
			w.Header().Set("Retry-After", "6")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, "tok-ok")
	}))
	defer srv.Close()

	var esperas []time.Duration
	old := fbDormir
	fbDormir = func(d time.Duration) { esperas = append(esperas, d) }
	defer func() { fbDormir = old }()

	tok, err := fbLogin(srv.URL, "alu", "x")
	if err != nil || tok != "tok-ok" {
		t.Fatalf("fbLogin = %q, %v; quere o token tras esperar", tok, err)
	}
	if len(esperas) != 2 || esperas[0] != 6*time.Second {
		t.Errorf("esperas = %v; quere dúas de 6 s (o Retry-After)", esperas)
	}

	// A server that never lets go is given up on, with a clear message and a bounded wait.
	calls, esperas = -1000, nil
	if _, err := fbLogin(srv.URL, "alu", "x"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("fbLogin con 429 permanente = %v; quere un erro que o mencione", err)
	}
	if len(esperas) != fbLoginIntentos-1 {
		t.Errorf("esperou %d veces; quere %d", len(esperas), fbLoginIntentos-1)
	}
}

// The brute-force lockout (429 + Retry-After: 900 + "too many failed authentication
// attempts") must fail at once with a clear message: waiting and retrying only hammers a
// locked account and freezes the UI for minutes.
func TestFbLoginAccountLockoutFailsFast(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			http.NotFound(w, r) // the flavor probe: inconclusive, so fbLogin tries Quantum
			return
		}
		calls++
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"status":429,"message":"too many failed authentication attempts"}`)
	}))
	defer srv.Close()

	var esperas []time.Duration
	old := fbDormir
	fbDormir = func(d time.Duration) { esperas = append(esperas, d) }
	defer func() { fbDormir = old }()

	_, err := fbLogin(srv.URL, "admin", "x")
	if !errors.Is(err, errFBContaBloqueada) {
		t.Fatalf("fbLogin = %v; quere errFBContaBloqueada", err)
	}
	if !strings.Contains(err.Error(), "15 min") || !strings.Contains(err.Error(), `"admin"`) {
		t.Errorf("a mensaxe debe dicir a conta e os minutos: %v", err)
	}
	if calls != 1 || len(esperas) != 0 {
		t.Errorf("fixo %d peticións e %d esperas; quere 1 e 0 (non insistir nun bloqueo)", calls, len(esperas))
	}

	// The admin session does not go looking for another URL either (more attempts).
	a := NewApp()
	a.dataDir = t.TempDir()
	if err := a.gardarAdminConfigDirecto(AdminConfig{ServerURL: srv.URL, FBAdminUser: "admin", FBAdminPass: "x"}); err == nil {
		calls = 0
		if _, _, _, err := a.fbAdminSession(); !errors.Is(err, errFBContaBloqueada) || calls != 1 {
			t.Errorf("fbAdminSession = %v con %d peticións; quere o bloqueo e 1 petición", err, calls)
		}
	}
}
