package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// File Browser integration — works against BOTH the classic File Browser
// (https://github.com/filebrowser/filebrowser, checked against v2.63.18) and File Browser
// Quantum (https://github.com/gtsteffaniak/filebrowser, checked against v1.5.6). Which
// one is installed is detected by itself, per server URL, with nothing to configure: see
// fbsabor.go for how, and for the map of what differs.
//
// Backups are stored on a File Browser server (by default the local instance at
// DefaultServerURL). Authentication reuses the student's own account; fbLogin returns a
// JWT and fbAuth sends it on every request. Quantum is a rewrite of the classic File
// Browser, NOT a compatible fork: its paths live in query strings
// (?source=<fbSource>&path=/a/b) instead of the URL, directories need isDir=true, and a
// listing splits "files" from "folders". Each helper below notes what it does per flavor.
// Each student's data lives in their own folder inside fbUsersRoot (users/<login>),
// created on demand. That folder holds a single backup file (fbBackupName), which is
// overwritten on every push, and — provisioned separately — the student's
// certificate. No server-side configuration is required beyond a user account.

// The names below spell "dixitalizacion" because that is what the app used to be
// called. They are not labels: they address folders and files that already exist on
// the server (the shared programación, every student's backup) and on each machine.
// Renaming the app does not rename them — doing so would orphan all of it — so they
// stay as they are on purpose. The name the world sees lives in wails.json, main.go
// and the login screen.
const (
	// DefaultServerURL is the fallback the login screen points to when the install
	// left no tao-servidor.conf behind (see servidor.go, which is what the login
	// screen actually asks for). It can be overridden from the login form. Points
	// at localhost: File Browser typically runs on the same machine as the
	// classroom's Piztu install (or, for the standalone Tao_Yang_installer, on the
	// teacher's own machine) - a real LAN IP hardcoded here would be wrong for
	// everyone else. The /arquivos suffix is the subpath Apache proxies on port 80
	// and File Browser's own --baseurl (playbooks/instalar_filebrowser.yaml).
	DefaultServerURL = "http://127.0.0.1:8080/arquivos"

	// fbSource is the name of the Quantum "source" (server.sources[].name in its
	// config.yaml) that holds every path this app addresses. Quantum can serve several
	// sources and asks for one on each call; the install playbooks create exactly this one.
	// The classic File Browser has no sources, so it is ignored there.
	fbSource = "arquivos"

	// fbUsersRoot is the server folder that contains one subfolder per student,
	// named after their File Browser login (e.g. users/m_pardinas).
	fbUsersRoot = "users"
	// fbBackupName is the single backup file kept in each student's folder; it is
	// overwritten on every push so only the latest copy is retained.
	fbBackupName = "dixitalizacion-backup.json"
	// fbWorkRoot is the subfolder of each student's folder that holds the source files
	// of their work, filed per subject and session date (see traballos.go):
	// users/<login>/traballos/<materia>/<YYYY-MM-DD>/<ficheiro>.
	fbWorkRoot = "traballos"
	// fbCommentsRoot is the sibling subfolder of each student's folder holding the
	// clarifying files the professor attaches to their feedback on a session:
	// users/<login>/comentarios/<materia>/<YYYY-MM-DD>/<ficheiro>. The student may list
	// and download them; only the professor uploads or removes them.
	fbCommentsRoot = "comentarios"
	// fbTasksRoot is the subfolder of the SHARED folder that holds the task files the
	// professor attaches to a class session for the whole materia (a simple Moodle-like
	// assignment): dixitalizacion/tarefas/<materia>/<YYYY-MM-DD>/<ficheiro>. Every student
	// of the materia can list and download them; only the professor uploads or removes them.
	fbTasksRoot = "tarefas"
	// fbTaskDescRoot is the sibling subfolder holding the task's optional written
	// explanation, one file per session (dixitalizacion/tarefas-descricions/<materia>/
	// <YYYY-MM-DD>.md) — kept apart from fbTasksRoot so it never shows up mixed into
	// the listing of uploadable task files (see SaveTaskDescription/GetTaskDescription,
	// traballos.go). Same read/write rule as the task files: any student reads, only
	// the professor writes.
	fbTaskDescRoot = "tarefas-descricions"
	fbHTTPTimeMs   = 30000

	// fbInformesRoot is the subfolder of each student's folder that holds their signed
	// PDF reports, uploaded by ExportSignedReport as a server-side copy of the report
	// the student also saves locally: users/<login>/informes/<ficheiro>.
	fbInformesRoot = "informes"

	// fbIdentidadeFile is the small marker file kept alongside the certificate in each
	// student's own folder (users/<login>/identidade.json), holding just their internal
	// UUID. It is a third copy of the uuid<->login mapping (besides the local
	// profiles.json and the shared alumnos-uuid.json) so the link survives even if
	// both of those are lost, as long as the student's own folder exists.
	fbIdentidadeFile = "identidade.json"

	// fbPracticasRoot is the subfolder of the SHARED folder where a professor can
	// prepare a de-identified copy of a session's work, one subfolder per student named
	// only by their internal UUID (never by username, name or certificate):
	// dixitalizacion/practicas/<materia>/<YYYY-MM-DD>/<uuid>/<ficheiro>. It exists so a
	// professor can hand that folder to an AI tool for correction without exposing any
	// personal data. Built on demand by PrepareCorrectionFolder; students never access it.
	fbPracticasRoot = "practicas"

	// sharedRoot is the server folder (sibling of fbUsersRoot, physically
	// /var/www/html/arquivos/dixitalizacion) that holds the content authored by
	// professors and shared with students: the role manifest, the student roster
	// and the subject (materia) definitions. Every account can read it; professors
	// and the admin write to it.
	sharedRoot = "dixitalizacion"
	// sharedProfesores lists the usernames granted the "profesor" role.
	sharedProfesores = "profesores.json"
	// sharedAlumnos is the student roster (username -> name/surname/group). It is
	// what lets any student device resolve its own group at login (the group is
	// otherwise only known to the admin machine's local profiles.json).
	sharedAlumnos = "alumnos.json"
	// sharedAlumnosUUID is the username -> internal UUID mapping (see fbIdentidadeFile).
	// It lives beside alumnos.json, never inside fbPracticasRoot, so a professor can hand
	// that de-identified folder to an AI tool while keeping this mapping for grading.
	sharedAlumnosUUID = "alumnos-uuid.json"
	// sharedMateriasDir holds one JSON file per subject definition (materias/<id>.json).
	sharedMateriasDir = "materias"
	// fbSesionsDir is the shared subfolder where each machine publishes who is
	// currently using it (sesions/<hostname>.json), so the teacher's Piztu server —
	// which reads the SAME File Browser root directly off disk — can show the
	// student's name next to the machine on the classroom map.
	//
	// Relative to sharedRoot, NOT sharedRoot-prefixed: fbWriteSharedJSON below
	// already prepends "sharedRoot/" to whatever name it's given. Prefixing it
	// here too silently doubled it into sharedRoot/sharedRoot/sesions/... —
	// this was a real bug (fixed after finding the sessions actually landing
	// in dixitalizacion/dixitalizacion/sesions/ instead of dixitalizacion/sesions/).
	fbSesionsDir = "sesions"
)

// sesionTao is the small marker file written to fbSesionsDir on login/logout,
// identifying who is using this machine (or that nobody is, once anonymized).
type sesionTao struct {
	Equipo      string `json:"equipo"`
	Usuario     string `json:"usuario"`
	Nome        string `json:"nome"`
	Grupo       string `json:"grupo,omitempty"`
	Conectado   bool   `json:"conectado"`
	Actualizado string `json:"actualizado"`
}

// publicarSesion best-effort publishes (or clears) this machine's session marker so
// the professor's Piztu map can show who is using it. It never blocks nor surfaces an
// error to the caller (login/logout must succeed even if the server is unreachable);
// failures are only logged.
func (a *App) publicarSesion(conectado bool) {
	if a.fbToken == "" {
		return // no File Browser session to publish under
	}
	equipo, err := os.Hostname()
	if err != nil || equipo == "" {
		return
	}
	nome := a.fbUser
	grupo := ""
	if a.profile != nil {
		if n := strings.TrimSpace(a.profile.Name + " " + a.profile.Surname); n != "" {
			nome = n
		}
		grupo = a.profile.Group
	}
	sesion := sesionTao{
		Equipo:      equipo,
		Usuario:     a.fbUser,
		Nome:        nome,
		Grupo:       grupo,
		Conectado:   conectado,
		Actualizado: time.Now().Format(time.RFC3339),
	}
	if err := a.fbWriteSharedJSON(fbSesionsDir+"/"+equipo+".json", sesion); err != nil {
		log.Printf("[tao-sesion] non se puido publicar o estado de %s: %v", equipo, err)
	}
}

// fbUserDir returns the current student's folder on the server (users/<login>),
// where their backup is stored and their certificate is expected to live.
func (a *App) fbUserDir() string {
	return fbUsersRoot + "/" + a.fbUser
}

// fbTransport é o transporte compartido de todas as chamadas ao servidor de ficheiros.
//
// Por defecto Go reutiliza as conexións (keep-alive) e mantenas no pool ata 90 s. File
// Browser pecha as súas moito antes, así que a seguinte petición podía saír por unha
// conexión xa morta e fallar con «EOF» sen chegar sequera ao servidor — visto en
// produción: o alumnado entraba ben (login e lectura do roster reintentan) pero o
// listado das materias fallaba, e Tao dicía "Aínda non tes materias asignadas".
// Cun IdleConnTimeout curto é o cliente quen descarta esas conexións antes de usalas.
var fbTransport = &http.Transport{
	Proxy:               http.ProxyFromEnvironment,
	IdleConnTimeout:     5 * time.Second,
	MaxIdleConnsPerHost: 4,
}

func fbClient() *http.Client {
	return &http.Client{Timeout: fbHTTPTimeMs * time.Millisecond, Transport: fbTransport}
}

// ---- Transferencias de ficheiros (subir/descargar) --------------------------------
//
// fbClient ten un Timeout ABSOLUTO de 30 s que inclúe o envío do corpo. Vale para o
// JSON pequeno do día a día, pero non para un traballo do alumnado, que pode chegar
// aos 50 MiB de maxWorkFileBytes: nunha aula con wifi, ou co servidor ocupado, o
// envío pasa dos 30 s e o cliente corta a petición CANDO O FICHEIRO XA VIAXOU (o
// corte adoita caer mentres se agarda a resposta). De aí o síntoma que se vía: o
// ficheiro aparecía no servidor pero Tao daba a subida por fallada, encolaba o
// ficheiro enteiro e reintentábao unha e outra vez.
//
// Nunha transferencia non ten sentido un tope absoluto: os topes van nas fases que
// si poden quedar penduradas (conexión, TLS e espera da PRIMEIRA cabeceira da
// resposta) e o teito total vai por contexto, proporcional ao tamaño. É o mesmo
// criterio que xa segue piztu/internal/modulos coas descargas de módulos.
var fbTransportTransferencia = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	IdleConnTimeout:       5 * time.Second,
	MaxIdleConnsPerHost:   4,
	TLSHandshakeTimeout:   20 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
}

func fbClienteTransferencia() *http.Client {
	return &http.Client{Transport: fbTransportTransferencia}
}

// tempoTransferencia é o teito total para mover bytes: 2 minutos de base máis 1 s
// por cada 100 KiB, cun tope de 20 minutos. Un ficheiro de 50 MiB ten así ~10
// minutos, de abondo mesmo nunha liña lenta, e un servidor morto non chega a
// esperarse tanto porque o corta antes ResponseHeaderTimeout.
func tempoTransferencia(bytes int) time.Duration {
	d := 2*time.Minute + time.Duration(bytes/(100<<10))*time.Second
	if d > 20*time.Minute {
		d = 20 * time.Minute
	}
	return d
}

// ---- Shared folder (dixitalizacion/) helpers ------------------------------------
//
// These read/write JSON files in the shared folder using the app's current File
// Browser session (a.fbToken): the professor's when a professor acts, or the
// administrator's when reached through withAdminFB. They tolerate a missing file
// so a first run behaves as an empty shared folder.

// fbEnsureShared creates the shared folder (and its materias subfolder) if missing.
func (a *App) fbEnsureShared() error {
	if err := a.fbEnsureDir(sharedRoot); err != nil {
		return err
	}
	return a.fbEnsureDir(sharedRoot + "/" + sharedMateriasDir)
}

// fbReadSharedJSON downloads sharedRoot/name and unmarshals it into out. A missing
// file returns (false, nil) so callers can start from an empty value.
func (a *App) fbReadSharedJSON(name string, out any) (bool, error) {
	content, err := a.fbRawGet(sharedRoot + "/" + name)
	if err != nil {
		return false, nil // treat any read failure (typically 404) as "no file yet"
	}
	if len(content) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(content, out); err != nil {
		return false, fmt.Errorf("%s/%s non válido: %w", sharedRoot, name, err)
	}
	return true, nil
}

// fbWriteSharedJSON marshals v and uploads it to sharedRoot/name, creating the
// shared folder first.
func (a *App) fbWriteSharedJSON(name string, v any) error {
	if err := a.fbEnsureShared(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return a.fbUpload(sharedRoot+"/"+name, raw)
}

// fileNonEmpty reports whether path exists and has content (size > 0).
func fileNonEmpty(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// normalizeServerURL trims a trailing slash and ensures a scheme (defaulting to
// http, since the File Browser server is typically a plain-HTTP host on the LAN).
func normalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

// fbEscapePath percent-escapes each path segment while keeping the slashes.
func fbEscapePath(p string) string {
	segments := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// fbAuth authenticates req with the session JWT. It sends the token BOTH ways: the classic
// File Browser only reads X-Auth, Quantum only reads "Authorization: Bearer", and each one
// ignores the header it does not know (checked on both). So no request has to know which
// flavor it is talking to just to authenticate.
func fbAuth(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Auth", token)
}

// fbPasswordHeader encodes a password for Quantum's X-Password header, which the server
// URL-decodes on login and on every user change (a raw "ñ" or "%" would be rejected as
// a wrong password). Spaces become %20 rather than "+" so it decodes the same whether
// the server unescapes it as a query value or as a path segment.
func fbPasswordHeader(password string) string {
	return strings.ReplaceAll(url.QueryEscape(password), "+", "%20")
}

// fbResourceURL builds base + /api/resources?source=…&path=/a/b on Quantum, or
// base + /api/resources/a/b on the classic File Browser (see fbResourceURLClasico, which
// turns isDir=true into the trailing slash). remotePath is slash-normalised (leading "/"
// added, trailing one dropped). extra adds query parameters such as isDir or override.
func fbResourceURL(base, remotePath string, extra url.Values) string {
	if fbSaborDe(base) == fbClasico {
		return fbResourceURLClasico(base, remotePath, extra)
	}
	q := url.Values{}
	q.Set("source", fbSource)
	q.Set("path", "/"+strings.Trim(remotePath, "/"))
	for k, v := range extra {
		q[k] = v
	}
	return base + "/api/resources?" + q.Encode()
}

// fbDownloadURLQuantum builds base + /api/resources/download?source=…&file=/a/b,
// optionally as a zip (algo=zip) when the target is a folder. (The classic File Browser
// serves files from /api/raw instead: see fbDownloadURLs.)
func fbDownloadURLQuantum(base, remotePath string, zipped bool) string {
	q := url.Values{}
	q.Set("source", fbSource)
	q.Set("file", "/"+strings.Trim(remotePath, "/"))
	if zipped {
		q.Set("algo", "zip")
	}
	return base + "/api/resources/download?" + q.Encode()
}

// loginServer authenticates against the File Browser server, stores the session
// and builds the student Profile. Display name/surname are enriched from the local
// reference profiles when a matching username exists; otherwise the username is
// used, since File Browser only exposes the login name.
func (a *App) loginServer(username, password, serverURL string) (*Profile, error) {
	base := normalizeServerURL(serverURL)
	// The student only has to give the server's name (bac0.local): the port and the
	// /arquivos subpath are worked out, for either flavor (see resolverServidor).
	if real := resolverServidor(base); real != "" {
		base = real
	}

	token, err := fbLogin(base, username, password)
	if err != nil {
		return nil, err
	}

	a.fbURL = base
	a.fbToken = token
	a.fbUser = username
	a.fbPassword = password

	// Resolve the role from the shared manifest: a listed username is a professor,
	// everyone else is a student.
	role := "alumno"
	if a.fbIsProfessor(username) {
		role = "profesor"
	}

	// Identity: prefer the server roster (available on any device); fall back to the
	// admin machine's local profiles.json when the roster has no entry yet.
	name, surname, group := username, "", ""
	if e, ok := a.fbRosterEntry(username); ok {
		if e.Name != "" {
			name = e.Name
		}
		surname, group = e.Surname, e.Group
	} else if profiles, err := a.loadProfiles(); err == nil {
		for _, p := range profiles {
			if strings.EqualFold(p.Username, username) {
				name, surname, group = p.Name, p.Surname, p.Group
				break
			}
		}
	}

	// Use the certificate stored in the student's own server folder. If the server
	// is momentarily unreachable, fall back to a previously downloaded copy — but
	// NEVER to the shared demo certificate: signing must use the student's identity.
	certPath, hasCert, certSource := "", false, ""
	if p, err := a.fbFetchCert(username); err == nil && p != "" {
		certPath, hasCert, certSource = p, true, "servidor"
	} else if cached := filepath.Join(a.certsDir(), username+".p12"); fileNonEmpty(cached) {
		certPath, hasCert, certSource = cached, true, "local"
	}

	prof := &Profile{
		Username:   username,
		Name:       name,
		Surname:    surname,
		Group:      group,
		CertPath:   certPath,
		HasCert:    hasCert,
		CertSource: certSource,
		Connected:  true,
		Role:       role,
	}
	a.apiEstadoMu.Lock()
	a.profile = prof
	a.apiEstadoMu.Unlock()
	a.publicarSesion(true)
	return prof, nil
}

// CheckServer reports whether the File Browser server at serverURL is reachable. It
// is a lightweight probe used by the login screen to colour the URL field (green when
// the server answers, otherwise the field stays highlighted). Any HTTP response —
// even an error status — counts as reachable; only a transport failure or timeout
// counts as down. A short timeout keeps the login screen responsive while typing.
func (a *App) CheckServer(serverURL string) bool {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if base == "" {
		return false
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(base + "/health")
	if err != nil {
		// Fall back to the base URL: some deployments don't expose /health.
		resp, err = client.Get(base)
		if err != nil {
			return false
		}
	}
	resp.Body.Close()
	return true
}

// errFBBadCredentials is returned by fbLogin when the server actively rejects the
// username/password (HTTP 403/401), as opposed to a connectivity or server-side error.
// SaveAdminConfig checks for it (via errors.Is) to refuse storing administrator
// credentials that do not authenticate — catching the common setup slip of swapping
// the File Browser admin user and password.
var errFBBadCredentials = errors.New("usuario ou contrasinal incorrectos no servidor de ficheiros")

// errFBNonAtopado distingue "ese cartafol/ficheiro non existe" (404) de calquera outro
// fallo ao listar. Sen esta distinción, un 403 por permisos parecía un cartafol baleiro
// e o alumnado vía "Aínda non tes materias asignadas" no canto do erro real.
var errFBNonAtopado = errors.New("non existe no servidor de ficheiros")

// errFBContaBloqueada is Quantum's brute-force lockout: after a handful of FAILED logins
// for a username it answers 429 "too many failed authentication attempts" with
// "Retry-After: 900" (15 min) — to that username's CORRECT logins too, so a stale admin
// password in one place locks the account for every other client (the historial daemon
// included). Unlike the burst limit below it is not worth waiting out: retrying only
// keeps hammering a locked account and freezes the UI for minutes.
var errFBContaBloqueada = errors.New("conta bloqueada polo servidor de ficheiros")

// fbLoginIntentos is how many times fbLogin tries when Quantum answers 429 (too many
// requests). Quantum limits logins to a burst of ~8 per client IP and then answers 429
// with "Retry-After: 6", and it ignores X-Forwarded-For — so behind the Apache proxy the
// whole classroom counts as ONE client and the start of a class (30 students logging in
// at once) hits the limit for a few seconds. Waiting as told and retrying makes that
// invisible to the student instead of a "o servidor devolveu un erro (429)".
const fbLoginIntentos = 6

// fbLoginEsperaMax is the longest Retry-After (seconds) treated as the short burst limit
// worth waiting for; anything above it is the account lockout (errFBContaBloqueada).
const fbLoginEsperaMax = 30

// fbDormir is time.Sleep, swappable so tests do not really wait.
var fbDormir = time.Sleep

// fbLogin exchanges credentials for a JWT and works out which File Browser flavor it is
// talking to (see fbsabor.go), so the caller never has to say. Every login probes afresh:
// it is the start of a session, the cheapest moment to notice that a classroom's server
// was migrated from the classic File Browser to Quantum (or back).
//
// The classic one takes the credentials as JSON on POST /api/login and answers 403 when
// they are wrong. Quantum takes the username in the query and the (URL-encoded) password
// in X-Password on POST /api/auth/login, and answers 401. Both return the token as the
// plain-text response body. A Quantum 429 is waited out (see fbLoginIntentos).
//
// If the probe is inconclusive, login is tried as Quantum and, only when that route does
// not exist there (404/405), as the classic one: the flavor that answers is remembered.
func fbLogin(base, username, password string) (string, error) {
	sabor := fbDetectarSabor(base)
	if sabor == fbSaborIndeterminado {
		token, err := fbLoginSabor(base, fbQuantum, username, password)
		if !errors.Is(err, errFBRotaAusente) {
			if err == nil {
				fbRecordarSabor(base, fbQuantum)
			}
			return token, err
		}
		sabor = fbClasico
	}
	token, err := fbLoginSabor(base, sabor, username, password)
	if err == nil {
		fbRecordarSabor(base, sabor)
	}
	return token, err
}

// errFBRotaAusente marks a login answered 404/405: that server has no such login route,
// so it is the other flavor (see fbLogin).
var errFBRotaAusente = errors.New("a ruta de login non existe neste servidor")

// fbLoginSabor logs in as the given flavor, waiting out Quantum's 429 burst limit.
func fbLoginSabor(base string, sabor fbSabor, username, password string) (string, error) {
	for intento := 1; ; intento++ {
		token, espera, err := fbLoginUnha(base, sabor, username, password)
		if espera == 0 || intento >= fbLoginIntentos {
			return token, err
		}
		log.Printf("[login] o servidor limita os accesos (429); reintento %d/%d en %v", intento, fbLoginIntentos-1, espera)
		fbDormir(espera)
	}
}

// fbLoginUnha is one login attempt. A non-zero wait means Quantum asked to slow down.
func fbLoginUnha(base string, sabor fbSabor, username, password string) (token string, espera time.Duration, err error) {
	var req *http.Request
	if sabor == fbClasico {
		body, _ := json.Marshal(map[string]string{
			"username":  username,
			"password":  password,
			"recaptcha": "",
		})
		req, err = http.NewRequest(http.MethodPost, base+"/api/login", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequest(http.MethodPost, base+"/api/auth/login?username="+url.QueryEscape(username), nil)
		if err == nil {
			req.Header.Set("X-Password", fbPasswordHeader(password))
		}
	}
	if err != nil {
		return "", 0, err
	}
	resp, err := fbClient().Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		return "", 0, errFBBadCredentials
	case http.StatusTooManyRequests:
		n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if n > fbLoginEsperaMax || strings.Contains(string(data), "failed authentication") {
			mins := max(1, (n+59)/60)
			return "", 0, fmt.Errorf("%w: %q non pode entrar durante uns %d min por demasiados intentos fallidos de contrasinal; comproba o contrasinal e agarda (ou reinicia o servizo filebrowser no servidor)",
				errFBContaBloqueada, username, mins)
		}
		espera = 6 * time.Second
		if n > 0 {
			espera = time.Duration(n) * time.Second
		}
		return "", espera, fmt.Errorf("o servidor de ficheiros limitou os accesos (429); volve tentalo en uns segundos")
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return "", 0, fmt.Errorf("%w (%d): %w", errFBRotaAusente, resp.StatusCode, fbErroServidor(resp.StatusCode, data))
	default:
		return "", 0, fbErroServidor(resp.StatusCode, data)
	}

	token = strings.TrimSpace(string(data))
	if token == "" {
		return "", 0, fmt.Errorf("o servidor de ficheiros non devolveu un token de acceso")
	}
	return token, 0, nil
}

// fbErroServidor is the message for an unexpected login status.
func fbErroServidor(status int, data []byte) error {
	return fmt.Errorf("o servidor de ficheiros devolveu un erro (%d): %s", status, strings.TrimSpace(string(data)))
}

// ChangePassword changes the logged-in student's password on the File Browser
// server. The current password is not asked again: it was already provided at login
// and kept in memory (a.fbPassword), which File Browser demands to authorise a
// self-service password change. It sends PUT /api/users/<id> and then confirms the
// change by re-authenticating with the new password, refreshing the session.
func (a *App) ChangePassword(newPassword string) error {
	if a.profile == nil || !a.profile.Connected {
		return fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	newPassword = strings.TrimSpace(newPassword)
	if newPassword == "" {
		return fmt.Errorf("introduce o novo contrasinal")
	}
	if len(newPassword) < 4 {
		return fmt.Errorf("o novo contrasinal debe ter polo menos 4 caracteres")
	}
	if newPassword == a.fbPassword {
		return fmt.Errorf("o novo contrasinal debe ser distinto do actual")
	}

	userID, err := fbUserID(a.fbToken)
	if err != nil {
		return err
	}
	log.Printf("[pwd] cambiando o contrasinal do usuario %q (id=%d) en %s", a.fbUser, userID, a.fbURL)

	if err := a.fbUpdatePassword(a.fbToken, userID, a.fbPassword, newPassword); err != nil {
		return err
	}

	// Confirm the change really took effect: authenticate with the new password.
	// File Browser can answer 200 to the update yet not apply it (e.g. the account
	// has the password locked), so this check turns a silent failure into a clear
	// message and, on success, refreshes the stored session.
	fresh, err := fbLogin(a.fbURL, a.fbUser, newPassword)
	if err != nil {
		log.Printf("[pwd] o servidor aceptou a petición pero o novo contrasinal non funciona: %v", err)
		return fmt.Errorf("o servidor aceptou a petición pero o novo contrasinal non funciona; comproba que a conta non teña o contrasinal bloqueado no servidor")
	}
	a.fbToken = fresh
	a.fbPassword = newPassword
	log.Printf("[pwd] contrasinal cambiado e verificado correctamente para %q", a.fbUser)
	return nil
}

// fbUserID extracts the numeric File Browser user id from a JWT. Both flavors issue a
// standard JWT but keep the account id in different claims — Quantum in the top-level
// "belongsTo", the classic File Browser nested as user.id — so both are read (they never
// coexist). It is what the users API needs to address the account.
func fbUserID(token string) (int, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("o token de sesión do servidor non é válido")
	}
	payload, err := b64urlDecode(parts[1])
	if err != nil {
		return 0, fmt.Errorf("non se puido ler o token de sesión do servidor: %w", err)
	}
	var claims struct {
		BelongsTo int `json:"belongsTo"`
		User      struct {
			ID int `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, fmt.Errorf("non se puido interpretar o token de sesión do servidor: %w", err)
	}
	id := claims.BelongsTo
	if id == 0 {
		id = claims.User.ID
	}
	if id == 0 {
		log.Printf("[pwd] o token de sesión non contén belongsTo nin user.id; payload=%s", string(payload))
		return 0, fmt.Errorf("non se puido determinar o identificador de usuario no servidor")
	}
	return id, nil
}

// b64urlDecode decodes a JWT segment, tolerating the missing padding that JWTs omit
// as well as tokens that (non-standardly) include it.
func b64urlDecode(seg string) ([]byte, error) {
	if m := len(seg) % 4; m != 0 {
		seg += strings.Repeat("=", 4-m)
	}
	return base64.URLEncoding.DecodeString(seg)
}

// fbUpdatePassword changes the given user's password through Quantum's
// PUT /api/users?id=<id> (the classic File Browser's version is in admin_fb_clasico.go). "which":["password"] restricts the update to that one field —
// the sole change a non-admin account may perform, and all it needs to send. The
// actor's CURRENT password travels in X-Password to authorise the change (advisory
// GHSA-hxw8-4h9j-hq2r); on success Quantum answers 204 with no body.
func (a *App) fbUpdatePassword(token string, userID int, currentPassword, newPassword string) error {
	if fbSaborDe(a.fbURL) == fbClasico {
		return a.fbUpdatePasswordClasico(token, userID, currentPassword, newPassword)
	}
	body, _ := json.Marshal(map[string]any{
		"which": []string{"password"},
		"data":  map[string]string{"password": newPassword},
	})
	endpoint := fmt.Sprintf("%s/api/users?id=%d", a.fbURL, userID)
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	fbAuth(req, token)
	req.Header.Set("X-Password", fbPasswordHeader(currentPassword))
	req.Header.Set("Content-Type", "application/json")
	resp, err := fbClient().Do(req)
	if err != nil {
		return fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		log.Printf("[pwd] o servidor rexeitou o cambio (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
		return fmt.Errorf("o servidor de ficheiros rexeitou o cambio de contrasinal (%d): %s",
			resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

// fbGetUser fetches a user object (as a generic map so every field is preserved)
// from File Browser. A non-admin account is allowed to read its own record. Quantum needs
// ?id= to answer a single object (without it, the whole list); the classic one takes the
// id in the path (see fbUserURL).
func (a *App) fbGetUser(token string, userID int) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, fbUserURL(a.fbURL, userID), nil)
	if err != nil {
		return nil, err
	}
	fbAuth(req, token)
	resp, err := fbClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("non se puido obter os datos do usuario do servidor: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[pwd] non se puido obter o usuario id=%d (%d): %s", userID, resp.StatusCode, strings.TrimSpace(string(data)))
		return nil, fmt.Errorf("o servidor non devolveu os datos do usuario (%d)", resp.StatusCode)
	}
	var user map[string]any
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("resposta non válida do servidor ao obter o usuario")
	}
	// Ensure the id travels in the body too; File Browser requires data.id != 0.
	user["id"] = userID
	return user, nil
}

// RegenerateCertificate issues a fresh personal certificate for the logged-in
// student (signed by the department CA embedded in the app), protected with their
// current password, and uploads it to their folder on the File Browser server,
// replacing any previous certificate. The updated Profile is returned so the UI can
// reflect that a certificate is now available. It is called after a password change,
// but works on its own too (e.g. to provision a first certificate).
func (a *App) RegenerateCertificate(name, surname string) (*Profile, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	name = strings.TrimSpace(name)
	surname = strings.TrimSpace(surname)
	if name == "" {
		return nil, fmt.Errorf("introduce o nome e apelidos para o certificado")
	}

	// Remember the real identity (File Browser only knows the login) so it survives
	// across sessions and drives the certificate subject and the visible signature.
	if err := a.saveIdentity(a.fbUser, name, surname); err != nil {
		log.Printf("[cert] non se puido gardar o nome do alumno localmente: %v", err)
	}
	a.profile.Name = name
	a.profile.Surname = surname

	pfx, err := generateStudentP12(a.dataDir, name, surname, a.fbPassword)
	if err != nil {
		return nil, err
	}

	dir := a.fbUserDir()
	if err := a.fbEnsureDir(dir); err != nil {
		return nil, fmt.Errorf("non se puido preparar o cartafol no servidor: %w", err)
	}
	// Remove any certificate already in the folder so only the new one remains and
	// the login-time fetch can never pick up a stale .p12.
	a.fbDeleteExistingCerts(dir)

	remote := dir + "/" + certFileBase(name, surname) + ".p12"
	if err := a.fbUpload(remote, pfx); err != nil {
		return nil, err
	}

	// Save a local copy and point the session at it so signing works immediately.
	if err := os.MkdirAll(a.certsDir(), 0o700); err != nil {
		return nil, err
	}
	local := filepath.Join(a.certsDir(), a.fbUser+".p12")
	if err := os.WriteFile(local, pfx, 0o600); err != nil {
		return nil, err
	}
	a.profile.CertPath = local
	a.profile.HasCert = true
	a.profile.CertSource = "servidor"
	log.Printf("[cert] certificado rexenerado para %q e subido a %s", a.fbUser, remote)
	return a.profile, nil
}

// fbDeleteExistingCerts removes every .p12/.pfx file in dir. Best-effort: individual
// failures are logged but do not abort the caller.
func (a *App) fbDeleteExistingCerts(dir string) {
	items, err := a.fbList(dir)
	if err != nil {
		return // folder missing or unreadable: nothing to clean up
	}
	for _, it := range items {
		if it.IsDir {
			continue
		}
		lower := strings.ToLower(it.Name)
		if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
			if err := a.fbDelete(dir + "/" + it.Name); err != nil {
				log.Printf("[cert] non se puido borrar o certificado vello %q: %v", it.Name, err)
			}
		}
	}
}

// fbDeleteDirect removes a file on the server through File Browser's
// DELETE /api/resources. It is the raw, single-attempt HTTP call; fbDelete (below)
// wraps it with the offline queue.
func (a *App) fbDeleteDirect(remotePath string) error {
	req, err := http.NewRequest(http.MethodDelete, fbResourceURL(a.fbURL, remotePath, nil), nil)
	if err != nil {
		return err
	}
	fbAuth(req, a.fbToken)
	resp, err := fbClient().Do(req)
	if err != nil {
		return fbTransient(fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err))
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// File Browser answers 200 OK or 204 No Content on a successful delete.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		err := fmt.Errorf("erro %d ao borrar", resp.StatusCode)
		if resp.StatusCode >= 500 {
			return fbTransient(err)
		}
		return err
	}
	return nil
}

// PushBackup saves the given backup JSON to the student's folder on the File
// Browser server, overwriting the previous copy. Requires an active session.
func (a *App) PushBackup(jsonStr string) (string, error) {
	if a.profile == nil || !a.profile.Connected {
		return "", fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	dir := a.fbUserDir()
	if err := a.fbEnsureDir(dir); err != nil {
		return "", fmt.Errorf("non se puido preparar a carpeta no servidor de ficheiros: %w", err)
	}
	if err := a.fbUpload(dir+"/"+fbBackupName, []byte(jsonStr)); err != nil {
		return "", err
	}
	return fbBackupName, nil
}

// PullBackup returns the content of the student's backup, or "" when there is none
// (including when the folder does not exist yet). Requires an active session.
func (a *App) PullBackup() (string, error) {
	if a.profile == nil || !a.profile.Connected {
		return "", nil
	}
	content, err := a.fbDownload(a.fbUserDir() + "/" + fbBackupName)
	if err != nil {
		// A missing file simply means there is no backup yet.
		return "", nil
	}
	return content, nil
}

// fbEnsureDirDirect creates dir on the server, with any missing parents. It is
// idempotent: Quantum answers 409 when the directory already exists (the classic File
// Browser just answers 200), which counts as success here. On Quantum isDir=true is
// mandatory — a bare trailing slash (how the classic one makes a folder, and what
// fbResourceURL sends it) would make Quantum create an EMPTY FILE instead. It is
// the raw, single-attempt HTTP call; fbEnsureDir (below) wraps it with the offline queue.
func (a *App) fbEnsureDirDirect(dir string) error {
	req, err := http.NewRequest(http.MethodPost, fbResourceURL(a.fbURL, dir, url.Values{"isDir": {"true"}}), nil)
	if err != nil {
		return err
	}
	fbAuth(req, a.fbToken)
	resp, err := fbClient().Do(req)
	if err != nil {
		return fbTransient(fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err))
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		err := fmt.Errorf("erro %d ao crear a carpeta", resp.StatusCode)
		if resp.StatusCode >= 500 {
			return fbTransient(err)
		}
		return err
	}
	return nil
}

// fbUploadDirect writes content to remotePath, overwriting any existing file. It is
// the raw, single-attempt HTTP call; fbUpload (below) wraps it with the offline queue.
func (a *App) fbUploadDirect(remotePath string, content []byte) error {
	// override=true: without it Quantum answers 409 on an existing file. Missing parent
	// folders are created by the server itself.
	endpoint := fbResourceURL(a.fbURL, remotePath, url.Values{"override": {"true"}})
	ctx, cancel := context.WithTimeout(context.Background(), tempoTransferencia(len(content)))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(content))
	if err != nil {
		return err
	}
	fbAuth(req, a.fbToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(content))
	resp, err := fbClienteTransferencia().Do(req)
	if err != nil {
		return fbTransient(fmt.Errorf("non se puido gardar no servidor de ficheiros: %w", err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("o servidor de ficheiros rexeitou o gardado (%d): %s",
			resp.StatusCode, strings.TrimSpace(string(data)))
		if resp.StatusCode >= 500 {
			return fbTransient(err)
		}
		return err
	}
	return nil
}

// fbItem is one entry of a directory listing. Size is only the file's real size when the
// server runs with sources[].config.useLogicalSize: true (the install playbooks set it);
// otherwise Quantum reports disk usage rounded up to 4 KB blocks, which would break
// fbXaSubido's exact comparison.
type fbItem struct {
	Name  string
	IsDir bool
	Size  int64
}

// fbEntry is one element of Quantum's "files"/"folders" arrays.
type fbEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// fbList returns the entries of a directory on the server. Quantum answers the
// directory as one object with its files and sub-folders in separate arrays, and has
// no per-entry "isDir": which array an entry came from is what says it. The classic
// File Browser answers a single "items" array whose entries carry isDir. The two shapes
// never coexist in one answer, so both are read without asking which flavor it is.
func (a *App) fbList(dir string) ([]fbItem, error) {
	endpoint := fbResourceURL(a.fbURL, dir, nil)
	// Mesmos reintentos que fbRawGet: un fallo de transporte (a conexión pechada polo
	// outro lado, un corte momentáneo do wifi da aula) non pode acabar en "non hai
	// materias" — ver a nota de fbTransport.
	const maxAttempts = 3
	var resp *http.Response
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(300 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		fbAuth(req, a.fbToken)
		r, err := fbClient().Do(req)
		if err != nil {
			if attempt == maxAttempts {
				return nil, err
			}
			continue
		}
		resp = r
		break
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errFBNonAtopado
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("erro %d ao listar %s no servidor de ficheiros", resp.StatusCode, dir)
	}
	var listing struct {
		Files   []fbEntry `json:"files"`
		Folders []fbEntry `json:"folders"`
		Items   []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"isDir"`
			Size  int64  `json:"size"`
		} `json:"items"` // classic File Browser
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, fmt.Errorf("resposta non válida do servidor de ficheiros")
	}
	items := make([]fbItem, 0, len(listing.Folders)+len(listing.Files))
	for _, f := range listing.Folders {
		items = append(items, fbItem{Name: f.Name, IsDir: true})
	}
	for _, f := range listing.Files {
		items = append(items, fbItem{Name: f.Name, Size: f.Size})
	}
	for _, f := range listing.Items {
		items = append(items, fbItem{Name: f.Name, IsDir: f.IsDir, Size: f.Size})
	}
	return items, nil
}

// fbFetchCert downloads the student's PKCS#12 certificate (the first .p12/.pfx file
// found in their server folder) into the local certs directory and returns its
// path. It returns ("", nil) when the folder is missing or holds no certificate, so
// the caller can fall back to a locally provisioned certificate.
func (a *App) fbFetchCert(username string) (string, error) {
	dir := a.fbUserDir()
	items, err := a.fbList(dir)
	if err != nil {
		return "", nil // folder missing or unreadable → no server certificate
	}
	var certName string
	for _, it := range items {
		if it.IsDir {
			continue
		}
		lower := strings.ToLower(it.Name)
		if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
			certName = it.Name
			break
		}
	}
	if certName == "" {
		return "", nil // no certificate in the folder
	}

	content, err := a.fbRawGet(dir + "/" + certName)
	if err != nil {
		// The direct file download can fail (e.g. a server-side 500) for some
		// files. Fall back to downloading the whole folder as a zip — the URL then
		// only carries the folder name — and extract the certificate from it.
		content, err = a.fbFetchCertFromZip(dir)
	}
	if err != nil {
		log.Printf("[cert] non se puido obter o certificado de %q: %v", dir, err)
		return "", err
	}
	if len(content) == 0 {
		// The server delivered an empty file (e.g. the file is not readable by the
		// File Browser process). Do not save a 0-byte certificate.
		log.Printf("[cert] o certificado de %q chegou baleiro (0 bytes); revisa permisos/propiedade do ficheiro no servidor", dir)
		return "", fmt.Errorf("o servidor non puido entregar o certificado (0 bytes); revisa os permisos do ficheiro")
	}
	if err := os.MkdirAll(a.certsDir(), 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(a.certsDir(), username+".p12")
	if err := os.WriteFile(dest, content, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

// fbFetchCertFromZip downloads the given folder as a zip archive (File Browser's
// ?algo=zip) and returns the content of the first .p12/.pfx entry inside it. This
// avoids requesting the certificate by its own (possibly accented) file name, which
// some File Browser versions fail to serve directly.
func (a *App) fbFetchCertFromZip(dir string) ([]byte, error) {
	var zipBytes []byte
	var lastErr error
	// The classic File Browser sometimes only accepts the folder with a trailing slash,
	// so fbDownloadURLs may offer more than one URL; Quantum has exactly one.
	for _, endpoint := range fbDownloadURLs(a.fbURL, dir, true) {
		data, err := a.fbGetZip(endpoint)
		if err == nil {
			zipBytes, lastErr = data, nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("o cartafol descargado non é un zip válido: %w", err)
	}
	for _, f := range zr.File {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("non se atopou ningún .p12/.pfx no zip do cartafol")
}

// fbGetZip downloads one already-built zip URL.
func (a *App) fbGetZip(endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tempoTransferencia(maxWorkFileBytes))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	fbAuth(req, a.fbToken)
	resp, err := fbClienteTransferencia().Do(req)
	if err != nil {
		return nil, err
	}
	zipBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non se puido descargar o cartafol en zip (%d)", resp.StatusCode)
	}
	return zipBytes, nil
}

// fbDownload fetches the raw content of a file on the server.
func (a *App) fbDownload(remotePath string) (string, error) {
	data, err := a.fbRawGet(remotePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// fbRawGet downloads a file through /api/resources/download (Quantum) or /api/raw (the
// classic File Browser; see fbDownloadURLs). It retries a couple of
// times on server errors (5xx) or network failures, which File Browser can return
// transiently; 4xx responses are returned immediately since retrying is pointless.
// Quantum answers a MISSING file with a 500 (not 404) whose body says "no such file or
// directory": that one is definitive too, so it is not retried (PullBackup and friends
// ask for files that routinely do not exist yet).
func (a *App) fbRawGet(remotePath string) ([]byte, error) {
	endpoint := fbDownloadURLs(a.fbURL, remotePath, false)[0]
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(300 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), tempoTransferencia(maxWorkFileBytes))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		fbAuth(req, a.fbToken)
		resp, err := fbClienteTransferencia().Do(req)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("non se puido descargar do servidor de ficheiros: %w", err)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if resp.StatusCode == http.StatusOK {
			return data, nil
		}
		lastErr = fmt.Errorf("erro %d ao descargar", resp.StatusCode)
		if resp.StatusCode < 500 || bytes.Contains(data, []byte("no such file or directory")) {
			break // client error (401/404/…) or missing file: retrying will not help
		}
	}
	return nil, lastErr
}
