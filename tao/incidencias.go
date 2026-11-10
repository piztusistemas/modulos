package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Incidents (positivo/negativo/expulsión/falta de orde).
//
// A professor may record a small disciplinary/merit event on a student for one day, in
// the materia the professor owns (requireOwnsMateria, profesor.go). Unlike the private
// notes (notas_privadas.go), incidents are DELIBERATELY visible to the student: they are
// patched straight into the student's own backup (logs[date].incidents), the exact same
// channel profesorComment already uses (SaveStudentLogComment) — so they reach the
// student on their next login/PullBackup and ride along in whatever report they build
// from that materia (ReportBuilder.tsx counts them, see reportHtml.ts).

// incidentTypes are the only accepted values for AddIncident, mirroring the frontend's
// IncidentType union (frontend/src/types.ts): positivo, negativo, expulsión, falta de
// orde. Kept as a set here so a typo from the frontend fails loudly instead of silently
// storing junk that the calendar/report code would not recognise.
var incidentTypes = map[string]bool{
	"positivo":  true,
	"negativo":  true,
	"expulsion": true,
	"falta":     true,
}

// Incident mirrors the frontend's Incident (types.ts) — the shape AddIncident returns
// once persisted, so the frontend never has to guess the server-assigned id when it
// later wants to RemoveIncident it.
type Incident struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Note      string `json:"note,omitempty"`
	CreatedAt string `json:"createdAt"`
	CreatedBy string `json:"createdBy"`
}

// novoIncidentID mints a short random id so several incidents on the same day/materia
// can be told apart (and individually removed) without relying on client-picked ids.
func novoIncidentID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "inc_" + hex.EncodeToString(b)
}

// AddIncident records one incident for username on materiaID/date and returns it as
// stored (with its server-assigned id, so the frontend can RemoveIncident it later
// without guessing). Professor only, and only for a materia the professor owns (or one
// still unclaimed) — see requireOwnsMateria. note is an optional short plain-text reason.
func (a *App) AddIncident(username, materiaID, date, tipo, note string) (Incident, error) {
	if _, err := a.requireOwnsMateria(materiaID); err != nil {
		return Incident{}, err
	}
	username = strings.TrimSpace(username)
	date = strings.TrimSpace(date)
	tipo = strings.TrimSpace(tipo)
	if username == "" || date == "" {
		return Incident{}, fmt.Errorf("faltan datos (alumno ou data)")
	}
	if !incidentTypes[tipo] {
		return Incident{}, fmt.Errorf("tipo de incidencia non válido: %q", tipo)
	}

	subjects, err := a.loadStudentSubjects(username)
	if err != nil {
		return Incident{}, err
	}
	entry := logEntryFor(subjects, materiaID, date)

	incident := Incident{
		ID:        novoIncidentID(),
		Type:      tipo,
		Note:      strings.TrimSpace(note),
		CreatedAt: time.Now().Format(time.RFC3339),
		CreatedBy: a.fbUser,
	}
	incidents, _ := entry.entry["incidents"].([]any)
	entry.entry["incidents"] = append(incidents, incident)

	if err := a.saveStudentSubjects(username, entry.subjects); err != nil {
		return Incident{}, err
	}
	return incident, nil
}

// RemoveIncident drops one incident (by id) from username's log entry for
// materiaID/date. Professor only, same ownership guard as AddIncident.
func (a *App) RemoveIncident(username, materiaID, date, incidentID string) error {
	if _, err := a.requireOwnsMateria(materiaID); err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	date = strings.TrimSpace(date)
	incidentID = strings.TrimSpace(incidentID)
	if username == "" || date == "" || incidentID == "" {
		return fmt.Errorf("faltan datos (alumno, data ou incidencia)")
	}

	subjects, err := a.loadStudentSubjects(username)
	if err != nil {
		return err
	}
	entry := logEntryFor(subjects, materiaID, date)

	incidents, _ := entry.entry["incidents"].([]any)
	kept := incidents[:0]
	for _, raw := range incidents {
		if m, ok := raw.(map[string]any); ok {
			if id, _ := m["id"].(string); id == incidentID {
				continue
			}
		}
		kept = append(kept, raw)
	}
	if len(kept) == 0 {
		delete(entry.entry, "incidents")
	} else {
		entry.entry["incidents"] = kept
	}

	return a.saveStudentSubjects(username, entry.subjects)
}
