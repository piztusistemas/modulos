package main

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClassDaySequenceSkipsWeekendsAndHolidays(t *testing.T) {
	// Mondays only, over two weeks, with the middle Monday marked non-lective.
	config := map[string]any{
		"startDate":    "2025-09-01",
		"endDate":      "2025-09-15",
		"scheduleDays": []any{float64(1)}, // Monday
		"calendar":     []any{map[string]any{"start": "2025-09-08", "name": "Festivo"}},
	}
	seq, err := classDaySequence(config)
	if err != nil {
		t.Fatalf("classDaySequence: %v", err)
	}
	want := []string{"2025-09-01", "2025-09-15"}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("secuencia = %v, quere %v", seq, want)
	}
}

func TestRemapBySessionMapsByOrdinal(t *testing.T) {
	src := []string{"a1", "a2", "a3"}
	tgt := []string{"b1", "b2"}
	got := remapBySession(src, tgt)
	if len(got) != 2 || got["a1"] != "b1" || got["a2"] != "b2" {
		t.Fatalf("remap inesperado: %v", got)
	}
	if _, ok := got["a3"]; ok {
		t.Fatal("a3 non debería mapearse (o destino ten menos sesións)")
	}
}

// mondaysConfig builds a config with class days on Mondays only.
func mondaysConfig(start, end string) map[string]any {
	return map[string]any{"startDate": start, "endDate": end, "scheduleDays": []any{float64(1)}}
}

func TestRestoreCourseRemapsSessions(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana": {Group: "eso4a"}, // exists → restored
		// "bob" intentionally absent → skipped
	})
	// Target course already created, with the NEW calendar (Mondays of Sept 2025).
	fake.files["dixitalizacion/materias/dest.json"] = mustJSON(t, MateriaDef{
		ID: "dest", Name: "Tec", AcademicYear: "2025/2026",
		Config: mondaysConfig("2025-09-01", "2025-09-15"),
	})

	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()
	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	// The archive: source course (Mondays of Sept 2024), a task on session 3, and ana's
	// práctica + work file on session 2.
	srcMateria := mustJSON(t, MateriaDef{
		ID: "src", Name: "Tec (2024)", AcademicYear: "2024/2025",
		Config: mondaysConfig("2024-09-02", "2024-09-16"),
		Units:  []any{map[string]any{"id": float64(1), "title": "Debuxo", "sessions": float64(3)}},
		Logs:   map[string]any{"2024-09-09": map[string]any{"date": "2024-09-09", "content": "sesión 2", "status": "lectivo"}},
	})
	a.restoreFiles = map[string][]byte{
		"shared/materias/src.json":                       srcMateria,
		"shared/tarefas/src/2024-09-16/enunciado.pdf":    []byte("%PDF"),
		"users/ana/dixitalizacion-backup.json":           []byte(`[{"id":"src","logs":{"2024-09-09":{"date":"2024-09-09","content":"traballo ana","status":"lectivo"}}}]`),
		"users/ana/traballos/src/2024-09-09/memoria.odt": []byte("odt"),
	}
	a.restoreManifest = backupManifest{Kind: "professor", Courses: []string{"src"}, Users: []string{"ana", "bob"}}

	res, err := a.RestoreCourse("src", "dest", true)
	if err != nil {
		t.Fatalf("RestoreCourse: %v", err)
	}

	// Session ordinal: src sessions [09-02, 09-09, 09-16] → dest [09-01, 09-08, 09-15].
	// The professor's log from session 2 (09-09) lands on the target's session 2 (09-08).
	var dest MateriaDef
	if _, err := a.fbReadSharedJSON("materias/dest.json", &dest); err != nil {
		t.Fatalf("ler destino: %v", err)
	}
	entry, ok := dest.Logs["2025-09-08"].(map[string]any)
	if !ok || entry["content"] != "sesión 2" {
		t.Fatalf("o log non se remapou á sesión 2 do destino: %+v", dest.Logs)
	}
	if len(dest.Units) != 1 {
		t.Fatalf("as unidades non se copiaron: %+v", dest.Units)
	}

	// Task from session 3 (09-16) → target session 3 (09-15).
	if _, ok := fake.files["dixitalizacion/tarefas/dest/2025-09-15/enunciado.pdf"]; !ok {
		t.Fatalf("a tarefa non se remapou; ficheiros: %v", keys(fake.files))
	}
	if res.TaskFiles != 1 {
		t.Fatalf("TaskFiles = %d, quere 1", res.TaskFiles)
	}

	// Student data: ana updated (skip bob).
	if len(res.StudentsUpdated) != 1 || res.StudentsUpdated[0] != "ana" {
		t.Fatalf("StudentsUpdated = %v", res.StudentsUpdated)
	}
	if len(res.StudentsSkipped) != 1 || res.StudentsSkipped[0] != "bob" {
		t.Fatalf("StudentsSkipped = %v", res.StudentsSkipped)
	}
	// ana's práctica log remapped to 09-08 under the target course.
	var anaSubjects []map[string]any
	json.Unmarshal(fake.files["users/ana/"+fbBackupName], &anaSubjects)
	found := false
	for _, s := range anaSubjects {
		if s["id"] == "dest" {
			if logs, _ := s["logs"].(map[string]any); logs["2025-09-08"] != nil {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("a práctica de ana non se remapou: %s", fake.files["users/ana/"+fbBackupName])
	}
	// ana's work file remapped.
	if _, ok := fake.files["users/ana/traballos/dest/2025-09-08/memoria.odt"]; !ok {
		t.Fatalf("o ficheiro de ana non se remapou; ficheiros: %v", keys(fake.files))
	}
}

func TestBackupRestoreRoleGuards(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	a := loginStudent(t, fake, "alumno") // a plain student

	if _, err := a.AdminListMaterias(); err == nil {
		t.Fatal("AdminListMaterias debería requirir admin")
	}
	if err := a.AdminDeleteMateria("m1"); err == nil {
		t.Fatal("AdminDeleteMateria debería requirir admin")
	}
	if _, err := a.BackupCourses([]string{"m1"}, false); err == nil {
		t.Fatal("BackupCourses debería requirir profesor ou admin")
	}
	if _, err := a.RestoreSite(false); err == nil {
		t.Fatal("RestoreSite debería requirir admin")
	}
	if _, err := a.RestoreCourse("src", "dest", false); err == nil {
		t.Fatal("RestoreCourse debería requirir profesor ou admin")
	}
}
