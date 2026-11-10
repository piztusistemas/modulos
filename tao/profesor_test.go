package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// mustJSON marshals v or fails the test.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// seedShared marks the shared folder dirs as existing on the fake server so that
// listing/uploading inside dixitalizacion/ behaves like a real File Browser.
func seedShared(f *fakeFileBrowser) {
	f.dirs["dixitalizacion"] = true
	f.dirs["dixitalizacion/materias"] = true
}

func TestLoginResolvesStudentRoleAndGroup(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"m.pardinas"})
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"alumno": {Name: "Ana", Surname: "Pérez", Group: "eso4a"},
	})
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	prof, err := a.loginServer("alumno", "segredo", srv.URL)
	if err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	if prof.Role != "alumno" {
		t.Fatalf("rol = %q, quere alumno", prof.Role)
	}
	if prof.Group != "eso4a" || prof.Name != "Ana" || prof.Surname != "Pérez" {
		t.Fatalf("identidade do roster non aplicada: %+v", prof)
	}
}

func TestLoginResolvesProfessorRole(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"m.pardinas", "outro"})
	srv := httptest.NewServer(fake.handler("m.pardinas", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	prof, err := a.loginServer("m.pardinas", "segredo", srv.URL)
	if err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	if prof.Role != "profesor" {
		t.Fatalf("rol = %q, quere profesor", prof.Role)
	}
}

func TestMateriaRoundTripRequiresProfessor(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	m := MateriaDef{
		ID: "mat1", Name: "Tecnoloxía", AcademicYear: "2025/2026",
		Groups: []string{"eso4a"},
		Config: map[string]any{"startDate": "2025-09-08", "endDate": "2026-06-19"},
		Units:  []any{},
	}
	if err := a.SaveMateria(m); err != nil {
		t.Fatalf("SaveMateria: %v", err)
	}
	if _, ok := fake.files["dixitalizacion/materias/mat1.json"]; !ok {
		t.Fatalf("a materia non se gardou; ficheiros: %v", keys(fake.files))
	}

	list, err := a.ListMaterias()
	if err != nil {
		t.Fatalf("ListMaterias: %v", err)
	}
	if len(list) != 1 || list[0].ID != "mat1" || len(list[0].Groups) != 1 || list[0].Groups[0] != "eso4a" {
		t.Fatalf("listado inesperado: %+v", list)
	}

	// A non-professor session must be rejected.
	a.profile.Role = "alumno"
	if err := a.SaveMateria(m); err == nil {
		t.Fatal("esperábase erro: só o profesor pode gardar materias")
	}
}

// TestMateriaOwnershipEnforced covers point 1 of the feature: a professor may only
// view/edit their own materias (or one still unclaimed, predating the Owner field);
// another professor's materia is rejected outright, and reported ownership from the
// client is never trusted (the server always resolves it itself).
func TestMateriaOwnershipEnforced(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof1", "prof2"})

	srv1 := httptest.NewServer(fake.handler("prof1", "segredo"))
	defer srv1.Close()
	srv2 := httptest.NewServer(fake.handler("prof2", "segredo"))
	defer srv2.Close()

	prof1 := NewApp()
	prof1.dataDir = t.TempDir()
	if _, err := prof1.loginServer("prof1", "segredo", srv1.URL); err != nil {
		t.Fatalf("loginServer prof1: %v", err)
	}
	prof2 := NewApp()
	prof2.dataDir = t.TempDir()
	if _, err := prof2.loginServer("prof2", "segredo", srv2.URL); err != nil {
		t.Fatalf("loginServer prof2: %v", err)
	}

	m := MateriaDef{ID: "mat1", Name: "Tecnoloxía", Groups: []string{"eso4a"}}
	// A client-supplied Owner is ignored: the server resolves it to whoever is saving.
	m.Owner = "outra-persoa"
	if err := prof1.SaveMateria(m); err != nil {
		t.Fatalf("SaveMateria (prof1, creación): %v", err)
	}
	list, err := prof1.listMateriasRaw()
	if err != nil || len(list) != 1 || list[0].Owner != "prof1" {
		t.Fatalf("a materia debería quedar en propiedade de prof1, obtívose: %+v (err=%v)", list, err)
	}

	// prof2 cannot save nor delete it.
	if err := prof2.SaveMateria(MateriaDef{ID: "mat1", Name: "Modificada por prof2"}); err == nil {
		t.Fatal("esperábase erro: prof2 non pode gardar a materia de prof1")
	}
	if err := prof2.DeleteMateria("mat1"); err == nil {
		t.Fatal("esperábase erro: prof2 non pode eliminar a materia de prof1")
	}

	// prof1 may still edit their own.
	if err := prof1.SaveMateria(MateriaDef{ID: "mat1", Name: "Tecnoloxía (revisada)"}); err != nil {
		t.Fatalf("SaveMateria (prof1, propia): %v", err)
	}

	// ListMyMaterias never surfaces it to prof2.
	mine2, err := prof2.ListMyMaterias()
	if err != nil {
		t.Fatalf("ListMyMaterias (prof2): %v", err)
	}
	for _, mm := range mine2 {
		if mm.ID == "mat1" {
			t.Fatalf("prof2 non debería ver a materia de prof1: %+v", mine2)
		}
	}
	mine1, err := prof1.ListMyMaterias()
	if err != nil || len(mine1) != 1 || mine1[0].ID != "mat1" {
		t.Fatalf("prof1 debería ver a súa propia materia: %+v (err=%v)", mine1, err)
	}
}

// TestMateriaUnclaimedIsAdoptedOnSave covers the migration path: a materia written
// before the Owner field existed (Owner == "") stays editable by any professor until
// the first SaveMateria "adopts" it — no manual migration needed on upgrade.
func TestMateriaUnclaimedIsAdoptedOnSave(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof1", "prof2"})
	fake.files["dixitalizacion/materias/legacy.json"] = mustJSON(t, MateriaDef{ID: "legacy", Name: "Antiga"})

	srv1 := httptest.NewServer(fake.handler("prof1", "segredo"))
	defer srv1.Close()

	prof1 := NewApp()
	prof1.dataDir = t.TempDir()
	if _, err := prof1.loginServer("prof1", "segredo", srv1.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	if err := prof1.SaveMateria(MateriaDef{ID: "legacy", Name: "Antiga (retocada)"}); err != nil {
		t.Fatalf("SaveMateria (adoptar): %v", err)
	}
	list, err := prof1.listMateriasRaw()
	if err != nil || len(list) != 1 || list[0].Owner != "prof1" {
		t.Fatalf("a materia sen dono debería quedar adoptada por prof1: %+v (err=%v)", list, err)
	}
}

// TestAdminMateriaBypassesOwnership covers the administrator half of point 1: an admin
// may list, edit (including reassigning Owner) and delete any professor's materia.
func TestAdminMateriaBypassesOwnership(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof1"})
	fake.files["dixitalizacion/materias/mat1.json"] = mustJSON(t, MateriaDef{ID: "mat1", Name: "Tec", Owner: "prof1"})

	srvAdmin := httptest.NewServer(fake.handler("admin", "segredo"))
	defer srvAdmin.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	a.isAdmin = true // requireAdmin only checks this flag, same as admin_verify_test.go
	if err := a.SaveAdminConfig(AdminConfig{ServerURL: srvAdmin.URL, FBAdminUser: "admin", FBAdminPass: "segredo"}); err != nil {
		t.Fatalf("SaveAdminConfig: %v", err)
	}

	list, err := a.AdminListMaterias()
	if err != nil || len(list) != 1 || list[0].Owner != "prof1" {
		t.Fatalf("AdminListMaterias: %+v (err=%v)", list, err)
	}
	if err := a.AdminSaveMateria(MateriaDef{ID: "mat1", Name: "Tec", Owner: "prof2"}); err != nil {
		t.Fatalf("AdminSaveMateria (reasignar): %v", err)
	}
	list, _ = a.AdminListMaterias()
	if len(list) != 1 || list[0].Owner != "prof2" {
		t.Fatalf("a reasignación de propietario non se aplicou: %+v", list)
	}
	if err := a.AdminDeleteMateria("mat1"); err != nil {
		t.Fatalf("AdminDeleteMateria: %v", err)
	}
	if _, ok := fake.files["dixitalizacion/materias/mat1.json"]; ok {
		t.Fatal("a materia debería quedar eliminada")
	}
}

func TestGetMyMateriasFiltersByGroup(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"alumno": {Group: "eso4a"},
	})
	fake.files["dixitalizacion/materias/m1.json"] = mustJSON(t, MateriaDef{ID: "m1", Name: "Tec", Groups: []string{"eso4a"}})
	fake.files["dixitalizacion/materias/m2.json"] = mustJSON(t, MateriaDef{ID: "m2", Name: "TIC", Groups: []string{"bac2"}})
	srv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("alumno", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	mine, err := a.GetMyMaterias()
	if err != nil {
		t.Fatalf("GetMyMaterias: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != "m1" {
		t.Fatalf("esperábase só a materia do grupo eso4a, obtívose: %+v", mine)
	}
}

func TestProfessorReadsAndWritesStudentBackup(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	// No backup yet for the student.
	if got, err := a.GetStudentBackup("alumno"); err != nil || got != "" {
		t.Fatalf("GetStudentBackup baleiro = %q, %v", got, err)
	}

	payload := `[{"id":"m1","logs":{"2025-09-08":{"date":"2025-09-08","content":"corrixido","status":"lectivo"}}}]`
	if err := a.SaveStudentBackup("alumno", payload); err != nil {
		t.Fatalf("SaveStudentBackup: %v", err)
	}
	if _, ok := fake.files["users/alumno/"+fbBackupName]; !ok {
		t.Fatalf("a copia do alumno non se gardou; ficheiros: %v", keys(fake.files))
	}
	got, err := a.GetStudentBackup("alumno")
	if err != nil {
		t.Fatalf("GetStudentBackup: %v", err)
	}
	if got != payload {
		t.Fatalf("round-trip: esperábase %q, obtívose %q", payload, got)
	}

	// A non-professor session cannot touch another student's work.
	a.profile.Role = "alumno"
	if err := a.SaveStudentBackup("alumno", payload); err == nil {
		t.Fatal("esperábase erro: só o profesor pode editar traballos")
	}
}

func TestSaveStudentLogComment(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	// The student has an existing práctica for the day.
	fake.dirs["users/alumno"] = true
	fake.files["users/alumno/"+fbBackupName] = []byte(`[{"id":"m1","logs":{"2025-09-08":{"date":"2025-09-08","content":"o meu traballo","status":"lectivo"}}}]`)

	if err := a.SaveStudentLogComment("alumno", "m1", "2025-09-08", "  Ben feito, revisa o punto 3  "); err != nil {
		t.Fatalf("SaveStudentLogComment: %v", err)
	}

	// The comment is patched in, the student's own content preserved.
	var subjects []map[string]any
	if err := json.Unmarshal(fake.files["users/alumno/"+fbBackupName], &subjects); err != nil {
		t.Fatalf("backup non válido: %v", err)
	}
	logs := subjects[0]["logs"].(map[string]any)
	entry := logs["2025-09-08"].(map[string]any)
	if entry["profesorComment"] != "Ben feito, revisa o punto 3" {
		t.Fatalf("comentario non gardado (recortado): %+v", entry)
	}
	if entry["content"] != "o meu traballo" {
		t.Fatalf("o traballo do alumno non se debe tocar: %+v", entry)
	}

	// An empty comment clears it.
	if err := a.SaveStudentLogComment("alumno", "m1", "2025-09-08", "  "); err != nil {
		t.Fatalf("SaveStudentLogComment (limpar): %v", err)
	}
	json.Unmarshal(fake.files["users/alumno/"+fbBackupName], &subjects)
	entry = subjects[0]["logs"].(map[string]any)["2025-09-08"].(map[string]any)
	if _, ok := entry["profesorComment"]; ok {
		t.Fatalf("o comentario baleiro debería eliminarse: %+v", entry)
	}

	// A non-professor cannot comment.
	a.profile.Role = "alumno"
	if err := a.SaveStudentLogComment("alumno", "m1", "2025-09-08", "intruso"); err == nil {
		t.Fatal("só o profesor debería poder comentar")
	}
}

func TestCommentFilesFlow(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	dir := "users/alumno/comentarios/m1/2025-09-08"
	fake.dirs[dir] = true
	fake.files[dir+"/aclaracion.pdf"] = []byte("%PDF")

	prof := loginStudent(t, fake, "prof")
	got, err := prof.ListStudentCommentFiles("alumno", "m1", "2025-09-08")
	if err != nil {
		t.Fatalf("ListStudentCommentFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "aclaracion.pdf" {
		t.Fatalf("o profesor debería ver o ficheiro aclaratorio: %+v", got)
	}

	student := loginStudent(t, fake, "alumno")
	mine, err := student.ListCommentFiles("m1", "2025-09-08")
	if err != nil {
		t.Fatalf("ListCommentFiles: %v", err)
	}
	if len(mine) != 1 || mine[0].Name != "aclaracion.pdf" {
		t.Fatalf("o alumno debería ver o ficheiro aclaratorio: %+v", mine)
	}

	// Only the professor may delete it.
	if err := student.DeleteCommentFile("alumno", "m1", "2025-09-08", "aclaracion.pdf"); err == nil {
		t.Fatal("un alumno non debería borrar ficheiros aclaratorios")
	}
	if err := prof.DeleteCommentFile("alumno", "m1", "2025-09-08", "aclaracion.pdf"); err != nil {
		t.Fatalf("DeleteCommentFile (profesor): %v", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSaveMateriaGardaHistorial: SaveMateria sobrescribe a materia enteira, así que antes
// de facelo debe deixar a versión anterior en materias-historial/<id>/ — é o que permite
// recuperar «Contidos da sesión» borrados por unha reprogramación ou unha copia local
// desfasada (incidente de TICS 1ºBAC, 29/10/2026). Tamén se poda ao máximo configurado.
func TestSaveMateriaGardaHistorial(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	const historial = "dixitalizacion/materias-historial/tic1/"
	contarHistorial := func() (n int, ultimo []byte) {
		var nomes []string
		for p := range fake.files {
			if strings.HasPrefix(p, historial) {
				nomes = append(nomes, p)
			}
		}
		sort.Strings(nomes)
		if len(nomes) > 0 {
			ultimo = fake.files[nomes[len(nomes)-1]]
		}
		return len(nomes), ultimo
	}

	conContidos := MateriaDef{ID: "tic1", Name: "TICS 1ºBAC", Logs: map[string]any{
		"2026-09-23": map[string]any{"date": "2026-09-23", "content": "Práctica 7 Calc", "status": "lectivo"},
	}}
	if err := a.SaveMateria(conContidos); err != nil {
		t.Fatalf("SaveMateria (creación): %v", err)
	}
	if n, _ := contarHistorial(); n != 0 {
		t.Fatalf("unha materia nova non ten versión anterior que gardar; historial = %d", n)
	}

	// Gardado que perde os contidos (o que facía a reprogramación defectuosa).
	if err := a.SaveMateria(MateriaDef{ID: "tic1", Name: "TICS 1ºBAC", Logs: map[string]any{}}); err != nil {
		t.Fatalf("SaveMateria (sobrescritura): %v", err)
	}
	n, ultimo := contarHistorial()
	if n != 1 {
		t.Fatalf("historial = %d versións, quere 1", n)
	}
	var previa MateriaDef
	if err := json.Unmarshal(ultimo, &previa); err != nil {
		t.Fatalf("copia previa non é JSON: %v", err)
	}
	if _, ok := previa.Logs["2026-09-23"]; !ok {
		t.Fatalf("a copia previa non conserva os contidos da sesión: %+v", previa.Logs)
	}

	// Poda: nunca máis de materiaHistorialMax versións.
	for i := 0; i < materiaHistorialMax+5; i++ {
		if err := a.SaveMateria(MateriaDef{ID: "tic1", Name: fmt.Sprintf("v%d", i)}); err != nil {
			t.Fatalf("SaveMateria (%d): %v", i, err)
		}
		time.Sleep(2 * time.Millisecond) // nomes distintos (resolución de ms)
	}
	if n, _ := contarHistorial(); n != materiaHistorialMax {
		t.Fatalf("historial = %d versións, quere %d", n, materiaHistorialMax)
	}
	// O historial non se lista coma se fose unha materia máis.
	list, err := a.ListMaterias()
	if err != nil {
		t.Fatalf("ListMaterias: %v", err)
	}
	if len(list) != 1 || list[0].ID != "tic1" {
		t.Fatalf("ListMaterias = %+v, quere só tic1", list)
	}
}
