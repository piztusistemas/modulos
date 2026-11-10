package main

import (
	"net/http"
	"sync"
	"time"
)

// Segundo modelo de autenticación no MESMO servidor/socket de tao/api.go
// (ver mux() en api.go): token Bearer por módulo externo de Tao, con
// permisos concretos concedidos ao activalo — porte, en pequeno, de
// piztu/internal/api/auth.go. Convive co token estático de proceso xa
// existente (api_auth.go, usado por Pancho/Yang, gateado polo rol da sesión
// de Tao): son dous consumidores distintos, non hai por que unificalos —
// aquel segue igual.

// ttlSesionModuloTao é canto dura un token de módulo unha vez emitido (en
// AbrirModuloTao) — mesmo criterio ca Piztu: sobrevive a reinicios do
// módulo cliente sen ter que volver activalo, pero non para sempre.
const ttlSesionModuloTao = 12 * time.Hour

// sesionModuloTao é o que un token de módulo representa mentres é válido.
type sesionModuloTao struct {
	moduloID string
	permisos []string
	expira   time.Time
}

// ten reporta se esta sesión ten concedido `permiso` — cadea baleira sempre
// abonda (equivalente a "sen permiso requirido", coma /whoami).
func (s sesionModuloTao) ten(permiso string) bool {
	if permiso == "" {
		return true
	}
	for _, p := range s.permisos {
		if p == permiso {
			return true
		}
	}
	return false
}

// rexistroSesionsModulosTao vive só en memoria (coma en Piztu): reiniciar
// Tao invalida todos os tokens de módulo emitidos, obrigando a relanzalos
// (AbrirModuloTao volve emitir un token novo cada vez que se abre o
// módulo).
type rexistroSesionsModulosTao struct {
	mu      sync.Mutex
	sesions map[string]sesionModuloTao
}

func novoRexistroSesionsModulosTao() *rexistroSesionsModulosTao {
	return &rexistroSesionsModulosTao{sesions: map[string]sesionModuloTao{}}
}

// emitir crea un token novo para moduloID cos permisos indicados (copia dos
// permisos xa concedidos ao activar o módulo, ver SetModuloTao) e devólveo.
func (r *rexistroSesionsModulosTao) emitir(moduloID string, permisos []string, ttl time.Duration) string {
	token := novoTokenAPI() // mesmo xerador aleatorio ca o token estático (api_auth.go)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sesions[token] = sesionModuloTao{moduloID: moduloID, permisos: permisos, expira: time.Now().Add(ttl)}
	return token
}

// validar comproba un token e purga automaticamente calquera xa caducado.
func (r *rexistroSesionsModulosTao) validar(token string) (sesionModuloTao, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ses, ok := r.sesions[token]
	if !ok {
		return sesionModuloTao{}, false
	}
	if time.Now().After(ses.expira) {
		delete(r.sesions, token)
		return sesionModuloTao{}, false
	}
	return ses, true
}

// revogarModulo esquece TODOS os tokens vivos dun módulo — chamado ao
// desactivalo (SetModuloTao(id, false)), para que un token xa entregado
// deixe de servir no acto, sen agardar ao seu TTL.
func (r *rexistroSesionsModulosTao) revogarModulo(moduloID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for token, ses := range r.sesions {
		if ses.moduloID == moduloID {
			delete(r.sesions, token)
		}
	}
}

// EmitirTokenModulo/RevogarModulo son a interface pública do apiServidor
// cara ao resto de Tao (modulos_binding_tao.go) — manteñen r.mu privado.
func (s *apiServidor) EmitirTokenModulo(moduloID string, permisos []string) string {
	return s.sesionsModulos.emitir(moduloID, permisos, ttlSesionModuloTao)
}

func (s *apiServidor) RevogarModulo(moduloID string) {
	s.sesionsModulos.revogarModulo(moduloID)
}

// handlerAutenticadoModuloTao é a forma dun handler xa autenticado coma
// módulo externo — recibe a sesión resolta (moduloID/permisos) en vez de ter
// que revalidar o token el mesmo.
type handlerAutenticadoModuloTao func(w http.ResponseWriter, r *http.Request, ses sesionModuloTao)

// autenticarModulo esixe un token de módulo válido (rexistroSesionsModulosTao,
// NON o token estático de proceso que usa autenticar() en api_auth.go) e,
// se `permiso` non é "", que a sesión o teña concedido. 401 sen token
// válido, 403 sen o permiso concreto — nunca 404.
func (s *apiServidor) autenticarModulo(permiso string, next handlerAutenticadoModuloTao) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extraerBearerAPI(r.Header.Get("Authorization"))
		if token == "" {
			escribirErroAPI(w, http.StatusUnauthorized, "token_invalido",
				"falta a cabeceira Authorization: Bearer <token>")
			return
		}
		ses, ok := s.sesionsModulos.validar(token)
		if !ok {
			escribirErroAPI(w, http.StatusUnauthorized, "token_invalido",
				"token de módulo non válido ou caducado — o módulo debe reabrirse")
			return
		}
		if !ses.ten(permiso) {
			escribirErroAPI(w, http.StatusForbidden, "permiso_insuficiente",
				"este módulo non ten concedido o permiso \""+permiso+"\"")
			return
		}
		next(w, r, ses)
	}
}

// respostaModuloWhoamiAPI é a resposta a GET /api/v1/modulos/whoami — sen
// permiso requirido, coma o whoami xeral: só confirma identidade e permisos
// concedidos, nada sensible.
type respostaModuloWhoamiAPI struct {
	ModuloID string   `json:"moduloId"`
	Permisos []string `json:"permisos"`
	Version  string   `json:"version"`
}

func (s *apiServidor) handleModuloWhoami(w http.ResponseWriter, r *http.Request, ses sesionModuloTao) {
	escribirJSONAPI(w, http.StatusOK, respostaModuloWhoamiAPI{
		ModuloID: ses.moduloID, Permisos: ses.permisos, Version: VersionAPI,
	})
}

// handleModuloListMaterias reexpón ListMaterias (tao/profesor.go) — mesmos
// datos ca GET /api/v1/materias, pero gateado por permiso de módulo
// ("materias.ler") en vez de polo rol de sesión.
func (s *apiServidor) handleModuloListMaterias(w http.ResponseWriter, r *http.Request, ses sesionModuloTao) {
	materias, err := s.app.ListMaterias()
	if err != nil {
		escribirErroAPI(w, http.StatusBadGateway, "erro_servidor_ficheiros", err.Error())
		return
	}
	escribirJSONAPI(w, http.StatusOK, materias)
}
