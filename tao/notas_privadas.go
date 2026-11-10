package main

import (
	"fmt"
	"strings"
	"time"
)

// Private teacher notes on a student, per materia.
//
// Unlike incidents (incidencias.go) and profesorComment (profesor.go), a private note
// is NEVER visible to the student and never travels through the student's own backup
// (fbBackupName) — that file is downloadable by the student's own File Browser account,
// which would defeat the point. Instead it lives in its own file under the SHARED
// folder, keyed by materia AND student:
//
//	dixitalizacion/notas-privadas/<materiaID>/<username>.json
//
// so the ownership check is the same one already guarding everything else materia-
// scoped (requireOwnsMateria, profesor.go): only the professor who owns materiaID (or
// an administrator, via Admin* below) may read or write it. This structural separation
// from the student's backup is what keeps GetStudentBackup/PullBackup/ListMaterias/
// GetMyMaterias — every path the student's own session can reach — from ever exposing
// it, regardless of any bug in those unrelated functions.
//
// Confidentiality also has to hold at the File Browser layer itself: every account
// (student or professor) is created with the same scope="." and empty rules
// (fbCreateUser, admin_fb.go), so a student could otherwise read this path directly
// from the File Browser API, bypassing Tao entirely. fbCreateUser denies student
// accounts read access to this path via a File Browser access rule — see privacidade.go.

const sharedNotasPrivadasDir = "notas-privadas"

// notasPrivadasRulePath is the path of the File Browser access rule (see privacidade.go)
// that keeps student accounts from reading notas-privadas/ directly through the File
// Browser API, bypassing requireOwnsMateria entirely. Every account is scoped to the
// whole source with no path restrictions otherwise (fbCreateUser) — this is the one
// folder that must never be readable by an alumno account, however it is reached. It is
// Quantum's form (a plain path); the classic File Browser takes the same restriction as a
// regex, notasPrivadasRuleRegex (see privacidade_clasico.go).
const notasPrivadasRulePath = "/" + sharedRoot + "/" + sharedNotasPrivadasDir

// notasPrivadasRuleRegex is notasPrivadasRulePath as the classic File Browser's rule
// path: a regex matched against the request path inside the account's scope.
const notasPrivadasRuleRegex = "^/?" + sharedRoot + "/" + sharedNotasPrivadasDir + "(/|$)"

// notaPrivada is what is stored in notas-privadas/<materiaID>/<username>.json.
type notaPrivada struct {
	Content   string `json:"content"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	UpdatedBy string `json:"updatedBy,omitempty"`
}

func notaPrivadaPath(materiaID, username string) string {
	return sharedNotasPrivadasDir + "/" + materiaID + "/" + username + ".json"
}

// GetPrivateNote returns the professor's private markdown note on username for
// materiaID ("" if none yet). Professor only, and only for a materia they own.
func (a *App) GetPrivateNote(username, materiaID string) (string, error) {
	if _, err := a.requireOwnsMateria(materiaID); err != nil {
		return "", err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return "", fmt.Errorf("indica o alumno")
	}
	var n notaPrivada
	if _, err := a.fbReadSharedJSON(notaPrivadaPath(materiaID, username), &n); err != nil {
		return "", err
	}
	return n.Content, nil
}

// SavePrivateNote replaces the professor's private markdown note on username for
// materiaID. An empty (after trimming) note deletes the file rather than leaving an
// empty one behind. Professor only, and only for a materia they own.
func (a *App) SavePrivateNote(username, materiaID, markdown string) error {
	if _, err := a.requireOwnsMateria(materiaID); err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("indica o alumno")
	}
	path := notaPrivadaPath(materiaID, username)
	if strings.TrimSpace(markdown) == "" {
		return a.fbDelete(sharedRoot + "/" + path)
	}
	n := notaPrivada{
		Content:   markdown,
		UpdatedAt: time.Now().Format(time.RFC3339),
		UpdatedBy: a.fbUser,
	}
	if err := a.fbEnsureDir(sharedRoot + "/" + sharedNotasPrivadasDir + "/" + materiaID); err != nil {
		return fmt.Errorf("non se puido preparar o cartafol de notas privadas: %w", err)
	}
	return a.fbWriteSharedJSON(path, n)
}
