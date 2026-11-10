package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func novoTokenAPI() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "tao_" + hex.EncodeToString(b)
}

func extraerBearerAPI(cabeceira string) string {
	const prefixo = "Bearer "
	if !strings.HasPrefix(cabeceira, prefixo) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(cabeceira, prefixo))
}

// apiAutorizado reports whether the CURRENTLY active Tao session may use the
// API: a logged-in professor, or an unlocked administration panel
// (AdminLogin). Checked on every request, not when the token was issued —
// the session can change (login/logout) at any moment while the server
// keeps running between startup() and shutdown(). Guarded by apiEstadoMu
// because HTTP handlers run on goroutines distinct from the Wails bindings
// goroutine that assigns a.profile/a.isAdmin (see auth.go, admin.go).
func (a *App) apiAutorizado() bool {
	a.apiEstadoMu.RLock()
	defer a.apiEstadoMu.RUnlock()
	return (a.profile != nil && a.profile.Role == "profesor") || a.isAdmin
}

// autenticar requires the process's static Bearer token and, if requireRole,
// also apiAutorizado(). 401 without a valid token, 403 without an authorized
// role session — never 404 (that the route exists is not a secret).
func (s *apiServidor) autenticar(requireRole bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extraerBearerAPI(r.Header.Get("Authorization"))
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			escribirErroAPI(w, http.StatusUnauthorized, "token_invalido",
				"falta ou non é válida a cabeceira Authorization: Bearer <token>")
			return
		}
		if requireRole && !s.app.apiAutorizado() {
			escribirErroAPI(w, http.StatusForbidden, "sen_sesion_autorizada",
				"precísase unha sesión de profesor ou administración activa en Tao")
			return
		}
		next(w, r)
	}
}

// recuperar captures a panic in any handler and returns the same JSON
// envelope (never Go's default HTML page).
func (s *apiServidor) recuperar(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				escribirErroAPI(w, http.StatusInternalServerError, "erro_interno", "erro interno inesperado")
			}
		}()
		next(w, r)
	}
}

// ---- error envelope, same shape as piztu/internal/api/erro.go
// (docs/api-tao.md) — reimplemented here since tao and piztu are
// independent Go binaries/modules, no shared package.

type erroRespostaAPI struct {
	Erro erroCorpoAPI `json:"erro"`
}

type erroCorpoAPI struct {
	Codigo  string `json:"codigo"`
	Mensaxe string `json:"mensaxe"`
}

func escribirErroAPI(w http.ResponseWriter, status int, codigo, mensaxe string) {
	escribirJSONAPI(w, status, erroRespostaAPI{Erro: erroCorpoAPI{Codigo: codigo, Mensaxe: mensaxe}})
}

func escribirJSONAPI(w http.ResponseWriter, status int, dato any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dato)
}
