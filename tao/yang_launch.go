package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// contextoYang is what Tao hands to Yang when launching it to edit a
// session's task material — the professor-facing entry point of the
// Tao↔Yang pipe (see docs/api-tao.md, yang/docs/api-yang.md). Yang reads it
// via the TAO_CONTEXTO env var (yang/contexto_tao.go), symmetric to how
// Piztu hands PIZTU_CONTEXTO to any módulo it launches (see
// piztu/app.go, escribirContexto). Yang only activates the "push to Tao"
// flow (yang/taoclient.go) when this is present — opened any other way
// (Piztu's map, by hand), Yang behaves exactly as before, no coupling.
type contextoYang struct {
	MateriaID string `json:"materia_id"`
	Date      string `json:"date"`
	APISocket string `json:"api_socket"`
	APIToken  string `json:"api_token"`
	// Alumnos is the roster for this materia (docs/api-tao.md §11.4) — what lets
	// Yang's "só dentro do ecosistema" reparto UI suggest Iteracións=len(Alumnos)
	// and pair each generated variant with a real student. Absent/empty on any
	// error resolving the roster: Yang degrades to its normal behaviour, it never
	// blocks the launch (see alumnosDaMateria).
	Alumnos []alumnoContexto `json:"alumnos,omitempty"`
	// Username, when set, means Tao launched Yang to edit ONE student's
	// individual override (App.tsx modal de excepcións, "Editar con Yang"
	// dentro do editor dese alumno) instead of the general task — see
	// AbrirYangParaTarefaAlumno. Yang's UI (yang/tarefa_tao.go) then pushes
	// with EnviarTarefaAlumno straight to this student, and hides the
	// "Repartir" button (non ten sentido repartir cando xa se edita un só).
	Username string `json:"username,omitempty"`
}

// alumnoContexto is one student handed to Yang — enough to show a real name in the
// reparto UI, not just a login (docs/api-tao.md §11.4).
type alumnoContexto struct {
	Username string `json:"username"`
	Nome     string `json:"nome"`
}

// alumnosDaMateria resolves the students enrolled in materiaID — roster entries
// whose Group is one of the materia's Groups, the same matching rule already used
// by GetMyMaterias (profesor.go), EXCLUDING anyone listed in profesores.json
// (fbIsProfessor). Necesario porque alumnos.json non distingue rol — calquera
// conta (alumnado OU profesorado) que teña un Group asignado aparece en
// ListRoster por igual; un profesor matriculado casualmente no mesmo grupo (ex.
// para probas) non debe recibir a súa propia variante coma se fose alumnado.
//
// NOT a general roster dump: docs/api-tao.md §8 explicitly forbids exposing
// ListRoster to módulos over the socket API. This is a DIFFERENT circuit — the
// same one-shot Yang-launch pipe already documented in §10, triggered directly
// by the professor for ONE materia/session, never discoverable or reusable by
// any other process the way tao-api.json is.
func (a *App) alumnosDaMateria(materiaID string) ([]alumnoContexto, error) {
	materias, err := a.ListMaterias()
	if err != nil {
		return nil, err
	}
	var grupos []string
	for _, m := range materias {
		if m.ID == materiaID {
			grupos = m.Groups
			break
		}
	}
	if len(grupos) == 0 {
		return nil, nil // materia descoñecida ou sen grupos: sen alumnado que repartir
	}
	roster, err := a.ListRoster()
	if err != nil {
		return nil, err
	}
	var profs []string
	a.fbReadSharedJSON(sharedProfesores, &profs) // best-effort: lido unha soa vez, non por fila
	esProfesor := func(username string) bool {
		for _, p := range profs {
			if strings.EqualFold(strings.TrimSpace(p), username) {
				return true
			}
		}
		return false
	}
	var out []alumnoContexto
	for _, r := range roster {
		if esProfesor(r.Username) {
			continue
		}
		for _, g := range grupos {
			if !pertenceAGrupo(r.Group, g) {
				continue
			}
			nome := strings.TrimSpace(r.Name + " " + r.Surname)
			if nome == "" {
				nome = r.Username
			}
			out = append(out, alumnoContexto{Username: r.Username, Nome: nome})
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nome < out[j].Nome })
	return out, nil
}

// yangModuloManifest is the subset of modulo.json this file needs to find
// Yang's actual executable name — same field as
// piztu/internal/modulos.manifest, reimplemented here since tao and
// piztu are independent Go modules/binaries with no shared package.
type yangModuloManifest struct {
	Executable string `json:"executable"`
}

// destinoYangStandalone é onde aterra Yang cando se instala SEN Piztu neste
// equipo (yang_installer/install.go, destDir) — deliberadamente o MESMO
// camiño ca módulo de Piztu (yang_installer/install.go xa o di: "instalar
// Yang standalone e instalalo coma módulo son agora o mesmo destino"). Por
// iso abonda con probar este camiño fixo coma segunda alternativa: cobre os
// dous casos que pide o profesorado (Yang coma módulo de Piztu, e Yang
// instalado á parte nun equipo sen Piztu) coa mesma comprobación.
const destinoYangStandalone = "/opt/piztu/modulos/yang"

// YangDispoñible reports whether Yang can be found on THIS machine, coa
// mesma busca ca AbrirYangParaTarefa — para que o frontend só amose o botón
// "Editar con Yang" cando hai algo real que abrir (TaskFilesSection, ver
// App.tsx). Non esixe sesión de profesor: é unha simple comprobación de
// dispoñibilidade, sen efectos secundarios.
func (a *App) YangDispoñible() bool {
	_, ok := a.localizarYang()
	return ok
}

// localizarYang busca o executable de Yang en dous sitios posibles, nesta
// orde:
//  1. <base_dir>/modulos/yang — Yang coma módulo dunha instalación de Piztu
//     que Tao coñece (base_dir vén de PIZTU_CONTEXTO, ver contexto_piztu.go).
//  2. destinoYangStandalone — camiño fixo onde tamén aterra unha instalación
//     independente de Yang (yang_installer), mesmo nun equipo SEN Piztu.
//
// Devolve a primeira que exista de verdade (modulo.json + executable
// presente en disco), non simplemente a primeira candidata construída.
func (a *App) localizarYang() (string, bool) {
	var candidatos []string
	if a.baseDir != "" {
		candidatos = append(candidatos, filepath.Join(a.baseDir, "modulos", "yang"))
	}
	if len(candidatos) == 0 || candidatos[0] != destinoYangStandalone {
		candidatos = append(candidatos, destinoYangStandalone)
	}
	for _, dir := range candidatos {
		if exePath, ok := yangExecutable(dir); ok {
			return exePath, true
		}
	}
	return "", false
}

// AbrirYangParaTarefa launches Yang so the professor can author a session's
// task material, wired (via contexto_tao.go on Yang's side) to push the
// resulting Markdown straight back into this session's "Tarefa" tab
// (SaveTaskDescription, traballos.go) the instant Yang generates it — see
// docs/api-tao.md and yang/docs/api-yang.md. Professor only, same gate as
// SaveTaskDescription itself: no point opening Yang for a tarefa this Tao
// session couldn't save anyway.
func (a *App) AbrirYangParaTarefa(materiaID, date string) error {
	return a.abrirYangParaTarefaImpl(materiaID, date, "")
}

// AbrirYangParaTarefaAlumno is AbrirYangParaTarefa but for ONE student's
// individual override (App.tsx modal de excepcións) — same pipe, Yang just
// pushes back with EnviarTarefaAlumno instead of EnviarTarefa (see
// contextoYang.Username, yang/tarefa_tao.go). Professor only, same gate as
// SaveTaskDescriptionAlumno itself.
func (a *App) AbrirYangParaTarefaAlumno(materiaID, date, username string) error {
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("falta o alumno para o que editar a tarefa")
	}
	return a.abrirYangParaTarefaImpl(materiaID, date, username)
}

func (a *App) abrirYangParaTarefaImpl(materiaID, date, username string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if a.api == nil {
		return fmt.Errorf("a API local de Tao non está activa")
	}

	exePath, ok := a.localizarYang()
	if !ok {
		return fmt.Errorf("non se atopa Yang instalado neste equipo (nin coma módulo de Piztu, nin instalado á parte en %s)", destinoYangStandalone)
	}

	alumnos, err := a.alumnosDaMateria(materiaID)
	if err != nil {
		// Best-effort a propósito: sen lista de alumnos, Yang simplemente non
		// suxire iteracións nin ofrece reparto — non hai motivo para impedir
		// abrir Yang por isto (ex. fbList transitorio, materia sen grupos).
		log.Printf("[yang] non se puido resolver o alumnado de %q: %v", materiaID, err)
	}

	rutaContexto, err := escribirContextoYang(contextoYang{
		MateriaID: materiaID,
		Date:      date,
		APISocket: a.api.rutaSocket(),
		APIToken:  a.api.token,
		Alumnos:   alumnos,
		Username:  username,
	})
	if err != nil {
		return err
	}

	return lanzarYang(exePath, rutaContexto)
}

// yangExecutable reads Yang's own modulo.json to know its executable
// filename — don't hardcode "yang", modulo.json is the single source of
// truth (see piztu/internal/modulos.go). ok=false covers both "no
// modulo.json here" and "modulo.json exists but the executable it names
// isn't actually on disk" (half-finished install) — either way, not a
// usable Yang.
func yangExecutable(moduloDir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(moduloDir, "modulo.json"))
	if err != nil {
		return "", false
	}
	var m yangModuloManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Executable == "" {
		return "", false
	}
	exePath := filepath.Join(moduloDir, m.Executable)
	if _, err := os.Stat(exePath); err != nil {
		return "", false
	}
	return exePath, true
}

// escribirContextoYang leaves the context in a fresh temp file (system temp
// dir, not a.dataDir: this is a one-shot handoff for a single launch, not
// data Tao needs to keep around) and returns its path. 0600 because it
// carries the API token in clear, same criterion as tao-api.json
// (api_discovery.go).
func escribirContextoYang(ctx contextoYang) (string, error) {
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "tao-contexto-yang-*.json")
	if err != nil {
		return "", err
	}
	ruta := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}
	if err := os.Chmod(ruta, 0o600); err != nil {
		return "", err
	}
	return ruta, nil
}

// lanzarYang starts Yang as an independent process, passing the context by
// argument and environment variable — same pattern and same reason as
// piztu's lanzarAplicacion (app.go): a file plus both conventions works
// across OSes without depending on stdin (macOS .app bundles launched via
// `open` disconnect the process from Tao's).
func lanzarYang(exePath, rutaContexto string) error {
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("non se atopa o executable de Yang (%s): %w", exePath, err)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && filepath.Ext(exePath) == ".app" {
		cmd = exec.Command("open", "-n", exePath, "--args", "--tao-contexto", rutaContexto)
	} else {
		cmd = exec.Command(exePath, "--tao-contexto", rutaContexto)
		cmd.Dir = filepath.Dir(exePath)
	}
	cmd.Env = append(os.Environ(), "TAO_CONTEXTO="+rutaContexto)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // avoid zombies: Tao stays open long after Yang exits
	return nil
}
