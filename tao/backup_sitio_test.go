package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestBackupSiteLevaOsCursos é a proba da regresión que deixou unha copia de todo o
// sitio sen un só curso dentro: BackupSite listaba as materias con ListMaterias, que
// esixe a.profile — nil nunha sesión de Director —, e o erro ignorábase. A copia
// saía con usuarios e mensaxes pero sen materias nin tarefas, e despois non había
// nada que restaurar.
func TestBackupSiteLevaOsCursos(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{"ana": {Group: "eso4a"}})
	fake.files["dixitalizacion/materias/mat1.json"] = mustJSON(t, MateriaDef{ID: "mat1", Name: "Tecnoloxía"})
	fake.files["dixitalizacion/materias/mat2.json"] = mustJSON(t, MateriaDef{ID: "mat2", Name: "Robótica"})
	fake.files["dixitalizacion/tarefas/mat1/2025-09-15/enunciado.pdf"] = []byte("%PDF")
	fake.files["users/ana/traballos/mat1/2025-09-08/memoria.odt"] = []byte("odt")
	for _, d := range []string{
		"dixitalizacion/materias",
		"dixitalizacion/tarefas", "dixitalizacion/tarefas/mat1", "dixitalizacion/tarefas/mat1/2025-09-15",
	} {
		fake.dirs[d] = true
	}

	srv := httptest.NewServer(fake.handler("admin", "segredo"))
	defer srv.Close()

	a := NewApp()
	a.dataDir = t.TempDir()
	a.isAdmin = true
	if err := a.SaveAdminConfig(AdminConfig{ServerURL: srv.URL, FBAdminUser: "admin", FBAdminPass: "segredo"}); err != nil {
		t.Fatalf("SaveAdminConfig: %v", err)
	}

	// finishZip abre un diálogo de gardado, que non existe nunha proba: compróbase o
	// paso anterior, que é onde estaba o fallo — que os cursos entren no zip.
	destino := t.TempDir() + "/copia.zip"
	if err := a.withAdminFB(func() error {
		z := newZipBuilder()
		materias, err := a.listMateriasRaw()
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(materias))
		for _, m := range materias {
			ids = append(ids, m.ID)
		}
		if len(ids) != 2 {
			t.Fatalf("o Director ten que ver os 2 cursos, viu %v", ids)
		}
		if err := a.backupCoursesInto(z, ids); err != nil {
			return err
		}
		if err := z.addJSON(backupManifestName, backupManifest{Version: 1, Kind: "site", Courses: ids}); err != nil {
			return err
		}
		z.w.Close()
		return os.WriteFile(destino, z.buf.Bytes(), 0o600)
	}); err != nil {
		t.Fatalf("copia: %v", err)
	}

	raw, err := os.ReadFile(destino)
	if err != nil {
		t.Fatalf("ler o zip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("abrir o zip: %v", err)
	}
	dentro := map[string]bool{}
	for _, f := range zr.File {
		dentro[f.Name] = true
	}
	for _, agardado := range []string{
		"shared/materias/mat1.json",
		"shared/materias/mat2.json",
		"shared/tarefas/mat1/2025-09-15/enunciado.pdf",
	} {
		if !dentro[agardado] {
			t.Errorf("falta %q na copia; contén: %v", agardado, keysOf(dentro))
		}
	}
	var man backupManifest
	for _, f := range zr.File {
		if f.Name == backupManifestName {
			rc, _ := f.Open()
			dec := json.NewDecoder(rc)
			dec.Decode(&man)
			rc.Close()
		}
	}
	if len(man.Courses) != 2 {
		t.Errorf("o manifest di %v cursos; a copia que fallaba dicía []", man.Courses)
	}
}

func keysOf(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return strings.Join(out, ", ")
}
