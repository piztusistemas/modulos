package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Pipe Tao→Malta: distinto do subsistema xeral de módulos externos de Tao
// (modulos_tao.go/modulos_launch_tao.go, usado só para a apertura normal de
// Malta → configuración de IA). Mesmo patrón có pipe Tao→Yang xa existente
// (yang_launch.go, docs/api-tao.md §10), pero SEN interface: Malta procesa
// e remata sen amosar ningunha xanela — ver docs/api-tao.md §12.

// contextoTarefaMalta é o que Tao escribe cando lanza Malta para xerar UN
// informe de forma autónoma. Espello de ContextoTarefaMalta en
// tao/modulos/malta/tarefa.go — dous binarios distintos, sen paquete Go
// compartido (mesmo motivo có resto do ecosistema, ver modulos_launch_tao.go).
type contextoTarefaMalta struct {
	Tipo                  string     `json:"tipo"` // "unidade" | "mensual"
	Materia               MateriaDef `json:"materia"`
	RangoFrom             string     `json:"rangoFrom,omitempty"`
	RangoTo               string     `json:"rangoTo,omitempty"`
	UnitIDObxectivo       int        `json:"unitIdObxectivo,omitempty"` // só "unidade"
	PeticionPersonalizada string     `json:"peticionPersonalizada,omitempty"`
	APISocket             string     `json:"apiSocket"`
	APIToken              string     `json:"apiToken"` // token ESTÁTICO de proceso — mesmo cá do pipe Tao→Yang, non un de módulo
}

// maltaModuloManifest é o subconxunto de modulo.json que precisamos para
// atopar o executable de Malta — mesmo patrón ca yangModuloManifest
// (yang_launch.go).
type maltaModuloManifest struct {
	Executable string `json:"executable"`
}

// localizarMalta busca Malta en <taoInstallDir>/modulos/malta (o
// subsistema de módulos DE Tao, modulos_tao.go — non o de Piztu).
func localizarMalta() (string, bool) {
	dir, err := taoModulosDir()
	if err != nil {
		return "", false
	}
	moduloDir := filepath.Join(dir, "malta")
	data, err := os.ReadFile(filepath.Join(moduloDir, "modulo.json"))
	if err != nil {
		return "", false
	}
	var m maltaModuloManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Executable == "" {
		return "", false
	}
	exePath := filepath.Join(moduloDir, m.Executable)
	if _, err := os.Stat(exePath); err != nil {
		return "", false
	}
	return exePath, true
}

// MaltaDispoñible reporta se Malta está instalado E activado (mesmo
// criterio cós demais módulos de Tao, ver moduloTaoActivo) — ReportsModal
// úsao para amosar ou non os botóns "Xerar con IA". Non esixe sesión de
// profesor: é unha simple comprobación de dispoñibilidade, coma
// YangDispoñible.
func (a *App) MaltaDispoñible() bool {
	if !a.moduloTaoActivo("malta") {
		return false
	}
	_, ok := localizarMalta()
	return ok
}

// materiaCompleta resolve unha MateriaDef por ID a partir de ListMaterias
// (profesor.go) — a mesma que Tao xa usa para o resto da app, reempaquetada
// enteira para Malta (Config/Units/Logs/ProgramacionInfo completos: Malta
// non chama a ningún endpoint para obter datos, ver docs/api-tao.md §12.3).
func (a *App) materiaCompleta(materiaID string) (MateriaDef, error) {
	materias, err := a.ListMaterias()
	if err != nil {
		return MateriaDef{}, err
	}
	for _, m := range materias {
		if m.ID == materiaID {
			return m, nil
		}
	}
	return MateriaDef{}, fmt.Errorf("non se atopou a materia %q", materiaID)
}

// lanzarMaltaTarefa é o núcleo compartido por AbrirMaltaUnidade/
// AbrirMaltaMensual: valida, resolve o token/socket actuais de Tao e
// arrinca Malta headless.
func (a *App) lanzarMaltaTarefa(ctxTarefa contextoTarefaMalta) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if a.api == nil {
		return fmt.Errorf("a API local de Tao non está activa")
	}
	exePath, ok := localizarMalta()
	if !ok {
		return fmt.Errorf("non se atopa Malta instalado neste equipo (⚙ → Módulos)")
	}
	if !a.moduloTaoActivo("malta") {
		return fmt.Errorf("o módulo Malta está desactivado (⚙ → Módulos)")
	}

	ctxTarefa.APISocket = a.api.rutaSocket()
	ctxTarefa.APIToken = a.api.token

	rutaContexto, err := escribirContextoTarefaMalta(ctxTarefa)
	if err != nil {
		return err
	}
	return lanzarMalta(exePath, rutaContexto)
}

// AbrirMaltaUnidade pide a Malta o informe de seguimento dunha unidade
// didáctica concreta ("Seguimento por unidade", petición expresa).
func (a *App) AbrirMaltaUnidade(materiaID string, unitID int, peticionPersonalizada string) error {
	materia, err := a.materiaCompleta(materiaID)
	if err != nil {
		return err
	}
	return a.lanzarMaltaTarefa(contextoTarefaMalta{
		Tipo: "unidade", Materia: materia, UnitIDObxectivo: unitID,
		PeticionPersonalizada: peticionPersonalizada,
	})
}

// AbrirMaltaMensual pide a Malta o informe mensual ("Informe mensual")
// para o rango [rangoFrom, rangoTo].
func (a *App) AbrirMaltaMensual(materiaID, rangoFrom, rangoTo, peticionPersonalizada string) error {
	materia, err := a.materiaCompleta(materiaID)
	if err != nil {
		return err
	}
	return a.lanzarMaltaTarefa(contextoTarefaMalta{
		Tipo: "mensual", Materia: materia, RangoFrom: rangoFrom, RangoTo: rangoTo,
		PeticionPersonalizada: peticionPersonalizada,
	})
}

// escribirContextoTarefaMalta / lanzarMalta — mesmo patrón ca
// escribirContextoYang/lanzarYang (yang_launch.go).
func escribirContextoTarefaMalta(ctx contextoTarefaMalta) (string, error) {
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "tao-tarefa-malta-*.json")
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

// lanzarMalta starts Malta as an independent process, passing the context
// by argument AND environment variable — mesmo patrón e mesmo motivo ca
// lanzarYang (macOS .app bundles launched via `open` disconnect the
// process, so stdin can't be relied on).
func lanzarMalta(exePath, rutaContexto string) error {
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("non se atopa o executable de Malta (%s): %w", exePath, err)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && filepath.Ext(exePath) == ".app" {
		cmd = exec.Command("open", "-n", exePath, "--args", "--tao-tarefa-malta", rutaContexto)
	} else {
		cmd = exec.Command(exePath, "--tao-tarefa-malta", rutaContexto)
		cmd.Dir = filepath.Dir(exePath)
	}
	cmd.Env = append(os.Environ(), "TAO_TAREFA_MALTA="+rutaContexto)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // avoid zombies: Tao stays open long after Malta exits
	return nil
}
