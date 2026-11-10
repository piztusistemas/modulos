package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// emitirEvento envía un evento ao frontend (substitúe wailsruntime.EventsEmit
// de v2 — v3 non precisa contexto). A garda application.Get() != nil cobre os
// mesmos casos cós "if a.ctx != nil"/"if a.ctx == nil" de antes (chamado dende
// un test ou antes de que ServiceStartup remate). Variádico porque
// "modulos_tao_cambiado" non leva datos (chamado sen terceiro argumento en
// v2 tamén) — Event.Emit acepta "data ...any", así que un emitirEvento(nome)
// sen máis reenvía cero argumentos igual ca antes.
func emitirEvento(nome string, dato ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(nome, dato...)
	}
}

// init rexistra o TIPO de cada evento (obrigatorio en Wails v3,
// application.RegisterEvent[T] — Emit sen isto rexistrado falla). Un tipo
// por nome de evento, o mesmo que xa se lle pasaba a wailsruntime.EventsEmit
// en v2. Chamadas individuais (non un bucle sobre unha lista de nomes):
// "wails3 generate bindings" analiza o código de forma estática para
// descubrir os eventos — un nome que non sexa un literal constante non se
// detecta (confirmado xa na migración de Piztu).
func init() {
	application.RegisterEvent[application.Void]("modulos_tao_cambiado")
	application.RegisterEvent[EstadoActualizacionTao]("actualizacion_tao_cambiada")
	application.RegisterEvent[FBQueueStatus]("fbcola")
	application.RegisterEvent[maltaInformeEvent]("tao:malta-informe")
	application.RegisterEvent[maltaRedaccionEvent]("tao:malta-redaccion")
	application.RegisterEvent[tarefaActualizadaEvent]("tao:tarefa-actualizada")
	application.RegisterEvent[tarefaAlumnoActualizadaEvent]("tao:tarefa-alumno-actualizada")
}

// App struct holds the current session state.
type App struct {
	dataDir string   // per-user data directory (profiles, certificates)
	profile *Profile // the currently logged-in student, nil when signed out

	// baseDir is where Piztu is installed, learned from PIZTU_CONTEXTO if Tao
	// was launched by it (contexto_piztu.go). "" when Tao ran without that
	// context (development, by hand) — AbrirYangParaTarefa (yang_launch.go)
	// then simply can't locate Yang, same degrade-quietly criterion as the
	// rest of this feature.
	baseDir string

	// taoModulosDir is <cartafol-de-Tao>/modulos, where external módulos DE
	// Tao live (modulos_tao.go) — distinct from baseDir/modulos, which is
	// Piztu's own module tree. Computed in startup() from os.Executable(),
	// so it works whether Tao runs as part of a Piztu install or standalone
	// (Tao_Yang_installer). "" only if os.Executable() itself fails.
	taoModulosDir string

	// idiomaPiztu is the language code (gl/es/en/pt) the professorado has set
	// for Piztu itself, learned from the same PIZTU_CONTEXTO as baseDir — "" if
	// Tao ran without that context. Only a DEFAULT for Tao's own language (see
	// IdiomaPiztu, frontend/src/i18n/index.ts): an explicit choice already
	// saved in Tao (localStorage) always wins over this.
	idiomaPiztu string

	// File Browser session, set when logging in against a File Browser server.
	fbURL      string // base URL of the File Browser server, "" when in local mode
	fbToken    string // JWT obtained at login, sent as X-Auth and "Authorization: Bearer" (fbAuth)
	fbUser     string // File Browser username, used to scope the backups folder
	fbPassword string // password used at login, kept in memory to re-use as the
	// current password when changing it (File Browser requires it)

	// fbAdminPass holds the File Browser administrator's own password while an admin
	// operation runs (set by withAdminFB, cleared afterwards). Both flavors demand it to
	// confirm sensitive changes: Quantum URL-encoded in the X-Password header on every
	// change to an account or access rule (401 without it, see fbAdminCall); the classic
	// File Browser as "current_password" in the JSON body (400 without it).
	fbAdminPass string

	isAdmin bool // true after a successful AdminLogin (Director); gates admin ops

	// Local HTTP API for other Piztu modules on the same machine (professor/admin
	// only, see api.go, api_auth.go, api_handlers.go and docs/api-tao.md).
	api *apiServidor
	// apiEstadoMu guards profile/isAdmin against the local API's HTTP handlers,
	// which run on goroutines distinct from the Wails bindings goroutine that
	// normally assigns these fields (Login/Logout/AdminLogin) — see api_auth.go.
	apiEstadoMu sync.RWMutex

	// Offline queue for File Browser writes (see fbqueue.go): operations that fail
	// transiently (server unreachable, slow VPS) are persisted here and retried in
	// the background instead of being lost.
	fbQueue     []fbOp
	fbQueueMu   sync.Mutex
	fbQueueWake chan struct{}

	// restoreFiles holds the contents of a backup .zip loaded for restoring (path ->
	// bytes), and restoreManifest its manifest. Set by LoadBackupArchive, consumed by
	// RestoreCourse, so the file dialog only opens once.
	restoreFiles    map[string][]byte
	restoreManifest backupManifest

	// actualizacionTao is the last known snapshot of whether a newer Tao is
	// published (see actualizacion.go) — checked once in the background at
	// startup, refreshed by comprobarActualizacionTao. Guarded because the
	// background goroutine writes it while GetActualizacionTao reads it from
	// the Wails bindings goroutine.
	actualizacionMu  sync.Mutex
	actualizacionTao EstadoActualizacionTao
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ServiceStartup is called when the app starts (v3 equivalent of v2's
// OnStartup) — the local data directory is prepared/seeded here.
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if pctx, ok := lerContextoPiztu(); ok {
		a.baseDir = pctx.BaseDir
		a.idiomaPiztu = pctx.Idioma
	}

	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	// "dixitalizacion" is the app's former name, kept because this folder already holds
	// the certificates on every machine the app runs on; see the note in filebrowser.go.
	a.dataDir = filepath.Join(base, "dixitalizacion")
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		fmt.Println("aviso: non se puido crear o directorio de datos:", err)
	}

	go a.comprobarActualizacionTao()

	// Cartafol de módulos externos DE Tao (modulos_tao.go) — calculado antes
	// de arrincar a API local, porque orixeRecoñecidaAPI (api_peercred_linux.go)
	// xa o consulta na primeira conexión aceptada.
	if dir, err := taoModulosDir(); err == nil {
		a.taoModulosDir = dir
		if err := PrepararModulosTao(dir); err != nil {
			fmt.Println("aviso: non se puido preparar o cartafol de módulos de Tao:", err)
		}
	} else {
		fmt.Println("aviso: non se puido determinar o cartafol de módulos de Tao:", err)
	}

	// Retoma calquera escritura ao servidor de ficheiros que quedase pendente dunha
	// sesión anterior (a app pechouse coa VPS caída, por exemplo) e arranca o worker
	// que reintenta en segundo plano (ver fbqueue.go).
	a.fbQueueInit()

	// Local HTTP API (professor/admin only, see api.go): starts listening
	// unconditionally so GET /api/v1/whoami always answers "ningunha" until a
	// professor/admin session begins — every other route stays gated live.
	a.api = novoAPIServidor(a)
	if err := a.api.escoitar(); err != nil {
		fmt.Println("aviso: non se puido arrincar a API local de Tao:", err)
	} else if err := a.escribirDescubrimentoAPI(a.api.rutaSocket(), a.api.token); err != nil {
		fmt.Println("aviso: non se puido escribir o ficheiro de descubrimento da API:", err)
	}

	// Heartbeat: refresca dixitalizacion/sesions/<hostname>.json cada 4 minutos mentres
	// haxa unha sesión aberta, para que o profesor non vexa "caducar" (ver core/tao.py
	// no lado do servidor) unha sesión que segue perfectamente activa. Sen isto,
	// publicarSesion só se chamaría en login/logout/shutdown e o timestamp quedaría
	// cravado ao momento do login.
	go func() {
		t := time.NewTicker(4 * time.Minute)
		defer t.Stop()
		for range t.C {
			if a.profile != nil {
				a.publicarSesion(true)
			}
		}
	}()

	return nil
}

// ServiceShutdown is called when the app window closes (v3 equivalent of
// v2's OnShutdown). Best-effort: marks this machine's session as
// disconnected even when the student closes the app without pressing
// Logout, so the professor's map doesn't keep showing a stale name.
//
// publicarSesion makes a network call to the File Browser server (up to fbHTTPTimeMs,
// 30s) — launched here in the background so a slow/unreachable server never delays the
// window closing. If it doesn't finish before the process exits, the session marker is
// simply picked up as stale by the map's own caducidade timeout (tao.caducidade_seg).
func (a *App) ServiceShutdown() error {
	go a.publicarSesion(false)
	if a.api != nil {
		a.api.pechar()
	}
	a.borrarDescubrimentoAPI()
	return nil
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// IdiomaPiztu devolve o código de idioma (gl/es/en/pt) que o profesorado ten
// escollido en ⚙ Aula do propio Piztu, ou "" se Tao arrincou sen ese contexto
// (desenvolvemento, executable á man). Chamado unha soa vez ao arrincar
// (frontend/src/i18n/index.ts) para usalo coma valor por defecto do idioma de
// Tao SE aínda non hai unha elección explícita gardada — nunca substitúe unha
// xa feita.
func (a *App) IdiomaPiztu() string {
	return a.idiomaPiztu
}

// Hostname devolve o nome deste equipo, para que quede rexistrado na práctica do día
// quen a editou desde onde (útil para localizar o responsable en caso de desperfectos).
func (a *App) Hostname() string {
	nome, err := os.Hostname()
	if err != nil {
		return ""
	}
	return nome
}

// checkOpenable reports whether path is a file that can be handed to the system, and
// returns it trimmed. Kept apart from OpenFile so it can be tested without launching a
// PDF reader on the machine running the tests.
func checkOpenable(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("non se indicou ningún ficheiro")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("non se atopou o ficheiro: %w", err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("%q é un cartafol, non un ficheiro", filepath.Base(path))
	}
	return path, nil
}

// OpenFile opens path with whatever application the system uses for that kind of file
// — the PDF reader, for the signed report the student has just generated. The file is
// only handed to the system, never read here.
func (a *App) OpenFile(path string) error {
	path, err := checkOpenable(path)
	if err != nil {
		return err
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// FileProtocolHandler opens the path with its associated program without
		// going through the shell, so spaces and accents in the name need no quoting.
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	// Start, not Run: the reader keeps running after this returns, and waiting for the
	// student to close it would freeze the app.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("non se puido abrir o ficheiro: %w", err)
	}
	return nil
}
