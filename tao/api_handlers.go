package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// respostaWhoamiAPI is the answer to GET /api/v1/whoami — the first call any
// consumer should make to know whether it can operate at all right now.
type respostaWhoamiAPI struct {
	Version   string `json:"version"`
	Role      string `json:"role"`      // "profesor" | "administrador" | "ningunha"
	Conectado bool   `json:"conectado"` // profile.Connected; meaningless for "administrador"
}

// handleWhoami has no role gate — it reveals only the current role, nothing
// sensitive, so a consumer module can decide whether to try anything else
// before hitting a 403 on every other route.
func (s *apiServidor) handleWhoami(w http.ResponseWriter, r *http.Request) {
	s.app.apiEstadoMu.RLock()
	resp := respostaWhoamiAPI{Version: VersionAPI, Role: "ningunha"}
	switch {
	case s.app.isAdmin:
		resp.Role = "administrador"
	case s.app.profile != nil && s.app.profile.Role == "profesor":
		resp.Role = "profesor"
		resp.Conectado = s.app.profile.Connected
	}
	s.app.apiEstadoMu.RUnlock()
	escribirJSONAPI(w, http.StatusOK, resp)
}

// handleListMaterias wraps the existing ListMaterias (tao/profesor.go) — no
// student data lives in a MateriaDef, only the professor-authored
// programación (calendar, didactic units), see docs/api-tao.md.
func (s *apiServidor) handleListMaterias(w http.ResponseWriter, r *http.Request) {
	materias, err := s.app.ListMaterias()
	if err != nil {
		escribirErroAPI(w, http.StatusBadGateway, "erro_servidor_ficheiros", err.Error())
		return
	}
	escribirJSONAPI(w, http.StatusOK, materias)
}

func (s *apiServidor) handleSaveMateria(w http.ResponseWriter, r *http.Request) {
	var m MateriaDef
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		escribirErroAPI(w, http.StatusBadRequest, "corpo_invalido", "corpo JSON non válido: "+err.Error())
		return
	}
	if err := s.app.SaveMateria(m); err != nil {
		escribirErroAPI(w, http.StatusBadRequest, "erro_gardar_materia", err.Error())
		return
	}
	s.app.auditarAPI(r.Method, r.URL.Path)
	escribirJSONAPI(w, http.StatusOK, map[string]string{"id": m.ID})
}

func (s *apiServidor) handleDeleteMateria(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if err := s.app.DeleteMateria(id); err != nil {
		escribirErroAPI(w, http.StatusBadRequest, "erro_borrar_materia", err.Error())
		return
	}
	s.app.auditarAPI(r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNoContent)
}

// respostaUsuarioAPI is the answer to GET /api/v1/usuarios/{username} — a
// single-user identity lookup by an ALREADY KNOWN username. Deliberately not
// a roster dump: unlike ListRoster (tao/profesor.go, never exposed here,
// see docs/api-tao.md §8), this cannot enumerate who exists, only resolve
// one username a caller already has (e.g. from an equipo→usuario mapping it
// got elsewhere) into a display name.
type respostaUsuarioAPI struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Surname  string `json:"surname"`
}

// handleUsuario resolves username's display name. Audited even though it is
// a GET (every other read in this API is not) — this is the one capability
// that discloses personal identity, so it gets the same audit trail piztu
// gives its own dangerous capability (motor.executar).
func (s *apiServidor) handleUsuario(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PathValue("username"))
	if username == "" {
		escribirErroAPI(w, http.StatusBadRequest, "usuario_requirido", "indica o nome de usuario na ruta")
		return
	}
	name, surname, atopado := s.app.resolveIdentidadeAPI(username)
	if !atopado {
		escribirErroAPI(w, http.StatusNotFound, "usuario_non_atopado", "non se atopou ese usuario")
		return
	}
	s.app.auditarAPI(r.Method, r.URL.Path)
	escribirJSONAPI(w, http.StatusOK, respostaUsuarioAPI{Username: username, Name: name, Surname: surname})
}

// peticionTarefa is the body of POST /api/v1/tarefa — Yang pushes the
// Markdown it just generated for a session's task material here the moment
// it's ready (see docs/api-tao.md, yang/docs/api-yang.md,
// yang/taoclient.go). Only reachable by a Yang process THIS Tao launched
// (see yang_launch.go): Yang gets the socket+token from the one-shot
// contexto_yang.json Tao wrote at launch time, never from tao-api.json.
//
// Username is optional (docs/api-tao.md §11.4): absent writes the GENERAL
// explanation exactly as before this field existed; present writes THAT
// student's own override instead (SaveTaskDescriptionAlumno). Yang sends one
// POST per destinatario when repartindo iteracións — see yang/taoclient.go.
type peticionTarefa struct {
	MateriaID string `json:"materiaId"`
	Date      string `json:"date"`
	Markdown  string `json:"markdown"`
	Username  string `json:"username,omitempty"`
}

// tarefaActualizadaEvent is emitted on the Wails event bus so the "Tarefa"
// tab (App.tsx, TaskFilesSection) can refresh live the instant Yang pushes
// new content — no polling, no action needed from the professor. Carries
// the Markdown itself (not just the ids) so the frontend can apply it
// straight away without a round trip back through GetTaskDescription. Only
// emitted for the GENERAL write (see handleSaveTarefa): a per-student push
// must never overwrite the professor's general-text box.
type tarefaActualizadaEvent struct {
	MateriaID string `json:"materiaId"`
	Date      string `json:"date"`
	Markdown  string `json:"markdown"`
}

// tarefaAlumnoActualizadaEvent mirrors tarefaActualizadaEvent for a per-student
// override push — a separate event (not reused fields on the same event) so
// existing listeners that only expect the general shape are unaffected.
// Carries Username so a future "quen ten excepción" panel can update live
// instead of polling ListTaskOverrides; today nothing in App.tsx listens to
// this yet (pendente, docs/api-tao.md §11.4 aviso na UI).
type tarefaAlumnoActualizadaEvent struct {
	MateriaID string `json:"materiaId"`
	Date      string `json:"date"`
	Username  string `json:"username"`
	Markdown  string `json:"markdown"`
}

func (s *apiServidor) handleSaveTarefa(w http.ResponseWriter, r *http.Request) {
	var peticion peticionTarefa
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&peticion); err != nil {
		escribirErroAPI(w, http.StatusBadRequest, "corpo_invalido", "corpo JSON non válido: "+err.Error())
		return
	}
	if strings.TrimSpace(peticion.MateriaID) == "" || strings.TrimSpace(peticion.Date) == "" {
		escribirErroAPI(w, http.StatusBadRequest, "parametros_incompletos", "materiaId e date son obrigatorios")
		return
	}
	username := strings.TrimSpace(peticion.Username)
	var err error
	if username == "" {
		err = s.app.SaveTaskDescription(peticion.MateriaID, peticion.Date, peticion.Markdown)
	} else {
		err = s.app.SaveTaskDescriptionAlumno(peticion.MateriaID, peticion.Date, username, peticion.Markdown)
	}
	if err != nil {
		escribirErroAPI(w, http.StatusBadGateway, "erro_servidor_ficheiros", err.Error())
		return
	}
	s.app.auditarAPITarefa(r.Method, r.URL.Path, username)
	if username == "" {
		emitirEvento("tao:tarefa-actualizada", tarefaActualizadaEvent{
			MateriaID: peticion.MateriaID, Date: peticion.Date, Markdown: peticion.Markdown,
		})
	} else {
		emitirEvento("tao:tarefa-alumno-actualizada", tarefaAlumnoActualizadaEvent{
			MateriaID: peticion.MateriaID, Date: peticion.Date, Username: username, Markdown: peticion.Markdown,
		})
	}
	escribirJSONAPI(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resolveIdentidadeAPI looks up username's name/surname: first in the shared
// roster (dixitalizacion/alumnos.json, authoritative for students — see
// fbRosterEntry in tao/profesor.go), falling back to this machine's local
// profiles.json (auth.go) which also covers professors who have logged in
// here before. Degrades gracefully with no session connected: fbRosterEntry
// then simply finds nothing and the local fallback still applies.
func (a *App) resolveIdentidadeAPI(username string) (name, surname string, atopado bool) {
	if e, ok := a.fbRosterEntry(username); ok {
		return e.Name, e.Surname, true
	}
	profiles, err := a.loadProfiles()
	if err != nil {
		return "", "", false
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Username, username) {
			return p.Name, p.Surname, true
		}
	}
	return "", "", false
}
