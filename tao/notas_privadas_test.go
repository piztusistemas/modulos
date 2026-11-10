package main

import (
	"net/http/httptest"
	"testing"
)

// TestPrivateNoteRoundTripIsolatedFromStudentBackup covers point 3: a private note
// round-trips through GetPrivateNote/SavePrivateNote, is scoped by materia ownership
// same as everything else materia-scoped, and — critically — never appears in the
// student's own backup, which is the channel the student's own account can read.
func TestPrivateNoteRoundTripIsolatedFromStudentBackup(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof", "outro"})
	fake.files["dixitalizacion/materias/m1.json"] = mustJSON(t, MateriaDef{ID: "m1", Name: "Tec", Owner: "prof"})

	srv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer("prof", "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer: %v", err)
	}

	if got, err := a.GetPrivateNote("alumno", "m1"); err != nil || got != "" {
		t.Fatalf("nota inicial = %q, %v; esperábase baleira", got, err)
	}

	if err := a.SavePrivateNote("alumno", "m1", "# Observacións\n\nFala moito na clase."); err != nil {
		t.Fatalf("SavePrivateNote: %v", err)
	}
	got, err := a.GetPrivateNote("alumno", "m1")
	if err != nil || got != "# Observacións\n\nFala moito na clase." {
		t.Fatalf("round-trip: obtívose %q, %v", got, err)
	}

	// It must NOT be stored under the student's own folder, nor show up in their backup.
	if _, ok := fake.files["users/alumno/"+fbBackupName]; ok {
		t.Fatal("a nota privada non debe tocar o backup do alumno")
	}
	if _, ok := fake.files["dixitalizacion/notas-privadas/m1/alumno.json"]; !ok {
		t.Fatalf("a nota debería quedar en dixitalizacion/notas-privadas/m1/alumno.json; ficheiros: %v", keys(fake.files))
	}

	// Clearing it (empty markdown) deletes the file rather than leaving an empty one.
	if err := a.SavePrivateNote("alumno", "m1", "   "); err != nil {
		t.Fatalf("SavePrivateNote (limpar): %v", err)
	}
	if _, ok := fake.files["dixitalizacion/notas-privadas/m1/alumno.json"]; ok {
		t.Fatal("unha nota baleira debería eliminar o ficheiro")
	}

	// A professor who does not own the materia cannot read or write its private notes.
	srv2 := httptest.NewServer(fake.handler("outro", "segredo"))
	defer srv2.Close()
	outro := NewApp()
	outro.dataDir = t.TempDir()
	if _, err := outro.loginServer("outro", "segredo", srv2.URL); err != nil {
		t.Fatalf("loginServer outro: %v", err)
	}
	if _, err := outro.GetPrivateNote("alumno", "m1"); err == nil {
		t.Fatal("esperábase erro: outro profesor non pode ler a nota privada dunha materia allea")
	}
	if err := outro.SavePrivateNote("alumno", "m1", "intento alleo"); err == nil {
		t.Fatal("esperábase erro: outro profesor non pode escribir a nota privada dunha materia allea")
	}
}
