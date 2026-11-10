package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

// Cambio de grupo dun alumno (modo Director).
//
// O grupo dun alumno vive no roster (alumnos.json) e é o que decide que materias ve
// (GetMyMaterias). Pero o seu traballo gárdase por materia, non por grupo:
//
//	users/<login>/dixitalizacion-backup.json   -> rexistros (logs) e informes por id de materia
//	users/<login>/traballos/<materia>/<data>/   -> ficheiros fonte de cada sesión
//	users/<login>/comentarios/<materia>/<data>/ -> ficheiros de resposta do profesor
//
// Se bach1a e bach1b comparten a mesma materia (Groups: [bach1a, bach1b]) abonda con
// cambiar o roster. Pero o habitual é que cada grupo teña a súa propia materia (outro
// id, outro calendario), e entón o alumno chegaría ao grupo novo sen ver nada do que
// xa fixera. Por iso o cambio de grupo leva un plan de migración: para cada materia do
// grupo vello, a súa equivalente no novo e a correspondencia de datas entre os dous
// calendarios (a Nª sesión dun co a Nª do outro, calculada no panel coa mesma lóxica de
// calendario da app, que non existe en Go).
//
// A migración COPIA, non move: a materia vella queda intacta no backup e no servidor
// (o alumno xa non a ve, pero se volve ao grupo vello o seu traballo segue alí), e no
// destino nunca se pisa unha sesión que xa teña contido nin un ficheiro que xa exista.
//
// Non se copian as notas privadas nin as tarefas personalizadas do profesor da materia
// vella: son do seu profesor, e a materia nova pode ser doutro.

// MigracionMateria é un par materia vella -> materia nova do plan de cambio de grupo.
// Datas leva a correspondencia de datas de sesión (vella -> nova); unha data que non
// estea no mapa consérvase igual.
type MigracionMateria struct {
	From  string            `json:"from"`
	To    string            `json:"to"`
	Datas map[string]string `json:"datas"`
}

// CambioGrupoResult resume o que fixo ChangeUserGroup, para o aviso do panel.
type CambioGrupoResult struct {
	Materias  int      `json:"materias"`  // materias migradas
	Rexistros int      `json:"rexistros"` // sesións copiadas no backup
	Ficheiros int      `json:"ficheiros"` // ficheiros copiados (traballos e comentarios)
	Avisos    []string `json:"avisos"`    // o que non se puido copiar ou se deixou estar
}

// ChangeUserGroup cambia o grupo do alumno username a newGroup (no roster do servidor e
// no perfil local) e, segundo plan, copia o seu traballo das materias do grupo vello ás
// equivalentes do novo. Só Director. Un ficheiro que non se poida copiar queda como
// aviso no resultado e non impide o cambio; se non se pode gardar o backup, o grupo non
// se cambia.
func (a *App) ChangeUserGroup(username, newGroup string, plan []MigracionMateria) (*CambioGrupoResult, error) {
	username = strings.TrimSpace(username)
	newGroup = strings.Join(gruposDe(newGroup), ", ")
	if username == "" {
		return nil, fmt.Errorf("indica o usuario")
	}
	if newGroup == "" {
		return nil, fmt.Errorf("indica o grupo novo")
	}
	if strings.EqualFold(username, directorUser) {
		return nil, fmt.Errorf("a conta de administración non ten grupo")
	}
	for _, m := range plan {
		if _, err := workSegment(m.From, "a materia de orixe"); err != nil {
			return nil, err
		}
		if _, err := workSegment(m.To, "a materia de destino"); err != nil {
			return nil, err
		}
		for de, para := range m.Datas {
			if _, err := workDate(de); err != nil {
				return nil, err
			}
			if _, err := workDate(para); err != nil {
				return nil, err
			}
		}
	}

	res := &CambioGrupoResult{}
	err := a.withAdminFB(func() error {
		if _, err := a.fbUserIDByName(username); err != nil {
			return err
		}
		if a.fbIsProfessor(username) {
			return fmt.Errorf("%s é profesor: o grupo só se cambia ao alumnado", username)
		}

		// Identidade: o roster é a fonte de verdade; o perfil local cobre as contas
		// creadas nesta máquina que aínda non estivesen nel.
		entry, ok := a.fbRosterEntry(username)
		perfil, perfilOK := a.perfilLocal(username)
		if !ok {
			if !perfilOK {
				return fmt.Errorf("non se atopou a %s no roster (alumnos.json) nin nos perfís locais", username)
			}
			entry = RosterEntry{Name: perfil.Name, Surname: perfil.Surname}
		}

		// Primeiro o traballo, despois o grupo: se a copia rompe a medias, o alumno
		// segue no grupo vello vendo todo o seu, e o Director pode volver tentalo.
		if len(plan) > 0 {
			materias, err := a.listMateriasRaw()
			if err != nil {
				return fmt.Errorf("non se puideron ler as materias: %w", err)
			}
			existe := map[string]bool{}
			for _, m := range materias {
				existe[m.ID] = true
			}
			subjects, err := a.loadStudentSubjects(username)
			if err != nil {
				return err
			}
			cambiouBackup := false
			for _, m := range plan {
				if !existe[m.From] || !existe[m.To] {
					res.Avisos = append(res.Avisos, fmt.Sprintf("a materia %s ou %s xa non existe; non se copiou", m.From, m.To))
					continue
				}
				var n int
				subjects, n = migrarRexistros(subjects, m)
				res.Rexistros += n
				cambiouBackup = cambiouBackup || n > 0
				for _, raiz := range []string{fbWorkRoot, fbCommentsRoot} {
					copiados, avisos := a.migrarFicheiros(username, raiz, m)
					res.Ficheiros += copiados
					res.Avisos = append(res.Avisos, avisos...)
				}
				res.Materias++
			}
			if cambiouBackup {
				if err := a.saveStudentSubjectsDirect(username, subjects); err != nil {
					return fmt.Errorf("non se puido gardar o traballo copiado; o grupo non se cambiou: %w", err)
				}
			}
		}

		vello := entry.Group
		entry.Group = newGroup
		if err := a.fbSetRosterEntry(username, entry); err != nil {
			return fmt.Errorf("non se puido actualizar o roster do servidor: %w", err)
		}
		if perfilOK {
			if err := a.saveFullIdentity(username, perfil.Name, perfil.Surname, newGroup, perfil.Email, perfil.UUID); err != nil {
				log.Printf("[admin] grupo de %q cambiado no servidor, pero non no perfil local: %v", username, err)
			}
		}
		log.Printf("[admin] grupo de %q: %q -> %q (%d materia(s), %d rexistro(s), %d ficheiro(s) copiados)",
			username, vello, newGroup, res.Materias, res.Rexistros, res.Ficheiros)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// perfilLocal devolve o rexistro de username en profiles.json, se o hai.
func (a *App) perfilLocal(username string) (storedProfile, bool) {
	profiles, _ := a.loadProfiles()
	for _, p := range profiles {
		if strings.EqualFold(p.Username, username) {
			return p, true
		}
	}
	return storedProfile{}, false
}

// saveStudentSubjectsDirect é saveStudentSubjects sen a cola offline: nunha sesión de
// administrador prestada (withAdminFB) un envío encolado executaríase despois co token
// doutra sesión, así que aquí ou se garda agora ou se informa do erro.
func (a *App) saveStudentSubjectsDirect(username string, subjects []map[string]any) error {
	out, err := json.MarshalIndent(subjects, "", "  ")
	if err != nil {
		return err
	}
	return a.fbUploadDirect(fbUsersRoot+"/"+username+"/"+fbBackupName, out)
}

// migrarRexistros copia ao subject m.To do backup os rexistros, informes de unidade e
// informes mensuais do subject m.From, traducindo as datas segundo m.Datas. Nunca pisa
// unha sesión do destino que xa teña contido, nin un informe que xa exista. Devolve os
// subjects (co destino engadido se non existía) e cantas sesións copiou.
func migrarRexistros(subjects []map[string]any, m MigracionMateria) ([]map[string]any, int) {
	var orixe, destino map[string]any
	for _, s := range subjects {
		switch id, _ := s["id"].(string); id {
		case m.From:
			orixe = s
		case m.To:
			destino = s
		}
	}
	if orixe == nil {
		return subjects, 0
	}
	if destino == nil {
		destino = map[string]any{"id": m.To, "logs": map[string]any{}}
		subjects = append(subjects, destino)
	}
	logsDestino, _ := destino["logs"].(map[string]any)
	if logsDestino == nil {
		logsDestino = map[string]any{}
		destino["logs"] = logsDestino
	}

	copiados := 0
	logsOrixe, _ := orixe["logs"].(map[string]any)
	for data, v := range logsOrixe {
		entrada, ok := v.(map[string]any)
		if !ok || entrada["_profReprog"] != nil {
			continue
		}
		nova := data
		if d, ok := m.Datas[data]; ok {
			nova = d
		}
		if existente, ok := logsDestino[nova].(map[string]any); ok && !rexistroBaleiro(existente) {
			continue
		}
		copia := make(map[string]any, len(entrada))
		for k, val := range entrada {
			copia[k] = val
		}
		copia["date"] = nova
		logsDestino[nova] = copia
		copiados++
	}

	for _, clave := range []string{"unitReports", "monthlyReports"} {
		de, _ := orixe[clave].(map[string]any)
		if len(de) == 0 {
			continue
		}
		para, _ := destino[clave].(map[string]any)
		if para == nil {
			para = map[string]any{}
			destino[clave] = para
		}
		for k, v := range de {
			if _, xa := para[k]; !xa {
				para[k] = v
			}
		}
	}
	return subjects, copiados
}

// rexistroBaleiro indica se unha sesión non ten nada do alumno nin do profesor que
// perder: sen contido, sen comentario e sen incidencias (p.ex. o esbozo que crea
// logEntryFor).
func rexistroBaleiro(e map[string]any) bool {
	if c, _ := e["content"].(string); strings.TrimSpace(c) != "" {
		return false
	}
	if c, _ := e["profesorComment"].(string); strings.TrimSpace(c) != "" {
		return false
	}
	if inc, _ := e["incidents"].([]any); len(inc) > 0 {
		return false
	}
	return true
}

// migrarFicheiros copia users/<login>/<raiz>/<m.From>/<data>/* a
// users/<login>/<raiz>/<m.To>/<data nova>/*, sen sobrescribir ficheiros que xa estean
// no destino. Devolve cantos copiou e os avisos dos que non puido.
func (a *App) migrarFicheiros(username, raiz string, m MigracionMateria) (int, []string) {
	base := fbUsersRoot + "/" + username + "/" + raiz
	dias, err := a.fbList(base + "/" + m.From)
	if err != nil {
		if errors.Is(err, errFBNonAtopado) {
			return 0, nil // non hai ficheiros desa materia
		}
		return 0, []string{fmt.Sprintf("%s/%s: non se puido listar (%v)", raiz, m.From, err)}
	}
	copiados := 0
	var avisos []string
	for _, dia := range dias {
		if !dia.IsDir {
			continue
		}
		nova := dia.Name
		if d, ok := m.Datas[dia.Name]; ok {
			nova = d
		}
		dirOrixe := base + "/" + m.From + "/" + dia.Name
		dirDestino := base + "/" + m.To + "/" + nova
		ficheiros, err := a.fbList(dirOrixe)
		if err != nil {
			avisos = append(avisos, fmt.Sprintf("%s: non se puido listar (%v)", dirOrixe, err))
			continue
		}
		xaHai := map[string]bool{}
		if existentes, err := a.fbList(dirDestino); err == nil {
			for _, f := range existentes {
				xaHai[f.Name] = true
			}
		}
		dirCreado := false
		for _, f := range ficheiros {
			if f.IsDir || xaHai[f.Name] {
				continue
			}
			contido, err := a.fbRawGet(dirOrixe + "/" + f.Name)
			if err != nil {
				avisos = append(avisos, fmt.Sprintf("%s/%s: non se puido ler (%v)", dirOrixe, f.Name, err))
				continue
			}
			if !dirCreado {
				if err := a.fbEnsureDirsDirect(dirDestino); err != nil {
					avisos = append(avisos, fmt.Sprintf("%s: non se puido crear (%v)", dirDestino, err))
					break
				}
				dirCreado = true
			}
			if err := a.fbUploadDirect(dirDestino+"/"+f.Name, contido); err != nil {
				avisos = append(avisos, fmt.Sprintf("%s/%s: non se puido copiar (%v)", dirDestino, f.Name, err))
				continue
			}
			copiados++
		}
	}
	return copiados, avisos
}

// fbEnsureDirsDirect crea dir e todos os pais que falten (o mkdir de File Browser só
// crea o último tramo), sen a cola offline — ver saveStudentSubjectsDirect.
func (a *App) fbEnsureDirsDirect(dir string) error {
	tramos := strings.Split(strings.Trim(dir, "/"), "/")
	for i := range tramos {
		if err := a.fbEnsureDirDirect(strings.Join(tramos[:i+1], "/")); err != nil {
			return err
		}
	}
	return nil
}
