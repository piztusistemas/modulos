package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// De-identified copy of a session's student work, built on demand by a professor so it
// can be handed to an AI tool for correction without exposing any personal data (see
// fbPracticasRoot). Each student's files land under a subfolder named only by their
// internal UUID (see fbIdentidadeFile / sharedAlumnosUUID) — never their username, name
// or certificate. The uuid<->student link needed to grade afterwards stays in
// alumnos-uuid.json and profiles.json, both outside this folder tree.

// PracticaCorrectionSummary reports what PrepareCorrectionFolder copied.
type PracticaCorrectionSummary struct {
	Students int      `json:"students"` // students whose work was copied
	Files    int      `json:"files"`    // total files copied
	Skipped  []string `json:"skipped"`  // usernames left out, with the reason, for visibility
}

// practicaDir returns the shared, de-identified folder for one session of a materia
// (dixitalizacion/practicas/<materia>/<data>), sibling of the per-student
// traballos/comentarios/tarefas folders.
func practicaDir(materiaID, date string) (string, error) {
	materia, err := workSegment(materiaID, "a materia")
	if err != nil {
		return "", err
	}
	day, err := workDate(date)
	if err != nil {
		return "", err
	}
	return sharedRoot + "/" + fbPracticasRoot + "/" + materia + "/" + day, nil
}

// PrepareCorrectionFolder builds (or refreshes) the de-identified folder for one
// session of a materia: for every student with an internal UUID who has work in that
// session, it copies their traballos/<materia>/<date>/ files into
// practicas/<materia>/<date>/<uuid>/, overwriting any previous copy so it can be
// re-run as students keep working. Professor only.
func (a *App) PrepareCorrectionFolder(materiaID, date string) (*PracticaCorrectionSummary, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	dir, err := practicaDir(materiaID, date)
	if err != nil {
		return nil, err
	}

	roster, err := a.ListRoster()
	if err != nil {
		return nil, err
	}
	uuids := map[string]string{}
	a.fbReadSharedJSON(sharedAlumnosUUID, &uuids)

	res := &PracticaCorrectionSummary{}
	for _, student := range roster {
		id, ok := uuids[student.Username]
		if !ok {
			for name, v := range uuids {
				if strings.EqualFold(name, student.Username) {
					id, ok = v, true
					break
				}
			}
		}
		if !ok || strings.TrimSpace(id) == "" {
			res.Skipped = append(res.Skipped, student.Username+" (sen uuid interno)")
			continue
		}

		files, err := a.listWorkFiles(student.Username, materiaID, date)
		if err != nil || len(files) == 0 {
			continue // nothing to copy for this session: not an error
		}
		srcDir, err := workDir(student.Username, materiaID, date)
		if err != nil {
			continue
		}

		dstDir := dir + "/" + id
		if err := a.fbEnsureWorkDir(dstDir); err != nil {
			log.Printf("[practicas] non se puido preparar %s: %v", dstDir, err)
			res.Skipped = append(res.Skipped, student.Username+" (erro de cartafol)")
			continue
		}

		// Cada ficheiro é unha descarga máis unha subida: en serie, preparar a carpeta
		// dun grupo enteiro eran centos de idas e voltas (ver concorrencia.go).
		var copiados int32
		enParalelo(len(files), func(i int) {
			f := files[i]
			content, err := a.fbRawGet(srcDir + "/" + f.Name)
			if err != nil {
				log.Printf("[practicas] non se puido ler %s/%s: %v", srcDir, f.Name, err)
				return
			}
			if err := a.fbUpload(dstDir+"/"+f.Name, content); err != nil {
				log.Printf("[practicas] non se puido copiar %s/%s: %v", srcDir, f.Name, err)
				return
			}
			atomic.AddInt32(&copiados, 1)
		})
		copied := int(copiados)
		if copied > 0 {
			res.Students++
			res.Files += copied
		}
	}

	sort.Strings(res.Skipped)
	log.Printf("[practicas] preparado %s: %d alumno(s), %d ficheiro(s)", dir, res.Students, res.Files)
	return res, nil
}

// AbrirCartafolCorreccion abre no navegador do sistema a vista web de File Browser
// sobre o cartafol de corrección dunha sesión (dixitalizacion/practicas/<materia>/<data>),
// para que o profesor poida explorala ou descargala sen saír do seu xestor de ficheiros
// habitual. O cartafol vive no servidor, non no disco local, así que "abrir" aquí é
// abrir a súa páxina en File Browser (pode pedir iniciar sesión se o navegador non ten
// xa unha sesión aberta con ese servidor). Professor only.
func (a *App) AbrirCartafolCorreccion(materiaID, date string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	dir, err := practicaDir(materiaID, date)
	if err != nil {
		return err
	}
	if a.fbURL == "" {
		return fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	application.Get().Browser.OpenURL(fbURLWeb(a.fbURL, dir))
	return nil
}
