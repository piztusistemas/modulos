package main

import (
	"net/http/httptest"
	"testing"
)

// loginStudent starts a student session against the fake server.
func loginStudent(t *testing.T, fake *fakeFileBrowser, user string) *App {
	t.Helper()
	srv := httptest.NewServer(fake.handler(user, "segredo"))
	t.Cleanup(srv.Close)
	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer(user, "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}
	return a
}

func TestWorkDirBuildsPerSessionPath(t *testing.T) {
	got, err := workDir("alumno", "m1", "2025-09-08")
	if err != nil {
		t.Fatalf("workDir: %v", err)
	}
	if want := "users/alumno/traballos/m1/2025-09-08"; got != want {
		t.Fatalf("workDir = %q, quere %q", got, want)
	}
}

// A materia id, username or date coming from the frontend must never be able to
// address a file outside the student's own session folder.
func TestWorkDirRejectsInvalidSegments(t *testing.T) {
	cases := []struct {
		name                string
		user, materia, date string
	}{
		{"materia baleira", "alumno", "", "2025-09-08"},
		{"usuario baleiro", "", "m1", "2025-09-08"},
		{"materia con barra", "alumno", "../outro", "2025-09-08"},
		{"materia con backslash", "alumno", `m1\x`, "2025-09-08"},
		{"usuario con traversal", "..", "m1", "2025-09-08"},
		{"data baleira", "alumno", "m1", ""},
		{"data non válida", "alumno", "m1", "8-9-2025"},
		{"data con traversal", "alumno", "m1", "../../etc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := workDir(c.user, c.materia, c.date); err == nil {
				t.Fatalf("esperábase erro para (%q, %q, %q)", c.user, c.materia, c.date)
			}
		})
	}
}

func TestWorkFileNameRejectsPaths(t *testing.T) {
	if _, err := workFileName("memoria.odt"); err != nil {
		t.Fatalf("nome válido rexeitado: %v", err)
	}
	for _, bad := range []string{"", "  ", "../backup.json", "sub/dir.odt", `..\win.odt`} {
		if _, err := workFileName(bad); err == nil {
			t.Fatalf("esperábase erro para o nome %q", bad)
		}
	}
}

func TestListWorkFilesReturnsSessionAttachments(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	dir := "users/alumno/traballos/m1/2025-09-08"
	fake.dirs[dir] = true
	fake.files[dir+"/memoria.odt"] = []byte("0123456789")
	fake.files[dir+"/circuito.fzz"] = []byte("ab")
	// Another session's file must not leak into this listing.
	fake.dirs["users/alumno/traballos/m1/2025-09-15"] = true
	fake.files["users/alumno/traballos/m1/2025-09-15/outro.ino"] = []byte("x")

	a := loginStudent(t, fake, "alumno")
	files, err := a.ListWorkFiles("m1", "2025-09-08")
	if err != nil {
		t.Fatalf("ListWorkFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("esperábanse 2 ficheiros, obtívose %+v", files)
	}
	// Sorted by name, with the size the server reported.
	if files[0].Name != "circuito.fzz" || files[0].Size != 2 {
		t.Fatalf("primeiro ficheiro inesperado: %+v", files[0])
	}
	if files[1].Name != "memoria.odt" || files[1].Size != 10 {
		t.Fatalf("segundo ficheiro inesperado: %+v", files[1])
	}
}

func TestListWorkFilesEmptyWhenNoUploadsYet(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	a := loginStudent(t, fake, "alumno")

	files, err := a.ListWorkFiles("m1", "2025-09-08")
	if err != nil {
		t.Fatalf("unha sesión sen ficheiros non debe ser un erro: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("esperábase unha lista baleira, obtívose %+v", files)
	}
}

func TestDeleteWorkFileRemovesOnlyThatFile(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	dir := "users/alumno/traballos/m1/2025-09-08"
	fake.dirs[dir] = true
	fake.files[dir+"/memoria.odt"] = []byte("contido")
	fake.files[dir+"/circuito.fzz"] = []byte("contido")

	a := loginStudent(t, fake, "alumno")
	if err := a.DeleteWorkFile("m1", "2025-09-08", "memoria.odt"); err != nil {
		t.Fatalf("DeleteWorkFile: %v", err)
	}
	if _, ok := fake.files[dir+"/memoria.odt"]; ok {
		t.Fatal("o ficheiro non se eliminou")
	}
	if _, ok := fake.files[dir+"/circuito.fzz"]; !ok {
		t.Fatal("eliminouse un ficheiro que non tocaba")
	}

	// A name that tries to escape the session folder must be rejected before any
	// request reaches the server.
	if err := a.DeleteWorkFile("m1", "2025-09-08", "../../"+fbBackupName); err == nil {
		t.Fatal("esperábase erro: o nome non pode saír do cartafol da sesión")
	}
	if _, ok := fake.files["users/alumno/"+fbBackupName]; ok {
		t.Fatal("a copia de seguridade non debería existir sequera")
	}
}

func TestWorkFilesRequireServerSession(t *testing.T) {
	a := NewApp() // sen login
	if _, err := a.ListWorkFiles("m1", "2025-09-08"); err == nil {
		t.Fatal("esperábase erro sen sesión no servidor")
	}
	if err := a.DeleteWorkFile("m1", "2025-09-08", "memoria.odt"); err == nil {
		t.Fatal("esperábase erro sen sesión no servidor")
	}
}

func TestProfessorListsStudentWorkFiles(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	dir := "users/alumno/traballos/m1/2025-09-08"
	fake.dirs[dir] = true
	fake.files[dir+"/memoria.odt"] = []byte("traballo do alumno")

	a := loginStudent(t, fake, "prof")
	if a.profile.Role != "profesor" {
		t.Fatalf("rol = %q, quere profesor", a.profile.Role)
	}

	files, err := a.ListStudentWorkFiles("alumno", "m1", "2025-09-08")
	if err != nil {
		t.Fatalf("ListStudentWorkFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "memoria.odt" {
		t.Fatalf("listado inesperado: %+v", files)
	}

	// A student session must not be able to read another student's files.
	a.profile.Role = "alumno"
	if _, err := a.ListStudentWorkFiles("alumno", "m1", "2025-09-08"); err == nil {
		t.Fatal("esperábase erro: só o profesor pode ver os traballos doutro alumno")
	}
}

func TestTaskFilesSharedAcrossMateria(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana": {Group: "eso4a"}, "beto": {Group: "eso4a"},
	})
	// The professor's task file is stored once, in the shared folder.
	dir := "dixitalizacion/tarefas/m1/2025-09-08"
	fake.dirs[dir] = true
	fake.files[dir+"/enunciado.pdf"] = []byte("%PDF")

	prof := loginStudent(t, fake, "prof")
	if prof.profile.Role != "profesor" {
		t.Fatalf("rol = %q, quere profesor", prof.profile.Role)
	}

	// Every student of the materia sees the same task.
	for _, u := range []string{"ana", "beto"} {
		st := loginStudent(t, fake, u)
		files, err := st.ListTaskFiles("m1", "2025-09-08")
		if err != nil {
			t.Fatalf("ListTaskFiles(%s): %v", u, err)
		}
		if len(files) != 1 || files[0].Name != "enunciado.pdf" {
			t.Fatalf("%s debería ver a tarefa: %+v", u, files)
		}
	}

	// Only the professor may delete it; a student cannot.
	ana := loginStudent(t, fake, "ana")
	if err := ana.DeleteTaskFile("m1", "2025-09-08", "enunciado.pdf"); err == nil {
		t.Fatal("un alumno non debería poder borrar a tarefa")
	}
	if err := prof.DeleteTaskFile("m1", "2025-09-08", "enunciado.pdf"); err != nil {
		t.Fatalf("DeleteTaskFile (profesor): %v", err)
	}
	if _, ok := fake.files[dir+"/enunciado.pdf"]; ok {
		t.Fatal("a tarefa non se eliminou")
	}

	// A name that escapes the session folder is rejected.
	if err := prof.DeleteTaskFile("m1", "2025-09-08", "../../"+fbBackupName); err == nil {
		t.Fatal("un nome con traversal debería rexeitarse")
	}
}

// File Browser's mkdir only creates the last segment, so the chain
// traballos/<materia>/<data> has to be created level by level.
func TestFbEnsureWorkDirCreatesEveryParent(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	a := loginStudent(t, fake, "alumno")

	dir := "users/alumno/traballos/m1/2025-09-08"
	if err := a.fbEnsureWorkDir(dir); err != nil {
		t.Fatalf("fbEnsureWorkDir: %v", err)
	}
	for _, want := range []string{
		"users", "users/alumno", "users/alumno/traballos",
		"users/alumno/traballos/m1", dir,
	} {
		if !fake.dirs[want] {
			t.Fatalf("non se creou o cartafol %q; creados: %v", want, fake.dirs)
		}
	}
}
