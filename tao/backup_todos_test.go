package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestRestoreAllCoursesDevolveOsCursosDaCopia é o caso que faltaba: o Director ten
// unha copia con VARIOS cursos (e o alumnado dentro) e quere devolvelos todos ao
// servidor dunha vez. Ata agora había que ir curso a curso, e só cara a cursos xa
// creados a man: un dos cursos desta proba nin sequera existe no servidor.
func TestRestoreAllCoursesDevolveOsCursosDaCopia(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana": {Group: "eso4a"}, // ten conta → restáurase
		// "bob" non está → omítese
	})
	// "mat1" segue no servidor (restaurarase por riba); "mat2" xa non existe.
	fake.files["dixitalizacion/materias/mat1.json"] = mustJSON(t, MateriaDef{
		ID: "mat1", Name: "Vello", AcademicYear: "2025/2026",
		Config: mondaysConfig("2025-09-01", "2025-09-15"),
	})

	srv := httptest.NewServer(fake.handler("admin", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	a.isAdmin = true
	if err := a.SaveAdminConfig(AdminConfig{ServerURL: srv.URL, FBAdminUser: "admin", FBAdminPass: "segredo"}); err != nil {
		t.Fatalf("SaveAdminConfig: %v", err)
	}

	cfg := mondaysConfig("2025-09-01", "2025-09-15")
	a.restoreFiles = map[string][]byte{
		"shared/materias/mat1.json": mustJSON(t, MateriaDef{
			ID: "mat1", Name: "Tecnoloxía", AcademicYear: "2025/2026", Config: cfg,
			Units: []any{map[string]any{"id": float64(1), "title": "Debuxo", "sessions": float64(3)}},
			Logs:  map[string]any{"2025-09-08": map[string]any{"date": "2025-09-08", "content": "sesión 2", "status": "lectivo"}},
		}),
		"shared/materias/mat2.json": mustJSON(t, MateriaDef{
			ID: "mat2", Name: "Robótica", AcademicYear: "2025/2026", Config: cfg,
		}),
		"shared/tarefas/mat1/2025-09-15/enunciado.pdf":    []byte("%PDF"),
		"users/ana/traballos/mat1/2025-09-08/memoria.odt": []byte("odt"),
		"users/ana/dixitalizacion-backup.json":            []byte(`[{"id":"mat1","logs":{"2025-09-08":{"date":"2025-09-08","content":"traballo ana","status":"lectivo"}}}]`),
	}
	a.restoreManifest = backupManifest{Kind: "site", Courses: []string{"mat1", "mat2"}, Users: []string{"ana", "bob"}}

	res, err := a.RestoreAllCourses(true)
	if err != nil {
		t.Fatalf("RestoreAllCourses: %v", err)
	}
	if len(res.Courses) != 2 {
		t.Fatalf("Courses = %v, queríanse os dous", res.Courses)
	}
	if len(res.FailedCourse) != 0 {
		t.Fatalf("FailedCourse = %v", res.FailedCourse)
	}

	// O curso que xa existía volve co nome e o contido da copia, non co do servidor.
	// Lese directamente do servidor falso: fóra de withAdminFB a app xa non ten
	// sesión ningunha (o Director non abre sesión propia de File Browser).
	var mat1 MateriaDef
	if err := json.Unmarshal(fake.files["dixitalizacion/materias/mat1.json"], &mat1); err != nil {
		t.Fatalf("ler mat1: %v", err)
	}
	if mat1.Name != "Tecnoloxía" || len(mat1.Units) != 1 {
		t.Fatalf("mat1 non quedou como na copia: %+v", mat1)
	}
	// Sen remapeo: a sesión queda na súa propia data.
	if entry, ok := mat1.Logs["2025-09-08"].(map[string]any); !ok || entry["content"] != "sesión 2" {
		t.Fatalf("o diario de mat1 non se restaurou na súa data: %+v", mat1.Logs)
	}
	// O curso que xa non existía créase.
	if _, ok := fake.files["dixitalizacion/materias/mat2.json"]; !ok {
		t.Fatalf("mat2 non se creou; ficheiros: %v", keys(fake.files))
	}
	// Tarefas e traballos do alumnado, na mesma ruta de orixe.
	if _, ok := fake.files["dixitalizacion/tarefas/mat1/2025-09-15/enunciado.pdf"]; !ok {
		t.Fatalf("a tarefa non se restaurou; ficheiros: %v", keys(fake.files))
	}
	if _, ok := fake.files["users/ana/traballos/mat1/2025-09-08/memoria.odt"]; !ok {
		t.Fatalf("o traballo de ana non se restaurou; ficheiros: %v", keys(fake.files))
	}
	if len(res.Students) != 1 || res.Students[0] != "ana" {
		t.Fatalf("Students = %v", res.Students)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "bob" {
		t.Fatalf("Skipped = %v", res.Skipped)
	}
	var anaSubjects []map[string]any
	json.Unmarshal(fake.files["users/ana/"+fbBackupName], &anaSubjects)
	if len(anaSubjects) == 0 {
		t.Fatalf("a práctica de ana non se restaurou: %s", fake.files["users/ana/"+fbBackupName])
	}
}

// TestRestoreAllCoursesRequireAdmin: só o Director. Un profesor restaura os seus
// cursos un a un (RestoreCourse), pero non recrea cursos no servidor.
func TestRestoreAllCoursesRequireAdmin(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	a.restoreFiles = map[string][]byte{"shared/materias/m1.json": []byte("{}")}
	if _, err := a.RestoreAllCourses(false); err == nil {
		t.Fatal("RestoreAllCourses debería requirir Director")
	}
}

// TestCursosDaCopiaSenManifest: unha copia vella, sen manifest, segue sendo
// restaurable — os cursos dedúcense dos propios ficheiros.
func TestCursosDaCopiaSenManifest(t *testing.T) {
	a := NewApp()
	a.restoreFiles = map[string][]byte{
		"shared/materias/b.json":                 {},
		"shared/materias/a.json":                 {},
		"shared/tarefas/a/2025-09-15/tarefa.pdf": {},
		"users/ana/dixitalizacion-backup.json":   {},
	}
	got := a.cursosDaCopia()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("cursosDaCopia = %v, quería [a b]", got)
	}
}
