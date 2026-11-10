package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestAddRemoveIncidentRoundTrip covers point 2: a professor records an incident on a
// student's day, it lands in the student's own backup (so it reaches them, unlike the
// private note), several incidents on the same day coexist, and RemoveIncident drops
// exactly the one asked for.
func TestAddRemoveIncidentRoundTrip(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	fake.files["dixitalizacion/materias/m1.json"] = mustJSON(t, MateriaDef{ID: "m1", Name: "Tec", Owner: "prof"})

	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	positivo, err := a.AddIncident("alumno", "m1", "2025-09-08", "positivo", "Axudou a un compañeiro")
	if err != nil {
		t.Fatalf("AddIncident (positivo): %v", err)
	}
	if positivo.ID == "" || positivo.CreatedBy != "prof" {
		t.Fatalf("incidencia devolta inesperada: %+v", positivo)
	}
	negativo, err := a.AddIncident("alumno", "m1", "2025-09-08", "negativo", "")
	if err != nil {
		t.Fatalf("AddIncident (negativo): %v", err)
	}

	raw, err := a.GetStudentBackup("alumno")
	if err != nil {
		t.Fatalf("GetStudentBackup: %v", err)
	}
	var subjects []map[string]any
	if err := json.Unmarshal([]byte(raw), &subjects); err != nil {
		t.Fatalf("backup non válido: %v", err)
	}
	entry := subjects[0]["logs"].(map[string]any)["2025-09-08"].(map[string]any)
	incidents := entry["incidents"].([]any)
	if len(incidents) != 2 {
		t.Fatalf("esperábanse 2 incidencias, obtívose %d: %+v", len(incidents), incidents)
	}

	// Reject an invalid type outright, without touching the backup.
	if _, err := a.AddIncident("alumno", "m1", "2025-09-08", "sospeitoso", ""); err == nil {
		t.Fatal("esperábase erro: tipo de incidencia non válido")
	}

	if err := a.RemoveIncident("alumno", "m1", "2025-09-08", positivo.ID); err != nil {
		t.Fatalf("RemoveIncident: %v", err)
	}
	raw, _ = a.GetStudentBackup("alumno")
	json.Unmarshal([]byte(raw), &subjects)
	entry = subjects[0]["logs"].(map[string]any)["2025-09-08"].(map[string]any)
	incidents = entry["incidents"].([]any)
	if len(incidents) != 1 {
		t.Fatalf("esperábase 1 incidencia despois de eliminar, obtívose %d", len(incidents))
	}
	kept := incidents[0].(map[string]any)
	if kept["id"] != negativo.ID {
		t.Fatalf("quedou a incidencia incorrecta: %+v", kept)
	}

	// A professor who does not own the materia cannot touch its incidents.
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof", "outro"})
	srv2 := httptest.NewServer(fake.handler("outro", "segredo"))
	defer srv2.Close()
	outro := NewApp()
	outro.dataDir = t.TempDir()
	if _, err := outro.loginServer("outro", "segredo", srv2.URL); err != nil {
		t.Fatalf("loginServer outro: %v", err)
	}
	if _, err := outro.AddIncident("alumno", "m1", "2025-09-09", "positivo", ""); err == nil {
		t.Fatal("esperábase erro: outro profesor non pode engadir incidencias a unha materia allea")
	}
}
