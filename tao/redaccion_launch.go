package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
)

// Pipe Tao→Malta PARA O INFORME ASINADO (ReportBuilder.tsx, "Redacción por
// IA") — distinto e paralelo ao de malta_launch.go (informes de seguimento,
// unidade/mensual): mesmo transporte e mesmo patrón de proceso independente
// (ver lanzarMalta), pero cun contexto e un contrato de retorno propios (ver
// redaccion.go en tao/modulos/malta), porque non hai unidades didácticas
// aquí, só o rexistro diario xa escrito que hai que redactar en prosa.

// contextoTarefaRedaccion é o que Tao escribe cando lanza Malta para
// redactar un informe asinado. Espello de ContextoTarefaRedaccion en
// tao/modulos/malta/redaccion.go — dous binarios distintos, sen paquete Go
// compartido, mesmo motivo cá contextoTarefaMalta de malta_launch.go.
type contextoTarefaRedaccion struct {
	RequestID             string                  `json:"requestId"`
	MateriaNome           string                  `json:"materiaNome"`
	AcademicYear          string                  `json:"academicYear"`
	ScopeDescription      string                  `json:"scopeDescription"`
	Entradas              []EntradaRedaccionMalta `json:"entradas"`
	PeticionPersonalizada string                  `json:"peticionPersonalizada,omitempty"`
	APISocket             string                  `json:"apiSocket"`
	APIToken              string                  `json:"apiToken"`
}

// EntradaRedaccionMalta é UN día do rexistro que hai que redactar — vén do
// frontend (ReportBuilder.tsx), xa filtrado polo alcance escollido.
type EntradaRedaccionMalta struct {
	Data    string `json:"data"`
	Status  string `json:"status"`
	Content string `json:"content"`
}

// AbrirMaltaRedaccion pide a Malta que redacte, coa axuda da IA, o texto en
// prosa do informe asinado a partir das entradas do alcance escollido.
// Devolve un requestId co que o frontend correlaciona a resposta asíncrona
// (evento "tao:malta-redaccion", ver redaccion_resultado.go) — pode chegar
// bastante despois, e pode haber máis dunha xanela de informe aberta.
func (a *App) AbrirMaltaRedaccion(materiaNome, academicYear, scopeDescription string, entradas []EntradaRedaccionMalta, peticionPersonalizada string) (string, error) {
	if err := a.requireProfessor(); err != nil {
		return "", err
	}
	if a.api == nil {
		return "", fmt.Errorf("a API local de Tao non está activa")
	}
	exePath, ok := localizarMalta()
	if !ok {
		return "", fmt.Errorf("non se atopa Malta instalado neste equipo (⚙ → Módulos)")
	}
	if !a.moduloTaoActivo("malta") {
		return "", fmt.Errorf("o módulo Malta está desactivado (⚙ → Módulos)")
	}
	if len(entradas) == 0 {
		return "", fmt.Errorf("non hai rexistros que redactar neste alcance")
	}

	requestID := uuid.NewString()
	ctx := contextoTarefaRedaccion{
		RequestID: requestID, MateriaNome: materiaNome, AcademicYear: academicYear,
		ScopeDescription: scopeDescription, Entradas: entradas, PeticionPersonalizada: peticionPersonalizada,
		APISocket: a.api.rutaSocket(), APIToken: a.api.token,
	}

	rutaContexto, err := escribirContextoTarefaRedaccion(ctx)
	if err != nil {
		return "", err
	}
	if err := lanzarMaltaRedaccion(exePath, rutaContexto); err != nil {
		return "", err
	}
	return requestID, nil
}

// escribirContextoTarefaRedaccion / lanzarMaltaRedaccion — mesmo patrón ca
// escribirContextoTarefaMalta/lanzarMalta (malta_launch.go), pero pasando o
// flag/variable de contorno propios desta tarefa (TAO_TAREFA_MALTA_ASINADO)
// para que Malta escolla o camiño headless correcto en main.go.
func escribirContextoTarefaRedaccion(ctx contextoTarefaRedaccion) (string, error) {
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "tao-tarefa-malta-asinado-*.json")
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

// lanzarMaltaRedaccion — mesmo patrón ca lanzarMalta (malta_launch.go), só
// que co flag/variable de contorno propios desta tarefa.
func lanzarMaltaRedaccion(exePath, rutaContexto string) error {
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("non se atopa o executable de Malta (%s): %w", exePath, err)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && filepath.Ext(exePath) == ".app" {
		cmd = exec.Command("open", "-n", exePath, "--args", "--tao-tarefa-malta-asinado", rutaContexto)
	} else {
		cmd = exec.Command(exePath, "--tao-tarefa-malta-asinado", rutaContexto)
		cmd.Dir = filepath.Dir(exePath)
	}
	cmd.Env = append(os.Environ(), "TAO_TAREFA_MALTA_ASINADO="+rutaContexto)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // avoid zombies: Tao stays open long after Malta exits
	return nil
}
