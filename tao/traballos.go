package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Source files of the student's work ("traballos").
//
// Besides writing the daily practice (the log kept in their backup), a student can
// attach the source files they produced in that session — the Fritzing sketch, the
// ODT report, the .ino program... Each file is stored in the student's own folder on
// the File Browser server, filed by subject and session date:
//
//	users/<login>/traballos/<materia>/<YYYY-MM-DD>/<ficheiro>
//
// so the attachments of one session never mix with another's and travel with the
// account, not the machine. The student uploads, downloads and removes their own
// files; a professor reviewing that student may list and download them, but not
// modify them — the work is the student's.

// maxWorkFileBytes caps a single upload. The File Browser HTTP client works with a
// 30 s timeout, and a source file for a class practice is far smaller than this; the
// limit turns a silent timeout into a clear message.
const maxWorkFileBytes = 50 << 20 // 50 MiB

// WorkFile is one attached source file, as listed for the frontend. Checksum is only
// filled in by listWorkFilesChecksummed (see below), never by the plain listing used
// by the file-browsing UI — computing it means downloading the whole file.
type WorkFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum,omitempty"`
}

// ---- Path building and validation -----------------------------------------------

// workSegment validates a single path segment (a materia id, a username) that is
// about to be interpolated into a server path, rejecting anything that could escape
// the intended folder.
func workSegment(s, what string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("falta %s", what)
	}
	if strings.ContainsAny(s, `/\`) || strings.Contains(s, "..") {
		return "", fmt.Errorf("%s non válido", what)
	}
	return s, nil
}

// workDate validates the session date, which names the folder of the day.
func workDate(date string) (string, error) {
	date = strings.TrimSpace(date)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", fmt.Errorf("data da sesión non válida")
	}
	return date, nil
}

// workFileName validates a file name coming back from the frontend (it must address a
// file inside the session folder, never a path).
func workFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("falta o nome do ficheiro")
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("nome de ficheiro non válido")
	}
	return name, nil
}

// sessionDir returns the server folder holding username's files for a session under a
// given root (fbWorkRoot for the student's own work, fbMaterialsRoot for the example
// files the professor sends), after validating every segment.
func sessionDir(root, username, materiaID, date string) (string, error) {
	user, err := workSegment(username, "o usuario")
	if err != nil {
		return "", err
	}
	materia, err := workSegment(materiaID, "a materia")
	if err != nil {
		return "", err
	}
	day, err := workDate(date)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{fbUsersRoot, user, root, materia, day}, "/"), nil
}

// workDir is sessionDir for the student's own work folder.
func workDir(username, materiaID, date string) (string, error) {
	return sessionDir(fbWorkRoot, username, materiaID, date)
}

// ---- Shared implementation (owner-agnostic) --------------------------------------

// listFilesInDir returns the files directly in dir (no path building). A folder that
// does not exist yet simply has no files.
func (a *App) listFilesInDir(dir string) ([]WorkFile, error) {
	items, err := a.fbList(dir)
	if err != nil {
		return []WorkFile{}, nil // no folder yet: no attachments
	}
	out := make([]WorkFile, 0, len(items))
	for _, it := range items {
		if it.IsDir {
			continue
		}
		out = append(out, WorkFile{Name: it.Name, Size: it.Size})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// listFilesIn returns the files in one session folder of username under root. A folder
// that does not exist yet simply has no files.
func (a *App) listFilesIn(root, username, materiaID, date string) ([]WorkFile, error) {
	dir, err := sessionDir(root, username, materiaID, date)
	if err != nil {
		return nil, err
	}
	return a.listFilesInDir(dir)
}

// listWorkFiles returns the files the student attached to one session (fbWorkRoot).
func (a *App) listWorkFiles(username, materiaID, date string) ([]WorkFile, error) {
	return a.listFilesIn(fbWorkRoot, username, materiaID, date)
}

// listWorkFileDates returns the session dates (folder names, "YYYY-MM-DD") that have at
// least one attached work file under username's traballos/<materiaID> — one listing for
// the whole materia, instead of one ListWorkFiles call per calendar day, so the calendar
// can mark "this session has attachments" (App.tsx, the black underline under a day)
// without a request per day. A date folder that exists is treated as "has files" even if
// every file inside it was later deleted (DeleteWorkFile does not remove the now-empty
// date folder) — a rare, harmless over-report, not worth a second listing per date to
// rule out.
func (a *App) listWorkFileDates(username, materiaID string) ([]string, error) {
	user, err := workSegment(username, "o usuario")
	if err != nil {
		return nil, err
	}
	materia, err := workSegment(materiaID, "a materia")
	if err != nil {
		return nil, err
	}
	dir := strings.Join([]string{fbUsersRoot, user, fbWorkRoot, materia}, "/")
	items, err := a.fbList(dir)
	if err != nil {
		return []string{}, nil // no folder yet: no sessions with files
	}
	dates := make([]string, 0, len(items))
	for _, it := range items {
		if it.IsDir {
			dates = append(dates, it.Name)
		}
	}
	return dates, nil
}

// fileChecksum returns the SHA-256 checksum of content, as lowercase hex.
func fileChecksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// listWorkFilesChecksummed is listWorkFiles with each file's SHA-256 checksum filled
// in — it downloads every file's content to compute it, so it is only used when
// building the signed report (ListWorkFilesForReport), never by the regular
// file-browsing UI, which polls far too often for that cost. A file whose content
// cannot be re-downloaded is still listed, just without a checksum (best effort: the
// report should not fail to include a session over one unreadable file).
func (a *App) listWorkFilesChecksummed(username, materiaID, date string) ([]WorkFile, error) {
	dir, err := workDir(username, materiaID, date)
	if err != nil {
		return nil, err
	}
	files, err := a.listFilesInDir(dir)
	if err != nil {
		return nil, err
	}
	for i := range files {
		content, err := a.fbRawGet(dir + "/" + files[i].Name)
		if err != nil {
			continue
		}
		files[i].Checksum = fileChecksum(content)
	}
	return files, nil
}

// downloadNamedFile asks where to save the file named name inside dir and writes it
// there. It returns the destination path, or "" when the user cancels the dialog.
func (a *App) downloadNamedFile(dir, name string) (string, error) {
	file, err := workFileName(name)
	if err != nil {
		return "", err
	}
	content, err := a.fbRawGet(dir + "/" + file)
	if err != nil {
		return "", fmt.Errorf("non se puido descargar %q do servidor: %w", file, err)
	}
	dest, err := gardarFicheiro("Gardar o ficheiro", file)
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // user cancelled
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return "", fmt.Errorf("non se puido gardar o ficheiro: %w", err)
	}
	return dest, nil
}

// downloadFileIn saves one session file of username under root, asking where.
func (a *App) downloadFileIn(root, username, materiaID, date, name string) (string, error) {
	dir, err := sessionDir(root, username, materiaID, date)
	if err != nil {
		return "", err
	}
	return a.downloadNamedFile(dir, name)
}

// downloadWorkFile saves one of username's work attachments (fbWorkRoot).
func (a *App) downloadWorkFile(username, materiaID, date, name string) (string, error) {
	return a.downloadFileIn(fbWorkRoot, username, materiaID, date, name)
}

// uploadLocalFiles reads each local path and uploads it into dir on the server,
// enforcing the per-file size cap and overwriting a file of the same name. It returns
// the names actually uploaded. Shared by the student's work upload and the professor's
// example-file upload.
func (a *App) uploadLocalFiles(dir string, paths []string) ([]string, error) {
	uploaded := make([]string, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return uploaded, fmt.Errorf("non se puido ler %q: %w", filepath.Base(p), err)
		}
		if fi.Size() > maxWorkFileBytes {
			return uploaded, fmt.Errorf("%q é grande de máis (%d MB); o límite por ficheiro é de %d MB",
				filepath.Base(p), fi.Size()>>20, maxWorkFileBytes>>20)
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return uploaded, fmt.Errorf("non se puido ler %q: %w", filepath.Base(p), err)
		}
		name := filepath.Base(p)
		if err := a.fbUpload(dir+"/"+name, content); err != nil {
			return uploaded, fmt.Errorf("non se puido subir %q: %w", name, err)
		}
		uploaded = append(uploaded, name)
	}
	return uploaded, nil
}

// ---- Student methods (own files) -------------------------------------------------

// requireStudentSession guards the student-owned file operations.
func (a *App) requireStudentSession() error {
	if a.profile == nil || !a.profile.Connected {
		return fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	return nil
}

// ListWorkFiles returns the source files the logged-in student attached to a session.
func (a *App) ListWorkFiles(materiaID, date string) ([]WorkFile, error) {
	if err := a.requireStudentSession(); err != nil {
		return nil, err
	}
	return a.listWorkFiles(a.fbUser, materiaID, date)
}

// ListWorkFileDates returns the session dates for which the logged-in student has at
// least one uploaded work file in materiaID — see listWorkFileDates.
func (a *App) ListWorkFileDates(materiaID string) ([]string, error) {
	if err := a.requireStudentSession(); err != nil {
		return nil, err
	}
	return a.listWorkFileDates(a.fbUser, materiaID)
}

// ListWorkFilesForReport returns the logged-in student's session files together with
// their SHA-256 checksums, for the signed report (ReportBuilder). It lets the
// professor spot verbatim file reuse between students by comparing checksums across
// reports — this app does not do that comparison itself.
func (a *App) ListWorkFilesForReport(materiaID, date string) ([]WorkFile, error) {
	if err := a.requireStudentSession(); err != nil {
		return nil, err
	}
	return a.listWorkFilesChecksummed(a.fbUser, materiaID, date)
}

// UploadWorkFiles opens a file picker and uploads the chosen source files to the
// logged-in student's folder for the given session, overwriting a file of the same
// name. It returns the names actually uploaded (empty when the picker is cancelled).
func (a *App) UploadWorkFiles(materiaID, date string) ([]string, error) {
	if err := a.requireStudentSession(); err != nil {
		return nil, err
	}
	dir, err := workDir(a.fbUser, materiaID, date)
	if err != nil {
		return nil, err
	}

	paths, err := escollerFicheiros("Escolle os ficheiros fonte do teu traballo")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []string{}, nil // user cancelled
	}

	// Create the whole chain (traballos/<materia>/<data>): File Browser's mkdir does
	// not create missing parents.
	if err := a.fbEnsureWorkDir(dir); err != nil {
		return nil, fmt.Errorf("non se puido preparar o cartafol no servidor: %w", err)
	}

	uploaded, err := a.uploadLocalFiles(dir, paths)
	if err != nil {
		return uploaded, err
	}
	log.Printf("[traballos] %q subiu %d ficheiro(s) a %s", a.fbUser, len(uploaded), dir)
	return uploaded, nil
}

// DownloadWorkFile saves one of the logged-in student's attachments to disk, asking
// where. Returns the destination path, or "" when cancelled.
func (a *App) DownloadWorkFile(materiaID, date, name string) (string, error) {
	if err := a.requireStudentSession(); err != nil {
		return "", err
	}
	return a.downloadWorkFile(a.fbUser, materiaID, date, name)
}

// DeleteWorkFile removes one of the logged-in student's attachments from the server.
func (a *App) DeleteWorkFile(materiaID, date, name string) error {
	if err := a.requireStudentSession(); err != nil {
		return err
	}
	dir, err := workDir(a.fbUser, materiaID, date)
	if err != nil {
		return err
	}
	file, err := workFileName(name)
	if err != nil {
		return err
	}
	return a.fbDelete(dir + "/" + file)
}

// fbEnsureWorkDir creates dir and every missing parent below the student's folder,
// since File Browser's mkdir only creates the last segment.
func (a *App) fbEnsureWorkDir(dir string) error {
	segments := strings.Split(strings.Trim(dir, "/"), "/")
	for i := range segments {
		if err := a.fbEnsureDir(strings.Join(segments[:i+1], "/")); err != nil {
			return err
		}
	}
	return nil
}

// ---- Professor methods (a student's files, read-only) ----------------------------

// ListStudentWorkFiles returns the source files a student attached to a session, so
// the professor can review them. Professor only.
func (a *App) ListStudentWorkFiles(username, materiaID, date string) ([]WorkFile, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	return a.listWorkFiles(username, materiaID, date)
}

// ListStudentWorkFileDates is ListWorkFileDates for a student the professor is
// reviewing. Professor only.
func (a *App) ListStudentWorkFileDates(username, materiaID string) ([]string, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	return a.listWorkFileDates(username, materiaID)
}

// DownloadStudentWorkFile saves a student's attachment to the professor's disk,
// asking where. Returns the destination path, or "" when cancelled. Professor only.
func (a *App) DownloadStudentWorkFile(username, materiaID, date, name string) (string, error) {
	if err := a.requireProfessor(); err != nil {
		return "", err
	}
	return a.downloadWorkFile(username, materiaID, date, name)
}

// ---- Comment clarifying files (professor → one student, per session) --------------
//
// Files the professor attaches to their feedback on a student's práctica. They live in
// the student's own space, keyed by materia and date
// (users/<login>/comentarios/<materia>/<date>/). The student may list and download them;
// only the professor uploads or removes them.

// UploadCommentFile opens a file picker and uploads the chosen clarifying files to a
// student's comment folder for a session. Returns the names uploaded. Professor only.
func (a *App) UploadCommentFile(username, materiaID, date string) ([]string, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	dir, err := sessionDir(fbCommentsRoot, username, materiaID, date)
	if err != nil {
		return nil, err
	}
	paths, err := escollerFicheiros("Escolle o(s) ficheiro(s) aclaratorio(s) para o alumno/a")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []string{}, nil // user cancelled
	}
	if err := a.fbEnsureWorkDir(dir); err != nil {
		return nil, fmt.Errorf("non se puido preparar o cartafol no servidor: %w", err)
	}
	uploaded, err := a.uploadLocalFiles(dir, paths)
	if err != nil {
		return uploaded, err
	}
	log.Printf("[comentarios] %q subiu %d ficheiro(s) aclaratorio(s) a %s", a.fbUser, len(uploaded), dir)
	return uploaded, nil
}

// DeleteCommentFile removes a clarifying file from a student's comment folder. Professor only.
func (a *App) DeleteCommentFile(username, materiaID, date, name string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	dir, err := sessionDir(fbCommentsRoot, username, materiaID, date)
	if err != nil {
		return err
	}
	file, err := workFileName(name)
	if err != nil {
		return err
	}
	return a.fbDelete(dir + "/" + file)
}

// ListStudentCommentFiles returns the clarifying files the professor attached to a
// student's session (professor view). Professor only.
func (a *App) ListStudentCommentFiles(username, materiaID, date string) ([]WorkFile, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	return a.listFilesIn(fbCommentsRoot, username, materiaID, date)
}

// DownloadStudentCommentFile saves one of a student's clarifying files onto the
// professor's disk. Professor only.
func (a *App) DownloadStudentCommentFile(username, materiaID, date, name string) (string, error) {
	if err := a.requireProfessor(); err != nil {
		return "", err
	}
	return a.downloadFileIn(fbCommentsRoot, username, materiaID, date, name)
}

// ListCommentFiles returns the clarifying files the professor attached to the logged-in
// student's session.
func (a *App) ListCommentFiles(materiaID, date string) ([]WorkFile, error) {
	if err := a.requireStudentSession(); err != nil {
		return nil, err
	}
	return a.listFilesIn(fbCommentsRoot, a.fbUser, materiaID, date)
}

// DownloadCommentFile saves one clarifying file onto the logged-in student's disk.
func (a *App) DownloadCommentFile(materiaID, date, name string) (string, error) {
	if err := a.requireStudentSession(); err != nil {
		return "", err
	}
	return a.downloadFileIn(fbCommentsRoot, a.fbUser, materiaID, date, name)
}

// ---- Task files (professor → whole materia, per session) --------------------------
//
// A simple Moodle-like assignment: the professor attaches a file to a class session and
// every student of the materia sees it. It lives in the SHARED folder, keyed by materia
// and date (dixitalizacion/tarefas/<materia>/<date>/), so a single copy serves the whole
// class. Any student may list and download it; only the professor uploads or removes it.

// taskDir returns the shared folder that holds a materia's task files for a session.
func taskDir(materiaID, date string) (string, error) {
	materia, err := workSegment(materiaID, "a materia")
	if err != nil {
		return "", err
	}
	day, err := workDate(date)
	if err != nil {
		return "", err
	}
	return sharedRoot + "/" + fbTasksRoot + "/" + materia + "/" + day, nil
}

// UploadTaskFiles opens a file picker and uploads the chosen files as the session's task
// for the whole materia. Returns the names uploaded (empty when cancelled). Professor only.
func (a *App) UploadTaskFiles(materiaID, date string) ([]string, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	dir, err := taskDir(materiaID, date)
	if err != nil {
		return nil, err
	}
	paths, err := escollerFicheiros("Escolle o(s) ficheiro(s) da tarefa para o alumnado")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []string{}, nil // user cancelled
	}
	if err := a.fbEnsureWorkDir(dir); err != nil {
		return nil, fmt.Errorf("non se puido preparar o cartafol no servidor: %w", err)
	}
	uploaded, err := a.uploadLocalFiles(dir, paths)
	if err != nil {
		return uploaded, err
	}
	log.Printf("[tarefas] %q subiu %d ficheiro(s) de tarefa a %s", a.fbUser, len(uploaded), dir)
	return uploaded, nil
}

// DeleteTaskFile removes a session's task file for the materia. Professor only.
func (a *App) DeleteTaskFile(materiaID, date, name string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	dir, err := taskDir(materiaID, date)
	if err != nil {
		return err
	}
	file, err := workFileName(name)
	if err != nil {
		return err
	}
	return a.fbDelete(dir + "/" + file)
}

// ListTaskFiles returns the task files the professor attached to a session of a materia.
// Any connected account (student or professor) may read them.
func (a *App) ListTaskFiles(materiaID, date string) ([]WorkFile, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	dir, err := taskDir(materiaID, date)
	if err != nil {
		return nil, err
	}
	return a.listFilesInDir(dir)
}

// DownloadTaskFile saves one of a session's task files to disk, asking where. Any
// connected account may download it. Returns the destination path, or "" when cancelled.
func (a *App) DownloadTaskFile(materiaID, date, name string) (string, error) {
	if a.profile == nil || !a.profile.Connected {
		return "", fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	dir, err := taskDir(materiaID, date)
	if err != nil {
		return "", err
	}
	return a.downloadNamedFile(dir, name)
}

// ---- Task description (professor → whole materia OR one student, per session) ----
//
// The written explanation of a session's task (what the professor edits in the
// "Tarefa" tab alongside the day's práctica, next to the task files above): plain
// Markdown text, kept in its OWN shared subfolder
// (dixitalizacion/tarefas-descricions/<materia>/<date>/) rather than inside taskDir
// — so it never shows up mixed into ListTaskFiles' listing of uploadable files.
//
// Since docs/api-tao.md §11, this is a GENERAL file (xeral.md) plus optional
// PER-STUDENT overrides (alumnos/<username>.md) — one mechanism covers "same for
// everyone" (only xeral.md), "same with exceptions" (xeral.md + a few overrides) and
// "individualised for all" (only overrides, no xeral.md), see §11.3. A subfolder was
// chosen over a "<date>__<username>.md" suffix so listing who has an override
// (ListTaskOverrides) is a plain fbList of alumnos/, no filename parsing.
//
// Any connected account may READ (GetTaskDescription resolves the caller's own
// override transparently, see below); only the professor writes, on either file.

// taskDescDir returns the shared per-session folder holding xeral.md and alumnos/.
func taskDescDir(materiaID, date string) (string, error) {
	materia, err := workSegment(materiaID, "a materia")
	if err != nil {
		return "", err
	}
	day, err := workDate(date)
	if err != nil {
		return "", err
	}
	return sharedRoot + "/" + fbTaskDescRoot + "/" + materia + "/" + day, nil
}

// taskDescXeralPath returns the general explanation file for a session.
func taskDescXeralPath(materiaID, date string) (string, error) {
	dir, err := taskDescDir(materiaID, date)
	if err != nil {
		return "", err
	}
	return dir + "/xeral.md", nil
}

// taskDescAlumnoPath returns one student's override file for a session.
func taskDescAlumnoPath(materiaID, date, username string) (string, error) {
	dir, err := taskDescDir(materiaID, date)
	if err != nil {
		return "", err
	}
	user, err := workSegment(username, "o usuario")
	if err != nil {
		return "", err
	}
	return dir + "/alumnos/" + user + ".md", nil
}

// taskDescLegacyPath is the flat single-file scheme this replaced
// (dixitalizacion/tarefas-descricions/<materia>/<date>.md, one file, no per-student
// concept) — GetTaskDescription still reads it as a fallback so sessions written
// before §11.3 keep working with no migration step; never written to again (the
// first SaveTaskDescription on that session writes xeral.md instead, and from then
// on the legacy file is simply ignored, left orphaned rather than moved — safer
// than an active move that could lose data on a failed write).
func taskDescLegacyPath(materiaID, date string) (string, error) {
	materia, err := workSegment(materiaID, "a materia")
	if err != nil {
		return "", err
	}
	day, err := workDate(date)
	if err != nil {
		return "", err
	}
	return sharedRoot + "/" + fbTaskDescRoot + "/" + materia + "/" + day + ".md", nil
}

// writeTaskDescFile is the shared write path for both xeral.md and an alumnos/
// override: blank text deletes (best effort — nada que explicar, non hai por que
// fallar se xa non existía), non-blank text ensures parentDir exists and uploads.
func (a *App) writeTaskDescFile(parentDir, path, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		_ = a.fbDelete(path)
		return nil
	}
	if err := a.fbEnsureWorkDir(parentDir); err != nil {
		return fmt.Errorf("non se puido preparar o cartafol da explicación da tarefa: %w", err)
	}
	return a.fbUpload(path, []byte(text))
}

// SaveTaskDescription sets (or, when text is blank, clears) the GENERAL task
// explanation for a session — what every student sees unless they have their own
// override (SaveTaskDescriptionAlumno). Professor only.
func (a *App) SaveTaskDescription(materiaID, date, text string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	dir, err := taskDescDir(materiaID, date)
	if err != nil {
		return err
	}
	path, err := taskDescXeralPath(materiaID, date)
	if err != nil {
		return err
	}
	return a.writeTaskDescFile(dir, path, text)
}

// SaveTaskDescriptionAlumno sets (or, when text is blank, clears) one student's
// OWN override of the task explanation for a session — docs/api-tao.md §11.3/§11.4.
// Professor only. Blank text deletes the override, reverting that student to
// whatever SaveTaskDescription last wrote (or to nothing, if it never wrote either).
func (a *App) SaveTaskDescriptionAlumno(materiaID, date, username, text string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	dir, err := taskDescDir(materiaID, date)
	if err != nil {
		return err
	}
	path, err := taskDescAlumnoPath(materiaID, date, username)
	if err != nil {
		return err
	}
	return a.writeTaskDescFile(dir+"/alumnos", path, text)
}

// ListTaskOverrides returns the usernames that have their own override of a
// session's task explanation — so the "Tarefa" tab can warn the professor ("N
// alumnos teñen unha tarefa distinta desta") before an edit to the general text
// gets mistaken for "changed for everyone". Professor only.
func (a *App) ListTaskOverrides(materiaID, date string) ([]string, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	dir, err := taskDescDir(materiaID, date)
	if err != nil {
		return nil, err
	}
	items, err := a.fbList(dir + "/alumnos")
	if err != nil {
		return []string{}, nil // sen cartafol aínda: sen excepcións
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.IsDir || !strings.HasSuffix(strings.ToLower(it.Name), ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(it.Name, ".md"))
	}
	sort.Strings(out)
	return out, nil
}

// DeleteTaskOverride removes one student's override, reverting them to the general
// explanation. Professor only.
func (a *App) DeleteTaskOverride(materiaID, date, username string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	path, err := taskDescAlumnoPath(materiaID, date, username)
	if err != nil {
		return err
	}
	return a.fbDelete(path)
}

// DeleteAllTaskOverrides clears every per-student override for a session, reverting
// it to "same for everyone" (case a of docs/api-tao.md §11.1). Professor only.
func (a *App) DeleteAllTaskOverrides(materiaID, date string) error {
	usernames, err := a.ListTaskOverrides(materiaID, date)
	if err != nil {
		return err
	}
	for _, username := range usernames {
		if err := a.DeleteTaskOverride(materiaID, date, username); err != nil {
			return fmt.Errorf("bórranse algunhas pero fallou en %s: %w", username, err)
		}
	}
	return nil
}

// GetTaskDescription returns the task's written explanation for a session, as seen
// by the LOGGED-IN account: their own override if the professor set one for them
// (alumnos/<a.profile.Username>.md), otherwise the general one (xeral.md), otherwise
// "" if neither exists. No username parameter on purpose (docs/api-tao.md §11.4): a
// student can never fetch another student's override by passing a different value,
// because there is no value to pass — the lookup key is always this process' own
// a.profile.Username. Falls back to the pre-§11.3 flat file, read-only (see
// taskDescLegacyPath). Any connected account may call this.
func (a *App) GetTaskDescription(materiaID, date string) (string, error) {
	if a.profile == nil || !a.profile.Connected {
		return "", fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	if username := strings.TrimSpace(a.profile.Username); username != "" {
		if alumnoPath, err := taskDescAlumnoPath(materiaID, date, username); err == nil {
			if content, err := a.fbRawGet(alumnoPath); err == nil {
				return string(content), nil
			}
		}
	}
	path, err := taskDescXeralPath(materiaID, date)
	if err != nil {
		return "", err
	}
	if content, err := a.fbRawGet(path); err == nil {
		return string(content), nil
	}
	legacy, err := taskDescLegacyPath(materiaID, date)
	if err != nil {
		return "", nil
	}
	content, err := a.fbRawGet(legacy)
	if err != nil {
		return "", nil // sen explicación aínda (nin nova nin vella): trátase coma baleiro
	}
	return string(content), nil
}

// GetTaskDescriptionAlumno returns ONE student's override text, for the professor to
// review/edit it (App.tsx modal de excepcións — "cambiarlle a súa tarefa individual").
// Unlike GetTaskDescription (which resolves the CALLER's own override) this takes an
// explicit username and is professor-only; "" with no error if that student has no
// override (nothing to edit yet — the caller decides what to show for that case).
func (a *App) GetTaskDescriptionAlumno(materiaID, date, username string) (string, error) {
	if err := a.requireProfessor(); err != nil {
		return "", err
	}
	path, err := taskDescAlumnoPath(materiaID, date, username)
	if err != nil {
		return "", err
	}
	content, err := a.fbRawGet(path)
	if err != nil {
		return "", nil // sen override: caixa baleira para empezar de cero
	}
	return string(content), nil
}
