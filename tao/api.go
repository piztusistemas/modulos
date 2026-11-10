package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// Local HTTP API — professor/administrator only.
//
// Tao runs on every machine (student or professor). This API lets another
// Piztu module on the SAME machine (e.g. Pancho/Yang) talk to the Tao
// instance that is already logged in as professor or administrator there,
// instead of re-implementing the File Browser contract itself. Full contract
// documented in docs/api-tao.md — this file and api_auth.go/api_handlers.go
// are its implementation.
//
// This is a deliberately simpler sibling of piztu/internal/api
// (docs/api-modulos.md): a single machine/user, a single static token per
// process (no per-module capability catalogue), and the real gate is the
// role of the CURRENTLY active Tao session, checked on every request (see
// apiAutorizado in api_auth.go) — not baked into the token at issue time,
// since login/logout can change the session without restarting the server.

// NomeSocketAPI is the socket file inside a.dataDir.
const NomeSocketAPI = "tao-api.sock"

// VersionAPI identifies the exposed contract (see GET /api/v1/whoami).
const VersionAPI = "v1"

// apiServidor serves the local API over a Unix socket. One instance per Tao
// process, created and put to listen in startup(), closed in shutdown().
type apiServidor struct {
	app      *App
	token    string // static per process, regenerated on every startup
	listener net.Listener

	// sesionsModulos is the second auth model, for external módulos de Tao
	// (api_auth_modulos.go) — a per-módulo Bearer token with concrete
	// permisos, unrelated to `token` above.
	sesionsModulos *rexistroSesionsModulosTao
}

// novoAPIServidor creates the server (not listening yet: see escoitar).
func novoAPIServidor(app *App) *apiServidor {
	return &apiServidor{app: app, token: novoTokenAPI(), sesionsModulos: novoRexistroSesionsModulosTao()}
}

func (s *apiServidor) rutaSocket() string {
	return filepath.Join(s.app.dataDir, NomeSocketAPI)
}

// escoitar opens the Unix socket and starts serving in the background. Any
// stale socket from an unclean previous shutdown is removed first (otherwise
// net.Listen fails with "address already in use").
func (s *apiServidor) escoitar() error {
	ruta := s.rutaSocket()
	_ = os.Remove(ruta)
	l, err := net.Listen("unix", ruta)
	if err != nil {
		return err
	}
	// 0600, not 0660 like piztu's shared daemon socket: there is no group to
	// coordinate here, just a single desktop user running Tao.
	if err := os.Chmod(ruta, 0o600); err != nil {
		l.Close()
		return err
	}
	// Wrap the listener so every accepted connection is checked against the
	// real calling binary (SO_PEERCRED on Linux, see api_peercred_linux.go)
	// before it ever reaches the token/HTTP layer — a bearer token alone
	// cannot tell "a Piztu módulo" apart from any other program running as
	// this same user, see docs/api-tao.md §3.
	s.listener = &listenerVerificadoAPI{Listener: l, app: s.app}
	go func() { _ = http.Serve(s.listener, s.mux()) }()
	return nil
}

// listenerVerificadoAPI rejects (closes, without ever reaching net/http) any
// connection whose peer isn't a recognized Piztu binary. baseDir is read
// fresh from app on every Accept (not cached at escoitar() time) — it's
// write-once early in startup(), before this listener can accept anything,
// so there's no race, just future-proofing against reordering.
type listenerVerificadoAPI struct {
	net.Listener
	app *App
}

func (l *listenerVerificadoAPI) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uc, ok := c.(*net.UnixConn)
		if !ok || !orixeRecoñecidaAPI(uc, l.app.baseDir, l.app.taoModulosDir) {
			c.Close()
			continue
		}
		return c, nil
	}
}

// pechar stops listening and removes the socket. Called from shutdown().
func (s *apiServidor) pechar() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.rutaSocket())
}

func (s *apiServidor) mux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/whoami", s.recuperar(s.autenticar(false, s.handleWhoami)))
	mux.Handle("GET /api/v1/materias", s.recuperar(s.autenticar(true, s.handleListMaterias)))
	mux.Handle("POST /api/v1/materias", s.recuperar(s.autenticar(true, s.handleSaveMateria)))
	mux.Handle("DELETE /api/v1/materias/{id}", s.recuperar(s.autenticar(true, s.handleDeleteMateria)))
	mux.Handle("GET /api/v1/usuarios/{username}", s.recuperar(s.autenticar(true, s.handleUsuario)))
	// Tao↔Yang pipe (see yang_launch.go, docs/api-tao.md): only reachable by
	// a Yang process Tao itself launched, using the socket+token from that
	// one-shot contexto_yang.json, not tao-api.json — see AbrirYangParaTarefa.
	mux.Handle("POST /api/v1/tarefa", s.recuperar(s.autenticar(true, s.handleSaveTarefa)))

	// Módulos externos de Tao (api_auth_modulos.go, modulos_tao.go): segundo
	// modelo de auth, por token de módulo con permisos concretos, non polo
	// token estático de proceso das rutas de arriba.
	mux.Handle("GET /api/v1/modulos/whoami", s.recuperar(s.autenticarModulo("", s.handleModuloWhoami)))
	mux.Handle("GET /api/v1/modulos/materias", s.recuperar(s.autenticarModulo("materias.ler", s.handleModuloListMaterias)))

	// Pipe Tao→Malta (malta_launch.go/malta_resultado.go, docs/api-tao.md
	// §6.2): mesmo modelo de auth cá do pipe Tao→Yang de enriba (token
	// estático de proceso, non o de módulo), só chamable por un proceso
	// Malta que o propio Tao acaba de lanzar cunha tarefa.
	mux.Handle("POST /api/v1/malta/resultado", s.recuperar(s.autenticar(true, s.handleMaltaResultado)))
	// Mesmo pipe/auth, para a redacción IA do informe asinado (ReportBuilder.tsx,
	// redaccion_launch.go/redaccion_resultado.go) — contrato distinto, non hai
	// unidades didácticas aquí.
	mux.Handle("POST /api/v1/malta/resultado-asinado", s.recuperar(s.autenticar(true, s.handleMaltaResultadoAsinado)))
	return mux
}
