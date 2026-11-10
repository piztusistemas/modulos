package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Backup and restore.
//
// A backup is a .zip saved to disk that mirrors the server folders (materias, tarefas,
// and optionally the users' folders) plus a manifest. Restore reads such a zip and
// re-creates a course, remapping every dated item BY SESSION ORDINAL: what a course
// taught on its Nth class day is restored onto the Nth class day of the target course,
// even if the weekday/date differ. This lets a professor reuse last year's programación,
// tasks and (optionally) student work in a freshly created course with a new calendar.

// backupManifest travels inside the zip as manifest.json.
type backupManifest struct {
	Version   int      `json:"version"`
	Kind      string   `json:"kind"`      // "site" or "professor"
	CreatedAt string   `json:"createdAt"` // RFC3339
	Courses   []string `json:"courses"`   // materia ids included
	Users     []string `json:"users"`     // user logins included (empty when not included)
}

// RestoreResult reports what a restore did, for the final notice.
type RestoreResult struct {
	SourceMateria   string   `json:"sourceMateria"`
	TargetMateria   string   `json:"targetMateria"`
	SessionsMapped  int      `json:"sessionsMapped"`
	DroppedSessions int      `json:"droppedSessions"` // source sessions with no slot in the target
	TaskFiles       int      `json:"taskFiles"`
	StudentsUpdated []string `json:"studentsUpdated"`
	StudentsSkipped []string `json:"studentsSkipped"` // logins in the archive with no account
}

const backupManifestName = "manifest.json"

// ---- Calendar: class-day sequence and session remap ------------------------------

// asString coerces a JSON value (from a map[string]any) to a trimmed string.
func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// nonLectiveDatesFromConfig returns the set of non-class dates declared in a materia's
// config: the calendar table when present (each row a single day or a [start,end] range),
// otherwise the legacy holidays/customHolidays lists. Mirrors nonLectiveDays in data.ts.
func nonLectiveDatesFromConfig(config map[string]any) map[string]bool {
	out := map[string]bool{}
	addRange := func(start, end string) {
		start, end = strings.TrimSpace(start), strings.TrimSpace(end)
		if start == "" {
			return
		}
		if end == "" || end < start {
			end = start
		}
		for d, guard := start, 0; d <= end && guard < 800; d, guard = addDaysISO(d, 1), guard+1 {
			out[d] = true
		}
	}
	if cal, ok := config["calendar"].([]any); ok {
		for _, raw := range cal {
			e, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			addRange(asString(e["start"]), asString(e["end"]))
		}
		return out
	}
	for _, key := range []string{"holidays", "customHolidays"} {
		if list, ok := config[key].([]any); ok {
			for _, raw := range list {
				if e, ok := raw.(map[string]any); ok {
					if d := asString(e["date"]); d != "" {
						out[d] = true
					}
				}
			}
		}
	}
	return out
}

// scheduleDaysFromConfig reads the weekday numbers of class days (0=Sun..6=Sat, matching
// JS getDay()), defaulting to Mon/Wed/Fri like the app does.
func scheduleDaysFromConfig(config map[string]any) map[int]bool {
	out := map[int]bool{}
	if raw, ok := config["scheduleDays"].([]any); ok && len(raw) > 0 {
		for _, v := range raw {
			if f, ok := v.(float64); ok {
				out[int(f)] = true
			}
		}
	}
	if len(out) == 0 {
		out[1], out[3], out[5] = true, true, true
	}
	return out
}

// addDaysISO adds n days to a YYYY-MM-DD date (noon-anchored, so DST never shifts the day).
func addDaysISO(date string, n int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// classDaySequence returns the ordered list of class-day dates for a materia's config:
// every date in [startDate, endDate] that is a scheduled weekday and not a weekend or a
// non-lective day. This is the "session ordinal" axis the remap is built on.
func classDaySequence(config map[string]any) ([]string, error) {
	start := asString(config["startDate"])
	end := asString(config["endDate"])
	if start == "" || end == "" {
		return nil, fmt.Errorf("o curso non ten datas de inicio/fin no calendario")
	}
	if end < start {
		return nil, fmt.Errorf("a data de fin é anterior á de inicio")
	}
	nonLective := nonLectiveDatesFromConfig(config)
	schedule := scheduleDaysFromConfig(config)

	var seq []string
	for d, guard := start, 0; d <= end && guard < 4000; d, guard = addDaysISO(d, 1), guard+1 {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			break
		}
		wd := int(t.Weekday()) // 0=Sun..6=Sat, same as JS getDay()
		if wd == 0 || wd == 6 {
			continue // weekend
		}
		if nonLective[d] {
			continue
		}
		if schedule[wd] {
			seq = append(seq, d)
		}
	}
	return seq, nil
}

// remapBySession maps each source class day to the target class day of the same ordinal.
// Source sessions beyond the target's length have no mapping (dropped).
func remapBySession(source, target []string) map[string]string {
	out := map[string]string{}
	n := len(source)
	if len(target) < n {
		n = len(target)
	}
	for i := 0; i < n; i++ {
		out[source[i]] = target[i]
	}
	return out
}

// ---- Recursive listing over File Browser -----------------------------------------

// fbWalk visits every file (not directories) under dir, calling fn with the full server
// path and its size. A missing/unreadable folder is skipped silently.
func (a *App) fbWalk(dir string, fn func(path string, size int64)) {
	items, err := a.fbList(dir)
	if err != nil {
		return
	}
	for _, it := range items {
		child := dir + "/" + it.Name
		if it.IsDir {
			a.fbWalk(child, fn)
		} else {
			fn(child, it.Size)
		}
	}
}

// ---- Zip building ----------------------------------------------------------------

// zipBuilder accumulates entries into an in-memory zip.
type zipBuilder struct {
	buf *bytes.Buffer
	w   *zip.Writer
}

func newZipBuilder() *zipBuilder {
	buf := &bytes.Buffer{}
	return &zipBuilder{buf: buf, w: zip.NewWriter(buf)}
}

func (z *zipBuilder) add(name string, content []byte) error {
	f, err := z.w.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(content)
	return err
}

func (z *zipBuilder) addJSON(name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return z.add(name, raw)
}

// addServerFile downloads a server file and stores it in the zip under archiveName.
func (a *App) addServerFile(z *zipBuilder, serverPath, archiveName string) error {
	content, err := a.fbRawGet(serverPath)
	if err != nil {
		return err // caller decides whether to tolerate
	}
	return z.add(archiveName, content)
}

// finishZip closes the archive and asks the user where to save it, writing it there.
// Returns the destination path, or "" when the save dialog is cancelled.
func (a *App) finishZip(z *zipBuilder, defaultName string) (string, error) {
	if err := z.w.Close(); err != nil {
		return "", err
	}
	dest, err := gardarFicheiro("Gardar a copia de seguridade", defaultName)
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // cancelled
	}
	if !strings.HasSuffix(strings.ToLower(dest), ".zip") {
		dest += ".zip"
	}
	if err := os.WriteFile(dest, z.buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("non se puido gardar a copia: %w", err)
	}
	return dest, nil
}

// ---- Backup: gathering course + user data into the zip ---------------------------

// backupCoursesInto adds the given materias and their task files to the zip, under
// shared/. Missing files are tolerated.
func (a *App) backupCoursesInto(z *zipBuilder, courseIDs []string) error {
	for _, id := range courseIDs {
		name := sharedMateriasDir + "/" + id + ".json"
		if err := a.addServerFile(z, sharedRoot+"/"+name, "shared/"+name); err != nil {
			continue // a listed course with no file: skip
		}
		a.fbWalk(sharedRoot+"/"+fbTasksRoot+"/"+id, func(p string, _ int64) {
			archive := "shared/" + strings.TrimPrefix(p, sharedRoot+"/")
			a.addServerFile(z, p, archive)
		})
	}
	return nil
}

// backupUsersInto adds each user's folder (backup, work files, messages, comment files)
// to the zip under users/<login>/.
func (a *App) backupUsersInto(z *zipBuilder, logins []string) {
	for _, login := range logins {
		a.fbWalk(fbUsersRoot+"/"+login, func(p string, _ int64) {
			a.addServerFile(z, p, p) // path already starts with "users/<login>/"
		})
	}
}

// ---- Backup entry points ---------------------------------------------------------

// SiteBackupResult é o que devolve unha copia do sitio: o .zip cos datos e, ao seu
// carón, o CSV coas altas.
//
// Van dous ficheiros porque son dúas cousas distintas e só unha vive nos ficheiros. As
// CONTAS de File Browser (usuario e contrasinal) están na súa base de datos, non no
// cartafol de datos, así que ningunha restauración as devolve: se se perde a base —
// unha reinstalación, un servidor novo— hai que dar de alta a todo o mundo outra vez
// ANTES de restaurar, ou os ficheiros volven a contas que non existen. Por iso a copia
// leva xa o CSV que a alta masiva do panel (CreateUsersFromCSV) sabe ler.
type SiteBackupResult struct {
	Zip string `json:"zip"`
	CSV string `json:"csv"`
	// Altas é o número de filas do CSV.
	Altas int `json:"altas"`
}

// csvDeAltas monta o CSV de altas a partir do roster do servidor: unha fila por
// persoa con cartafol, coas columnas que le parseUsersCSV. O contrasinal non se pode
// recuperar (no servidor só hai o seu hash), así que ou o pon quen fai a copia
// (contrasinal común para o día que haxa que restaurar) ou a columna queda baleira
// para enchela daquela.
func csvDeAltas(logins []string, roster map[string]RosterEntry, contrasinal string) (string, int) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	// "profile_field_grupo" e non "group": é o nome que le parseUsersCSV (vén dos
	// CSV exportados de Moodle). Cunha cabeceira "group" as contas crearíanse SEN
	// grupo, e sen grupo non se ve ningunha materia.
	w.Write([]string{"username", "password", "firstname", "lastname", "profile_field_grupo"})
	filas := 0
	for _, login := range logins {
		e := roster[login]
		w.Write([]string{login, contrasinal, strings.TrimSpace(e.Name), strings.TrimSpace(e.Surname), strings.TrimSpace(e.Group)})
		filas++
	}
	w.Flush()
	return b.String(), filas
}

// rutaCSVDeAltas devolve o nome do CSV que acompaña a un .zip de copia.
func rutaCSVDeAltas(rutaZip string) string {
	return strings.TrimSuffix(rutaZip, filepath.Ext(rutaZip)) + "-altas.csv"
}

// BackupSite makes a copy of the whole site (all courses and their tasks) and, when
// includeUsers is true, of every user's folder (backups, files, messages). Admin only.
// Returns the saved file path, or "" when cancelled.
func (a *App) BackupSite(includeUsers bool, contrasinalAltas string) (SiteBackupResult, error) {
	var res SiteBackupResult
	var dest string
	var rosterCSV map[string]RosterEntry
	var loginsCSV []string
	err := a.withAdminFB(func() error {
		z := newZipBuilder()

		// Course definitions and tasks.
		//
		// listMateriasRaw, NON ListMaterias: esta última esixe a.profile, que nunha
		// sesión de Director é nil (admin e profesorado/alumnado son sesións distintas,
		// ver auth.go/admin.go), así que devolvía erro sempre. E como o erro se
		// ignoraba, a copia "de todo o sitio" saía sen UN SÓ curso dentro, en silencio:
		// levaba os usuarios, as mensaxes e os manifestos, pero ningunha materia nin
		// tarefa — e despois non había nada que restaurar. Mesma trampa que xa
		// documenta AdminListMaterias. O erro, agora, aborta a copia: mellor non dar
		// copia ningunha que dar unha que parece boa e está incompleta.
		materias, err := a.listMateriasRaw()
		if err != nil {
			return fmt.Errorf("non se puideron listar os cursos: %w", err)
		}
		courseIDs := make([]string, 0, len(materias))
		for _, m := range materias {
			courseIDs = append(courseIDs, m.ID)
		}
		if err := a.backupCoursesInto(z, courseIDs); err != nil {
			return err
		}

		// Shared manifests and group messages.
		for _, name := range []string{sharedProfesores, sharedAlumnos, sharedMensaxesConfig} {
			a.addServerFile(z, sharedRoot+"/"+name, "shared/"+name)
		}
		a.fbWalk(sharedRoot+"/"+fbMessagesRoot, func(p string, _ int64) {
			a.addServerFile(z, p, "shared/"+strings.TrimPrefix(p, sharedRoot+"/"))
		})

		// Quen ten conta no sitio. Cóllese sempre, inclúanse ou non os ficheiros dos
		// usuarios: o CSV de altas precísao igual.
		roster := map[string]RosterEntry{}
		a.fbReadSharedJSON(sharedAlumnos, &roster)
		var logins []string
		for u := range roster {
			logins = append(logins, u)
		}
		// Also any user folder present on the server, even if not in the roster.
		if items, err := a.fbList(fbUsersRoot); err == nil {
			seen := map[string]bool{}
			for _, u := range logins {
				seen[strings.ToLower(u)] = true
			}
			for _, it := range items {
				if it.IsDir && !seen[strings.ToLower(it.Name)] {
					logins = append(logins, it.Name)
				}
			}
		}
		sort.Strings(logins)
		rosterCSV, loginsCSV = roster, logins
		if includeUsers {
			a.backupUsersInto(z, logins)
		}

		usuariosNoZip := logins
		if !includeUsers {
			usuariosNoZip = nil
		}
		if err := z.addJSON(backupManifestName, backupManifest{
			Version: 1, Kind: "site", CreatedAt: time.Now().Format(time.RFC3339),
			Courses: courseIDs, Users: usuariosNoZip,
		}); err != nil {
			return err
		}
		d, err := a.finishZip(z, "copia-sitio-"+time.Now().Format("2006-01-02")+".zip")
		dest = d
		return err
	})
	if err != nil || dest == "" {
		return res, err // erro, ou diálogo cancelado
	}
	res.Zip = dest

	// O CSV das altas, ao carón do zip. Un fallo aquí NON invalida a copia: dise, pero
	// devólvese igual o zip, que é o que leva os datos.
	texto, filas := csvDeAltas(loginsCSV, rosterCSV, strings.TrimSpace(contrasinalAltas))
	rutaCSV := rutaCSVDeAltas(dest)
	// 0600: se leva contrasinal, é unha credencial; e aínda sen el, é unha listaxe con
	// nomes e apelidos do alumnado.
	if err := os.WriteFile(rutaCSV, []byte(texto), 0o600); err != nil {
		return res, fmt.Errorf("gardouse a copia en %s, pero non se puido escribir o CSV de altas: %w", dest, err)
	}
	res.CSV, res.Altas = rutaCSV, filas
	return res, nil
}

// BackupCourses copies the selected courses (and their tasks) and, when
// includeStudents is true, the folders of the students enrolled in those courses'
// groups. Returns the saved file path, or "" when cancelled.
//
// Both roles can call it, each with its own File Browser session: the Director
// (admin) through the administration account — the whole point being that they can
// archive ONE course without having to copy the entire site — and a professor
// through their own account, limited to what that account may read.
func (a *App) BackupCourses(courseIDs []string, includeStudents bool) (string, error) {
	if len(courseIDs) == 0 {
		return "", fmt.Errorf("escolle polo menos un curso")
	}
	if a.isAdmin {
		var dest string
		err := a.withAdminFB(func() error {
			d, err := a.backupCoursesZip(courseIDs, includeStudents)
			dest = d
			return err
		})
		return dest, err
	}
	if err := a.requireProfessor(); err != nil {
		return "", err
	}
	return a.backupCoursesZip(courseIDs, includeStudents)
}

// backupCoursesZip is the shared body of BackupCourses: it assumes the caller has
// already put the right File Browser session in place.
func (a *App) backupCoursesZip(courseIDs []string, includeStudents bool) (string, error) {
	z := newZipBuilder()
	if err := a.backupCoursesInto(z, courseIDs); err != nil {
		return "", err
	}

	var logins []string
	if includeStudents {
		seen := map[string]bool{}
		for _, id := range courseIDs {
			students, _ := a.studentsInMateria(id)
			for _, u := range students {
				if !seen[strings.ToLower(u)] {
					seen[strings.ToLower(u)] = true
					logins = append(logins, u)
				}
			}
		}
		sort.Strings(logins)
		a.backupUsersInto(z, logins)
	}

	// Kind "professor" tamén cando fai a copia o Director: é o mesmo formato
	// (cursos soltos, sen os manifestos do sitio) e as copias xa feitas antes
	// desta función teñen que seguir cargándose igual.
	if err := z.addJSON(backupManifestName, backupManifest{
		Version: 1, Kind: "professor", CreatedAt: time.Now().Format(time.RFC3339),
		Courses: courseIDs, Users: logins,
	}); err != nil {
		return "", err
	}
	return a.finishZip(z, "copia-cursos-"+time.Now().Format("2006-01-02")+".zip")
}

// ---- Admin course management -----------------------------------------------------

// ---- Restore ---------------------------------------------------------------------

// requireBackupRole allows both the admin and a professor to back up / restore.
func (a *App) requireBackupRole() error {
	if a.isAdmin {
		return nil
	}
	return a.requireProfessor()
}

// LoadBackupArchive opens a file picker, reads the chosen backup .zip into memory and
// returns its manifest (so the UI can offer the courses it contains). The bytes are kept
// for the following RestoreCourse call. Admin or professor.
func (a *App) LoadBackupArchive() (backupManifest, error) {
	if err := a.requireBackupRole(); err != nil {
		return backupManifest{}, err
	}
	path, err := escollerFicheiro("Escolle a copia de seguridade (.zip)", application.FileFilter{DisplayName: "Copias de seguridade (*.zip)", Pattern: "*.zip"})
	if err != nil {
		return backupManifest{}, err
	}
	if path == "" {
		return backupManifest{}, nil // cancelled
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return backupManifest{}, fmt.Errorf("non se puido ler o ficheiro: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return backupManifest{}, fmt.Errorf("o ficheiro non é unha copia válida (.zip): %w", err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return backupManifest{}, err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return backupManifest{}, err
		}
		files[f.Name] = content
	}
	var man backupManifest
	if raw, ok := files[backupManifestName]; ok {
		json.Unmarshal(raw, &man)
	}
	a.restoreFiles = files
	a.restoreManifest = man
	return man, nil
}

// RestoreCourse restores one course from the loaded backup into an EXISTING target
// course, remapping every dated item by session ordinal onto the target's calendar. When
// withStudents is true, each student's práctica, work files and comment files are also
// restored, under the same login (skipping logins with no current account). Admin or
// professor.
func (a *App) RestoreCourse(sourceMateriaID, targetMateriaID string, withStudents bool) (RestoreResult, error) {
	if err := a.requireBackupRole(); err != nil {
		return RestoreResult{}, err
	}
	if a.restoreFiles == nil {
		return RestoreResult{}, fmt.Errorf("primeiro carga unha copia de seguridade")
	}
	if a.isAdmin {
		var res RestoreResult
		var rerr error
		err := a.withAdminFB(func() error {
			res, rerr = a.doRestore(sourceMateriaID, targetMateriaID, withStudents)
			return rerr
		})
		return res, err
	}
	return a.doRestore(sourceMateriaID, targetMateriaID, withStudents)
}

// doRestore performs the restore using the current File Browser session.
func (a *App) doRestore(sourceID, targetID string, withStudents bool) (RestoreResult, error) {
	sourceID, targetID = strings.TrimSpace(sourceID), strings.TrimSpace(targetID)
	if sourceID == "" || targetID == "" {
		return RestoreResult{}, fmt.Errorf("indica o curso de orixe e o de destino")
	}
	res := RestoreResult{SourceMateria: sourceID, TargetMateria: targetID}

	// Source course from the archive.
	srcRaw, ok := a.restoreFiles["shared/"+sharedMateriasDir+"/"+sourceID+".json"]
	if !ok {
		return res, fmt.Errorf("a copia non contén o curso %q", sourceID)
	}
	var src MateriaDef
	if err := json.Unmarshal(srcRaw, &src); err != nil {
		return res, fmt.Errorf("o curso de orixe da copia non é válido: %w", err)
	}

	// Target course from the server (its config is the NEW calendar to remap onto).
	var target MateriaDef
	if ok, err := a.fbReadSharedJSON(sharedMateriasDir+"/"+targetID+".json", &target); err != nil {
		return res, err
	} else if !ok {
		return res, fmt.Errorf("o curso de destino %q non existe; créao primeiro co seu calendario", targetID)
	}

	srcSeq, err := classDaySequence(src.Config)
	if err != nil {
		return res, fmt.Errorf("o curso de orixe: %w", err)
	}
	tgtSeq, err := classDaySequence(target.Config)
	if err != nil {
		return res, fmt.Errorf("o curso de destino: %w", err)
	}
	remap := remapBySession(srcSeq, tgtSeq)
	res.SessionsMapped = len(remap)
	if len(srcSeq) > len(remap) {
		res.DroppedSessions = len(srcSeq) - len(remap)
	}

	// ---- Course data: units + session logs + tasks ----
	target.Units = src.Units
	target.Logs = remapLogs(src.Logs, remap)
	if err := a.fbWriteSharedJSON(sharedMateriasDir+"/"+targetID+".json", target); err != nil {
		return res, fmt.Errorf("non se puido gardar o curso de destino: %w", err)
	}
	res.TaskFiles = a.restoreDatedFiles("shared/"+fbTasksRoot+"/"+sourceID+"/",
		sharedRoot+"/"+fbTasksRoot+"/"+targetID, remap)

	// ---- Student data ----
	if withStudents {
		roster := map[string]RosterEntry{}
		a.fbReadSharedJSON(sharedAlumnos, &roster)
		exists := map[string]bool{}
		for u := range roster {
			exists[strings.ToLower(u)] = true
		}
		for _, login := range a.restoreManifest.Users {
			if !exists[strings.ToLower(login)] {
				res.StudentsSkipped = append(res.StudentsSkipped, login)
				continue
			}
			if a.restoreStudent(login, sourceID, targetID, target, remap) {
				res.StudentsUpdated = append(res.StudentsUpdated, login)
			}
		}
		sort.Strings(res.StudentsUpdated)
		sort.Strings(res.StudentsSkipped)
	}
	return res, nil
}

// remapLogs returns a copy of logs re-keyed by the session remap; entries whose date has
// no target slot are dropped, and each kept entry's "date" field is updated.
func remapLogs(logs map[string]any, remap map[string]string) map[string]any {
	out := map[string]any{}
	for date, v := range logs {
		nd, ok := remap[date]
		if !ok {
			continue
		}
		if entry, ok := v.(map[string]any); ok {
			cp := map[string]any{}
			for k, val := range entry {
				cp[k] = val
			}
			cp["date"] = nd
			out[nd] = cp
		} else {
			out[nd] = v
		}
	}
	return out
}

// restoreDatedFiles copies archive files under archivePrefix (…/<oldDate>/<name>) to
// destBase/<newDate>/<name> on the server, remapping the date. Returns files written.
func (a *App) restoreDatedFiles(archivePrefix, destBase string, remap map[string]string) int {
	written := 0
	for name, content := range a.restoreFiles {
		if !strings.HasPrefix(name, archivePrefix) {
			continue
		}
		rest := strings.TrimPrefix(name, archivePrefix) // "<oldDate>/<file>"
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			continue
		}
		oldDate, file := rest[:slash], rest[slash+1:]
		newDate, ok := remap[oldDate]
		if !ok || file == "" || strings.Contains(file, "/") {
			continue
		}
		dir := destBase + "/" + newDate
		if err := a.fbEnsureWorkDir(dir); err != nil {
			continue
		}
		if err := a.fbUpload(dir+"/"+file, content); err == nil {
			written++
		}
	}
	return written
}

// restoreStudent restores one student's course data: their práctica logs for the course
// (merged into their backup under the target id), plus their work and comment files,
// remapped by session. Returns true if anything was written.
func (a *App) restoreStudent(login, sourceID, targetID string, target MateriaDef, remap map[string]string) bool {
	touched := false

	// Práctica logs: from the archived backup's source subject → target subject.
	if raw, ok := a.restoreFiles["users/"+login+"/"+fbBackupName]; ok {
		var subjects []map[string]any
		if json.Unmarshal(raw, &subjects) == nil {
			var srcSubj map[string]any
			for _, s := range subjects {
				if id, _ := s["id"].(string); id == sourceID {
					srcSubj = s
					break
				}
			}
			if srcSubj != nil {
				srcLogs, _ := srcSubj["logs"].(map[string]any)
				newSubj := map[string]any{
					"id":           targetID,
					"name":         target.Name,
					"academicYear": target.AcademicYear,
					"config":       target.Config,
					"units":        target.Units,
					"logs":         remapLogs(srcLogs, remap),
					"unitReports":  srcSubj["unitReports"],
				}
				// Merge into the student's CURRENT backup, replacing any existing target.
				cur := []map[string]any{}
				if curRaw, err := a.fbDownload(fbUsersRoot + "/" + login + "/" + fbBackupName); err == nil && strings.TrimSpace(curRaw) != "" {
					json.Unmarshal([]byte(curRaw), &cur)
				}
				merged := make([]map[string]any, 0, len(cur)+1)
				for _, s := range cur {
					if id, _ := s["id"].(string); id != targetID {
						merged = append(merged, s)
					}
				}
				merged = append(merged, newSubj)
				if out, err := json.MarshalIndent(merged, "", "  "); err == nil {
					dir := fbUsersRoot + "/" + login
					if a.fbEnsureDir(dir) == nil && a.fbUpload(dir+"/"+fbBackupName, out) == nil {
						touched = true
					}
				}
			}
		}
	}

	// Work files and comment files, remapped by date.
	if n := a.restoreDatedFiles("users/"+login+"/"+fbWorkRoot+"/"+sourceID+"/",
		fbUsersRoot+"/"+login+"/"+fbWorkRoot+"/"+targetID, remap); n > 0 {
		touched = true
	}
	if n := a.restoreDatedFiles("users/"+login+"/"+fbCommentsRoot+"/"+sourceID+"/",
		fbUsersRoot+"/"+login+"/"+fbCommentsRoot+"/"+targetID, remap); n > 0 {
		touched = true
	}
	return touched
}

// AdminListMaterias returns every course definition, for the admin panel. Admin only.
// Calls listMateriasRaw directly (not ListMaterias): that one gates on a.profile, which
// is nil in an admin-only session (see auth.go/admin.go — admin and professor/student
// are separate session kinds), so it would reject every admin call outright.
func (a *App) AdminListMaterias() ([]MateriaDef, error) {
	var out []MateriaDef
	err := a.withAdminFB(func() error {
		var e error
		out, e = a.listMateriasRaw()
		return e
	})
	return out, err
}

// AdminSaveMateria writes (creates or overwrites) a course definition with no ownership
// check, unlike the professor's own SaveMateria (profesor.go) — this is how an
// administrator edits another professor's materia, or reassigns it (m.Owner) to a
// different professor. Admin only.
func (a *App) AdminSaveMateria(m MateriaDef) error {
	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return fmt.Errorf("a materia precisa un identificador")
	}
	if strings.ContainsAny(m.ID, "/\\") {
		return fmt.Errorf("identificador de materia non válido")
	}
	return a.withAdminFB(func() error {
		return a.fbWriteSharedJSON(sharedMateriasDir+"/"+m.ID+".json", m)
	})
}

// AdminDeleteMateria removes a course definition and its task files. Admin only.
func (a *App) AdminDeleteMateria(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf("identificador de curso non válido")
	}
	return a.withAdminFB(func() error {
		if err := a.fbDelete(sharedRoot + "/" + sharedMateriasDir + "/" + id + ".json"); err != nil {
			return err
		}
		// Best-effort: drop the shared task folder too.
		a.fbDelete(sharedRoot + "/" + fbTasksRoot + "/" + id)
		return nil
	})
}

// AdminGetPrivateNote and AdminSavePrivateNote are the administrator's equivalent of
// GetPrivateNote/SavePrivateNote (notas_privadas.go), with no per-materia ownership
// check — an administrator may read/edit any professor's private note on any student.
func (a *App) AdminGetPrivateNote(username, materiaID string) (string, error) {
	username, materiaID = strings.TrimSpace(username), strings.TrimSpace(materiaID)
	if username == "" || materiaID == "" {
		return "", fmt.Errorf("indica a materia e o alumno")
	}
	var content string
	err := a.withAdminFB(func() error {
		var n notaPrivada
		if _, err := a.fbReadSharedJSON(notaPrivadaPath(materiaID, username), &n); err != nil {
			return err
		}
		content = n.Content
		return nil
	})
	return content, err
}

func (a *App) AdminSavePrivateNote(username, materiaID, markdown string) error {
	username, materiaID = strings.TrimSpace(username), strings.TrimSpace(materiaID)
	if username == "" || materiaID == "" {
		return fmt.Errorf("indica a materia e o alumno")
	}
	return a.withAdminFB(func() error {
		path := notaPrivadaPath(materiaID, username)
		if strings.TrimSpace(markdown) == "" {
			return a.fbDelete(sharedRoot + "/" + path)
		}
		n := notaPrivada{Content: markdown, UpdatedAt: time.Now().Format(time.RFC3339), UpdatedBy: "admin"}
		if err := a.fbEnsureDir(sharedRoot + "/" + sharedNotasPrivadasDir + "/" + materiaID); err != nil {
			return fmt.Errorf("non se puido preparar o cartafol de notas privadas: %w", err)
		}
		return a.fbWriteSharedJSON(path, n)
	})
}

// ---- Restore: the whole site ------------------------------------------------------
//
// Restoring a course (RestoreCourse, above) remaps everything onto ANOTHER course's
// calendar, because it is meant to reuse last year's material in a new course. A site
// restore is the opposite case: putting a server back the way it was — after a
// reinstall, a migration to another machine, or a mistake that wiped content. So it is
// literal: every file in the copy goes back to the very same path it came from, with no
// date remapping at all.
//
// It never deletes: what the copy holds is written over what is there, and anything
// created after the copy stays. That way a restore can never be the thing that loses
// data, which matters when it is run by someone trying to fix a problem in a hurry.

// SiteRestoreResult reports what a site restore wrote, for the final notice.
type SiteRestoreResult struct {
	Courses     int      `json:"courses"`     // course definitions rewritten
	SharedFiles int      `json:"sharedFiles"` // files restored under dixitalizacion/
	Users       int      `json:"users"`       // user folders touched
	UserFiles   int      `json:"userFiles"`   // files restored under users/
	Failed      []string `json:"failed"`      // paths that could not be written
}

// rutaArquivoSegura rexeita as entradas dun .zip que apunten fóra de onde deben:
// rutas absolutas, segmentos ".." ou baleiros. Unha copia é un ficheiro que pode vir
// de calquera parte (un pendrive, un correo), así que non se dá por boa a súa palabra
// sobre onde quere escribir.
func rutaArquivoSegura(nome string) bool {
	if nome == "" || strings.HasPrefix(nome, "/") || strings.Contains(nome, "\\") {
		return false
	}
	for _, seg := range strings.Split(nome, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// RestoreSite writes a whole-site copy back onto the server, exactly as it was. When
// withUsers is false only the shared part (courses, tasks, manifests, group messages)
// is restored, leaving every user folder untouched. Admin only.
func (a *App) RestoreSite(withUsers bool) (SiteRestoreResult, error) {
	var res SiteRestoreResult
	if err := a.requireAdmin(); err != nil {
		return res, err
	}
	if a.restoreFiles == nil {
		return res, fmt.Errorf("primeiro carga unha copia de seguridade")
	}
	if a.restoreManifest.Kind != "site" {
		return res, fmt.Errorf("esta copia non é de todo o sitio (só leva cursos soltos): restáuraa curso a curso")
	}
	err := a.withAdminFB(func() error {
		res = a.doRestoreSite(withUsers)
		if res.SharedFiles == 0 && res.UserFiles == 0 {
			return fmt.Errorf("non se restaurou nada: a copia está baleira ou o servidor rexeitou todas as escrituras")
		}
		return nil
	})
	return res, err
}

// doRestoreSite is the body of RestoreSite, with the administrator session already in
// place. Files are written in path order so a folder's parents are always created first.
func (a *App) doRestoreSite(withUsers bool) SiteRestoreResult {
	res := SiteRestoreResult{}
	nomes := make([]string, 0, len(a.restoreFiles))
	for nome := range a.restoreFiles {
		nomes = append(nomes, nome)
	}
	sort.Strings(nomes)

	usuarios := map[string]bool{}
	prefixoMaterias := "shared/" + sharedMateriasDir + "/"
	for _, nome := range nomes {
		if nome == backupManifestName {
			continue
		}
		var destino string
		switch {
		case strings.HasPrefix(nome, "shared/"):
			destino = sharedRoot + "/" + strings.TrimPrefix(nome, "shared/")
		case strings.HasPrefix(nome, fbUsersRoot+"/"):
			if !withUsers {
				continue
			}
			destino = nome
		default:
			continue // entrada allea ao formato: ignórase
		}
		if !rutaArquivoSegura(nome) || !rutaArquivoSegura(destino) {
			res.Failed = append(res.Failed, nome)
			continue
		}
		dir := destino[:strings.LastIndexByte(destino, '/')]
		if err := a.fbEnsureWorkDir(dir); err != nil {
			res.Failed = append(res.Failed, destino)
			continue
		}
		if err := a.fbUpload(destino, a.restoreFiles[nome]); err != nil {
			res.Failed = append(res.Failed, destino)
			continue
		}
		if strings.HasPrefix(nome, fbUsersRoot+"/") {
			res.UserFiles++
			if login := strings.SplitN(strings.TrimPrefix(nome, fbUsersRoot+"/"), "/", 2)[0]; login != "" {
				usuarios[login] = true
			}
			continue
		}
		res.SharedFiles++
		if strings.HasPrefix(nome, prefixoMaterias) && strings.HasSuffix(nome, ".json") {
			res.Courses++
		}
	}
	res.Users = len(usuarios)
	sort.Strings(res.Failed)
	return res
}

// ---- Restore: every course in the copy, at once -----------------------------------

// AllCoursesRestoreResult reports a whole-copy course restore.
type AllCoursesRestoreResult struct {
	Courses      []string `json:"courses"`      // course ids restored
	TaskFiles    int      `json:"taskFiles"`    // task files written, across all courses
	Students     []string `json:"students"`     // logins whose data was restored
	Skipped      []string `json:"skipped"`      // logins in the copy with no account now
	FailedCourse []string `json:"failedCourse"` // courses that could not be restored
}

// RestoreAllCourses restores EVERY course held in the loaded copy, each one onto
// itself: the course comes back with its own id, name, calendar and groups, creating
// it if it is no longer on the server. It is the missing middle ground between
// RestoreCourse (one course, remapped onto a DIFFERENT course's calendar, for reusing
// last year's material) and RestoreSite (the whole server, only from a site copy):
// here the Director recovers the courses of any copy — a site one or a courses one —
// without touching the roster, the professors list or the messages.
//
// Because each course is put back on its own calendar the session remap is the
// identity, so the per-course path is reused verbatim (doRestore with the same id as
// source and target) and with it all its tested handling of task files and student
// data. Admin only: creating or overwriting courses is a Director action.
func (a *App) RestoreAllCourses(withStudents bool) (AllCoursesRestoreResult, error) {
	var res AllCoursesRestoreResult
	if err := a.requireAdmin(); err != nil {
		return res, err
	}
	if a.restoreFiles == nil {
		return res, fmt.Errorf("primeiro carga unha copia de seguridade")
	}
	ids := a.cursosDaCopia()
	if len(ids) == 0 {
		return res, fmt.Errorf("a copia non contén ningún curso")
	}

	estudantes, omitidos := map[string]bool{}, map[string]bool{}
	err := a.withAdminFB(func() error {
		for _, id := range ids {
			// 1. O curso, tal cal estaba: así volve co seu nome, calendario e
			//    grupos, e créase se xa non existise no servidor.
			raw := a.restoreFiles["shared/"+sharedMateriasDir+"/"+id+".json"]
			destino := sharedRoot + "/" + sharedMateriasDir + "/" + id + ".json"
			if err := a.fbEnsureWorkDir(sharedRoot + "/" + sharedMateriasDir); err != nil {
				res.FailedCourse = append(res.FailedCourse, id)
				continue
			}
			if err := a.fbUpload(destino, raw); err != nil {
				res.FailedCourse = append(res.FailedCourse, id)
				continue
			}
			// 2. O resto (tarefas e, se se pide, os datos do alumnado) polo mesmo
			//    camiño de sempre. Orixe e destino son o mesmo curso, así que o
			//    remapeo de sesións é a identidade e nada cambia de data.
			parcial, err := a.doRestore(id, id, withStudents)
			if err != nil {
				res.FailedCourse = append(res.FailedCourse, id)
				continue
			}
			res.Courses = append(res.Courses, id)
			res.TaskFiles += parcial.TaskFiles
			for _, u := range parcial.StudentsUpdated {
				estudantes[u] = true
			}
			for _, u := range parcial.StudentsSkipped {
				omitidos[u] = true
			}
		}
		if len(res.Courses) == 0 {
			return fmt.Errorf("non se puido restaurar ningún dos %d curso(s) da copia", len(ids))
		}
		return nil
	})

	for u := range estudantes {
		res.Students = append(res.Students, u)
	}
	for u := range omitidos {
		if !estudantes[u] {
			res.Skipped = append(res.Skipped, u)
		}
	}
	sort.Strings(res.Students)
	sort.Strings(res.Skipped)
	return res, err
}

// cursosDaCopia lista os ids de curso que leva a copia cargada. Fíase do manifest
// cando o trae, e se non (copia vella ou manifest incompleto) dedúceos dos propios
// ficheiros, para que unha copia sen manifest siga sendo restaurable.
func (a *App) cursosDaCopia() []string {
	vistos := map[string]bool{}
	engadir := func(id string) {
		if id = strings.TrimSpace(id); id != "" {
			vistos[id] = true
		}
	}
	for _, id := range a.restoreManifest.Courses {
		if _, ok := a.restoreFiles["shared/"+sharedMateriasDir+"/"+id+".json"]; ok {
			engadir(id)
		}
	}
	prefixo := "shared/" + sharedMateriasDir + "/"
	for nome := range a.restoreFiles {
		if strings.HasPrefix(nome, prefixo) && strings.HasSuffix(nome, ".json") {
			engadir(strings.TrimSuffix(strings.TrimPrefix(nome, prefixo), ".json"))
		}
	}
	ids := make([]string, 0, len(vistos))
	for id := range vistos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
