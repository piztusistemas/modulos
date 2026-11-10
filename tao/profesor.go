package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// Professor mode.
//
// A professor is an ordinary File Browser account whose username is listed in the
// shared manifest dixitalizacion/profesores.json (the admin grants the role from the
// admin panel). Professors author the "programación" — calendar, didactic units,
// holidays and schedule — once per subject (materia), shared with every student of
// the assigned groups, and may inspect and edit each student's work. Students only
// publish the per-session content (their daily logs); they cannot edit the
// programación.
//
// The shared content lives in the dixitalizacion/ folder (see filebrowser.go):
//   profesores.json      -> ["m.pardinas", ...]
//   alumnos.json         -> { "<login>": {name, surname, group}, ... }
//   materias/<id>.json   -> one MateriaDef per subject
// Students are matched to a materia by group (their roster group ∈ materia.Groups).

// MateriaDef is the professor-authored definition of a subject: the calendar/schedule
// configuration and the didactic units, plus the groups it is taught to. It is the
// shared half of the frontend's Subject; the per-student half (logs and reports) stays
// in each student's own backup (users/<login>/dixitalizacion-backup.json).
type MateriaDef struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	AcademicYear string   `json:"academicYear"`
	Groups       []string `json:"groups"` // class groups this materia is taught to
	// Owner is the File Browser username of the professor who created this materia.
	// Only that professor (or an administrator, via Admin* methods) may view or edit
	// it — see requireOwnsMateria and SaveMateria/DeleteMateria below. Empty means an
	// "unclaimed" materia predating this field (created before ownership existed): it
	// stays visible/editable to any professor until the next SaveMateria adopts it.
	Owner  string         `json:"owner,omitempty"`
	Config map[string]any `json:"config"` // AppConfig (startDate, endDate, scheduleDays, holidays, ...)
	Units  []any          `json:"units"`  // Unit[] (title, sessions, obxectivos, contidos, ...)
	// Logs is the professor's own per-session record (keyed by date): what was worked on
	// each class day and whether the class happened (default) or the reason it did not
	// (folga, excursión/charla, ausencia...). It is the authoritative log for the
	// department memory and Proens tracking, kept apart from each student's own logs.
	Logs map[string]any `json:"logs,omitempty"`
	// ProgramacionInfo is the general "programación" info scraped from Proens
	// (introdución, obxectivos e a súa contribución ás competencias, criterios de
	// cualificación, e o desglose currículo/avaliación por UD — ver ProensInfo,
	// proens_info.go), cacheada aquí para que un dispositivo calquera (o propio
	// profesor noutra máquina, ou o alumnado) a lea do servidor en vez de terse
	// que conectar a Proens el mesmo. Escríbese ao premer "Extraer Datos" en
	// Configuración ou "Actualizar" no panel de programación (App.tsx,
	// ProgramacionPanel.tsx) — sempre por un profesor coa súa propia materia,
	// nunca por un alumno nin en modo revisión.
	ProgramacionInfo *ProensInfo `json:"programacionInfo,omitempty"`
	// ProgramacionInfoUpdatedAt is the ISO timestamp of the last successful
	// extraction that populated ProgramacionInfo.
	ProgramacionInfoUpdatedAt string `json:"programacionInfoUpdatedAt,omitempty"`
}

// RosterEntry is one student's identity record in alumnos.json. The group drives the
// group-based enrolment.
type RosterEntry struct {
	Name    string `json:"name"`
	Surname string `json:"surname"`
	Group   string `json:"group"`
}

// RosterRow is a roster entry flattened with its username, returned to the frontend as
// a list (Wails does not emit a bound type for a map's value, only for slice elements).
type RosterRow struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Surname  string `json:"surname"`
	Group    string `json:"group"`
}

// ---- Shared manifest lookups (used at login, tolerate a missing file) -----------

// fbIsProfessor reports whether username is listed in profesores.json.
func (a *App) fbIsProfessor(username string) bool {
	var profs []string
	if _, err := a.fbReadSharedJSON(sharedProfesores, &profs); err != nil {
		return false
	}
	for _, p := range profs {
		if strings.EqualFold(strings.TrimSpace(p), username) {
			return true
		}
	}
	return false
}

// fbRosterEntry returns username's roster record from alumnos.json, if present.
func (a *App) fbRosterEntry(username string) (RosterEntry, bool) {
	roster := map[string]RosterEntry{}
	if _, err := a.fbReadSharedJSON(sharedAlumnos, &roster); err != nil {
		return RosterEntry{}, false
	}
	for name, e := range roster {
		if strings.EqualFold(name, username) {
			return e, true
		}
	}
	return RosterEntry{}, false
}

// fbSetRosterEntry upserts username's roster record (alumnos.json). Called by the
// admin enrolment with an active administrator File Browser session.
func (a *App) fbSetRosterEntry(username string, e RosterEntry) error {
	roster := map[string]RosterEntry{}
	a.fbReadSharedJSON(sharedAlumnos, &roster)
	if roster == nil {
		roster = map[string]RosterEntry{}
	}
	roster[username] = e
	return a.fbWriteSharedJSON(sharedAlumnos, roster)
}

// fbRemoveRosterEntry drops username from the roster (best-effort, case-insensitive).
func (a *App) fbRemoveRosterEntry(username string) error {
	roster := map[string]RosterEntry{}
	if ok, _ := a.fbReadSharedJSON(sharedAlumnos, &roster); !ok {
		return nil
	}
	for k := range roster {
		if strings.EqualFold(k, username) {
			delete(roster, k)
		}
	}
	return a.fbWriteSharedJSON(sharedAlumnos, roster)
}

// fbUUIDEntry returns username's internal UUID from alumnos-uuid.json, if present.
func (a *App) fbUUIDEntry(username string) (string, bool) {
	uuids := map[string]string{}
	if _, err := a.fbReadSharedJSON(sharedAlumnosUUID, &uuids); err != nil {
		return "", false
	}
	for name, id := range uuids {
		if strings.EqualFold(name, username) {
			return id, true
		}
	}
	return "", false
}

// fbSetUUIDEntry upserts username's internal UUID (alumnos-uuid.json). Called by the
// admin enrolment alongside fbSetRosterEntry.
func (a *App) fbSetUUIDEntry(username, uuid string) error {
	uuids := map[string]string{}
	a.fbReadSharedJSON(sharedAlumnosUUID, &uuids)
	if uuids == nil {
		uuids = map[string]string{}
	}
	uuids[username] = uuid
	return a.fbWriteSharedJSON(sharedAlumnosUUID, uuids)
}

// fbRemoveUUIDEntry drops username from alumnos-uuid.json (best-effort, case-insensitive).
func (a *App) fbRemoveUUIDEntry(username string) error {
	uuids := map[string]string{}
	if ok, _ := a.fbReadSharedJSON(sharedAlumnosUUID, &uuids); !ok {
		return nil
	}
	for k := range uuids {
		if strings.EqualFold(k, username) {
			delete(uuids, k)
		}
	}
	return a.fbWriteSharedJSON(sharedAlumnosUUID, uuids)
}

// requireProfessor guards the professor-only methods.
func (a *App) requireProfessor() error {
	if a.profile == nil || a.profile.Role != "profesor" {
		return fmt.Errorf("non hai unha sesión de profesor activa")
	}
	return nil
}

// ---- Materias (subject definitions) ---------------------------------------------

// listMateriasRaw reads every subject definition in the shared folder, assuming a File
// Browser session is already open (a.fbURL/a.fbToken) — either the logged-in user's own
// (student, professor) or one borrowed via withAdminFB. It does NOT check a.profile, so
// it works from an administrator session too (a.profile is nil there) — see
// AdminListMaterias (backup.go). ListMaterias and GetMyMaterias keep their own guard for
// the normal student/professor session and delegate here.
func (a *App) listMateriasRaw() ([]MateriaDef, error) {
	dir := sharedRoot + "/" + sharedMateriasDir
	items, err := a.fbList(dir)
	if err != nil {
		if errors.Is(err, errFBNonAtopado) {
			return nil, nil // aínda non se creou o cartafol: non hai materias
		}
		// Calquera outro fallo (permisos, servidor caído, resposta rara) NON é "non hai
		// materias": propágase para que a interface poida dicir que pasou de verdade.
		log.Printf("[materias] non se puido listar %s: %v", dir, err)
		return nil, err
	}
	var nomes []string
	for _, it := range items {
		if it.IsDir || !strings.HasSuffix(strings.ToLower(it.Name), ".json") {
			continue
		}
		nomes = append(nomes, it.Name)
	}
	// As definicións báixanse en paralelo (ver concorrencia.go): en serie, abrir a
	// programación custaba unha ida e volta por materia.
	lidas := make([]*MateriaDef, len(nomes))
	enParalelo(len(nomes), func(i int) {
		content, err := a.fbRawGet(dir + "/" + nomes[i])
		if err != nil || len(content) == 0 {
			return
		}
		var m MateriaDef
		if err := json.Unmarshal(content, &m); err != nil {
			return // skip a corrupt file rather than failing the whole list
		}
		lidas[i] = &m
	})
	var out []MateriaDef
	for _, m := range lidas { // a orde do listado consérvase
		if m != nil {
			out = append(out, *m)
		}
	}
	return out, nil
}

// ListMaterias returns every subject definition in the shared folder, regardless of
// owner. Used internally by GetMyMaterias for students (a materia's owner is
// irrelevant to which students it is taught to) and by AdminListMaterias. Professors
// managing their own materias should call ListMyMaterias instead — see there for why.
func (a *App) ListMaterias() ([]MateriaDef, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	return a.listMateriasRaw()
}

// ListMyMaterias returns only the subject definitions owned by the logged-in professor
// (plus any unclaimed, owner-less materia predating this field — see MateriaDef.Owner).
// This is what the professor's own materia selector should call, so a professor never
// even sees another professor's materia, let alone edits it.
func (a *App) ListMyMaterias() ([]MateriaDef, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	all, err := a.listMateriasRaw()
	if err != nil {
		return nil, err
	}
	var out []MateriaDef
	for _, m := range all {
		if m.Owner == "" || strings.EqualFold(m.Owner, a.fbUser) {
			out = append(out, m)
		}
	}
	return out, nil
}

// GetMyMaterias returns the subject definitions whose groups include the logged-in
// student's group. Used to show the student only the programación that concerns them.
func (a *App) GetMyMaterias() ([]MateriaDef, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	group := strings.TrimSpace(a.profile.Group)
	all, err := a.ListMaterias()
	if err != nil {
		return nil, err
	}
	if group == "" {
		return []MateriaDef{}, nil // no group: nothing enrolled
	}
	var out []MateriaDef
	for _, m := range all {
		for _, g := range m.Groups {
			// group pode conter varios grupos ("eso3a, eso3b"): ver grupos.go.
			if pertenceAGrupo(group, g) {
				out = append(out, m)
				break
			}
		}
	}
	return out, nil
}

// SaveMateria writes (creates or overwrites) a subject definition. Professor only, and
// only over a materia the professor owns (or one still unclaimed — see MateriaDef.Owner):
// a materia owned by someone else is rejected outright, and the Owner field itself is
// always resolved server-side (never trusted from the client), so a professor can
// neither claim another's materia nor blank out its ownership.
func (a *App) SaveMateria(m MateriaDef) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return fmt.Errorf("a materia precisa un identificador")
	}
	if strings.ContainsAny(m.ID, "/\\") {
		return fmt.Errorf("identificador de materia non válido")
	}

	existing, err := a.findMateria(m.ID)
	if err != nil {
		return err
	}
	if existing == nil || existing.Owner == "" {
		m.Owner = a.fbUser // new materia, or adopting a legacy unclaimed one
	} else if !strings.EqualFold(existing.Owner, a.fbUser) {
		return fmt.Errorf("esta materia pertence a outro profesor; só un administrador pode modificala")
	} else {
		m.Owner = existing.Owner
	}
	if existing != nil {
		a.gardarHistorialMateria(m.ID)
	}
	return a.fbWriteSharedJSON(sharedMateriasDir+"/"+m.ID+".json", m)
}

// sharedMateriasHistorialDir garda, por materia, as versións anteriores de cada
// materias/<id>.json (dixitalizacion/materias-historial/<id>/<data>.json). SaveMateria
// sobrescribe a materia enteira — rexistros de sesión incluídos — co que trae o
// cliente, así que unha reprogramación defectuosa ou unha copia local desfasada podía
// borrar «Contidos da sesión» xa publicados sen deixar rastro (pasou en TICS 1ºBAC o
// 29/10/2026). Con esta copia previa sempre se pode recuperar a versión anterior.
// Vive fóra de materias/ para que listMateriasRaw nunca a confunda cunha materia.
const sharedMateriasHistorialDir = "materias-historial"

// materiaHistorialMax é cantas versións anteriores se conservan por materia. Cada
// gardado (tamén un desbloqueo de sesión) crea unha, e cada unha ocupa uns poucos KB.
const materiaHistorialMax = 60

// gardarHistorialMateria copia a versión actual de materias/<id>.json ao historial antes
// de que SaveMateria a sobrescriba, e poda as máis antigas. É a mellor posible: un fallo
// aquí rexístrase pero nunca impide gardar a materia.
func (a *App) gardarHistorialMateria(id string) {
	actual, err := a.fbRawGet(sharedRoot + "/" + sharedMateriasDir + "/" + id + ".json")
	if err != nil || len(actual) == 0 {
		return
	}
	dir := sharedRoot + "/" + sharedMateriasHistorialDir + "/" + id
	nome := time.Now().Format("2006-01-02_150405.000") + ".json"
	if err := a.fbUploadDirect(dir+"/"+nome, actual); err != nil {
		log.Printf("[materias] non se puido gardar a copia previa de %s: %v", id, err)
		return
	}
	items, err := a.fbList(dir)
	if err != nil {
		return
	}
	var versions []string
	for _, it := range items {
		if !it.IsDir && strings.HasSuffix(strings.ToLower(it.Name), ".json") {
			versions = append(versions, it.Name)
		}
	}
	sort.Strings(versions) // o nome é a data: orde alfabética = orde cronolóxica
	for i := 0; i < len(versions)-materiaHistorialMax; i++ {
		if err := a.fbDeleteDirect(dir + "/" + versions[i]); err != nil {
			log.Printf("[materias] non se puido podar %s/%s: %v", dir, versions[i], err)
		}
	}
}

// DeleteMateria removes a subject definition. Professor only, and only one the
// professor owns (or one still unclaimed) — same rule as SaveMateria.
func (a *App) DeleteMateria(id string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("identificador de materia non válido")
	}
	existing, err := a.findMateria(id)
	if err != nil {
		return err
	}
	if existing != nil && existing.Owner != "" && !strings.EqualFold(existing.Owner, a.fbUser) {
		return fmt.Errorf("esta materia pertence a outro profesor; só un administrador pode eliminala")
	}
	return a.fbDelete(sharedRoot + "/" + sharedMateriasDir + "/" + id + ".json")
}

// findMateria returns the current definition of materiaID, or nil if it does not exist
// yet. Shared by the ownership checks in SaveMateria/DeleteMateria/requireOwnsMateria.
func (a *App) findMateria(materiaID string) (*MateriaDef, error) {
	all, err := a.listMateriasRaw()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == materiaID {
			return &all[i], nil
		}
	}
	return nil, nil
}

// requireOwnsMateria guards the professor-only, per-materia methods introduced for
// incidents (incidencias.go) and private notes (notas_privadas.go): a professor may act
// only on a materia they own (or one still unclaimed, same rule as SaveMateria/
// DeleteMateria) — this is what keeps one professor's notes on a student out of another
// professor's reach, per materia. Returns the materia so callers do not have to look it
// up again.
func (a *App) requireOwnsMateria(materiaID string) (MateriaDef, error) {
	if err := a.requireProfessor(); err != nil {
		return MateriaDef{}, err
	}
	materiaID = strings.TrimSpace(materiaID)
	if materiaID == "" {
		return MateriaDef{}, fmt.Errorf("indica a materia")
	}
	m, err := a.findMateria(materiaID)
	if err != nil {
		return MateriaDef{}, err
	}
	if m == nil {
		return MateriaDef{}, fmt.Errorf("non existe esa materia")
	}
	if m.Owner != "" && !strings.EqualFold(m.Owner, a.fbUser) {
		return MateriaDef{}, fmt.Errorf("esta materia pertence a outro profesor")
	}
	return *m, nil
}

// ---- Student roster and work (professor consultation/edition) -------------------

// ListRoster returns the student roster as a list. Professor only; used to list the
// students of a group and open their work.
func (a *App) ListRoster() ([]RosterRow, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	roster := map[string]RosterEntry{}
	if _, err := a.fbReadSharedJSON(sharedAlumnos, &roster); err != nil {
		return nil, err
	}
	out := make([]RosterRow, 0, len(roster))
	for username, e := range roster {
		// Defensivo: alumnos.json e profesores.json xestiónanse por separado, así que un
		// username pode quedar en ambos por erro de alta (visto en produción: "prof1" con
		// rol profesor tamén dado de alta coma alumno). Un profesor nunca debe aparecer no
		// selector de alumnado, aínda que os datos estean mal.
		if a.fbIsProfessor(username) {
			continue
		}
		out = append(out, RosterRow{Username: username, Name: e.Name, Surname: e.Surname, Group: e.Group})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

// GetStudentBackup downloads a student's backup JSON (their logs and reports) so the
// professor can review it. Returns "" when the student has no backup yet. Professor only.
func (a *App) GetStudentBackup(username string) (string, error) {
	if err := a.requireProfessor(); err != nil {
		return "", err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return "", fmt.Errorf("indica o alumno")
	}
	content, err := a.fbDownload(fbUsersRoot + "/" + username + "/" + fbBackupName)
	if err != nil {
		return "", nil // no backup yet
	}
	return content, nil
}

// SaveStudentLogComment sets (or clears, when empty) the professor's feedback comment
// on a student's práctica for one session date, patching it straight into the student's
// backup so it survives independently of any other edit and reaches the student on their
// next login. It reads the current backup, touches only logs[date].profesorComment of
// the given materia (creating a minimal log/subject if needed) and writes it back.
// Professor only.
func (a *App) SaveStudentLogComment(username, materiaID, date, comment string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	materiaID = strings.TrimSpace(materiaID)
	date = strings.TrimSpace(date)
	if username == "" || materiaID == "" || date == "" {
		return fmt.Errorf("faltan datos (alumno, materia ou data)")
	}

	subjects, err := a.loadStudentSubjects(username)
	if err != nil {
		return err
	}
	entry := logEntryFor(subjects, materiaID, date)
	if comment = strings.TrimSpace(comment); comment == "" {
		delete(entry.entry, "profesorComment")
	} else {
		entry.entry["profesorComment"] = comment
	}
	return a.saveStudentSubjects(username, entry.subjects)
}

// studentLogEntry bundles the pieces logEntryFor locates/creates inside a student's
// backup, so callers (SaveStudentLogComment, AddIncident, RemoveIncident) can patch
// entry.entry in place and hand subjects straight back to saveStudentSubjects.
type studentLogEntry struct {
	subjects []map[string]any
	subj     map[string]any
	logs     map[string]any
	entry    map[string]any
}

// loadStudentSubjects downloads and unmarshals a student's backup into the same loose
// []map[string]any shape SaveStudentLogComment has always used: Go has no fixed schema
// for a student's backup (see the frontend Subject/DailyLog types), so professor-side
// patches touch only the keys they need and leave everything else untouched.
func (a *App) loadStudentSubjects(username string) ([]map[string]any, error) {
	raw, _ := a.fbDownload(fbUsersRoot + "/" + username + "/" + fbBackupName)
	var subjects []map[string]any
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &subjects); err != nil {
			return nil, fmt.Errorf("a copia do alumno non é válida: %w", err)
		}
	}
	return subjects, nil
}

// saveStudentSubjects re-marshals subjects and uploads it back as username's backup.
func (a *App) saveStudentSubjects(username string, subjects []map[string]any) error {
	out, err := json.MarshalIndent(subjects, "", "  ")
	if err != nil {
		return err
	}
	dir := fbUsersRoot + "/" + username
	if err := a.fbEnsureDir(dir); err != nil {
		return fmt.Errorf("non se puido acceder ao cartafol do alumno: %w", err)
	}
	return a.fbUpload(dir+"/"+fbBackupName, out)
}

// logEntryFor finds (creating stubs as needed) the subject/logs/day-entry for materiaID
// and date inside subjects, exactly as SaveStudentLogComment always has — a student with
// no work yet for that materia/day still gets a minimal stub so the patch has somewhere
// to land. The returned studentLogEntry.subjects is subjects itself (grown in place when
// a stub subject was appended), ready to pass to saveStudentSubjects.
func logEntryFor(subjects []map[string]any, materiaID, date string) studentLogEntry {
	var subj map[string]any
	for _, s := range subjects {
		if id, _ := s["id"].(string); id == materiaID {
			subj = s
			break
		}
	}
	if subj == nil {
		subj = map[string]any{"id": materiaID, "logs": map[string]any{}}
		subjects = append(subjects, subj)
	}
	logs, ok := subj["logs"].(map[string]any)
	if !ok || logs == nil {
		logs = map[string]any{}
		subj["logs"] = logs
	}
	entry, ok := logs[date].(map[string]any)
	if !ok || entry == nil {
		entry = map[string]any{"date": date, "content": "", "status": "lectivo"}
		logs[date] = entry
	}
	return studentLogEntry{subjects: subjects, subj: subj, logs: logs, entry: entry}
}

// SaveStudentBackup overwrites a student's backup with professor-edited content, so
// the change reaches the student on their next login. Professor only.
func (a *App) SaveStudentBackup(username, jsonStr string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("indica o alumno")
	}
	if strings.TrimSpace(jsonStr) == "" {
		return fmt.Errorf("o contido está baleiro")
	}
	dir := fbUsersRoot + "/" + username
	if err := a.fbEnsureDir(dir); err != nil {
		return fmt.Errorf("non se puido acceder ao cartafol do alumno: %w", err)
	}
	return a.fbUpload(dir+"/"+fbBackupName, []byte(jsonStr))
}
