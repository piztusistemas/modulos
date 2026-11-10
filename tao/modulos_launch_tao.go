package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Contexto de lanzamento para os módulos externos DE Tao — un pipe
// DISTINTO do contextoYang/--tao-contexto/TAO_CONTEXTO (yang_launch.go),
// que segue igual e exclusivo do handoff Tao→Yang. Este é xenérico, para
// calquera módulo externo: quen é (ModuloID/ModuloDir), onde está a API de
// Tao e con que token/permisos pode falarlle (EmitirTokenModulo), e o
// idioma por defecto. Non leva BaseDir/Motor/Equipos coma ContextoModulo de
// Piztu (piztu/app.go) — non teñen sentido en Tao.
type ContextoModuloTao struct {
	Version     int      `json:"version"`
	ModuloID    string   `json:"modulo_id"`
	ModuloDir   string   `json:"modulo_dir"`
	APISocket   string   `json:"api_socket"`
	APIToken    string   `json:"api_token,omitempty"`
	APIPermisos []string `json:"api_permisos,omitempty"`
	Idioma      string   `json:"idioma"`
}

// escribirContextoModuloTao leaves the context in a fresh temp file (system
// temp dir, one-shot handoff — mesmo criterio ca escribirContextoYang),
// 0600 porque leva o token da API en claro.
func escribirContextoModuloTao(ctx ContextoModuloTao) (string, error) {
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "tao-contexto-modulo-*.json")
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

// lanzarAplicacionModuloTao starts the módulo as an independent process,
// passing the context by argument AND environment variable — mesmo patrón
// e mesmo motivo ca lanzarYang/lanzarAplicacion de Piztu (macOS .app
// bundles launched via `open` disconnect the process, so stdin can't be
// relied on).
func lanzarAplicacionModuloTao(exePath, rutaContexto string) error {
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("non se atopa o executable do módulo (%s): %w", exePath, err)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && filepath.Ext(exePath) == ".app" {
		cmd = exec.Command("open", "-n", exePath, "--args", "--tao-modulo-contexto", rutaContexto)
	} else {
		cmd = exec.Command(exePath, "--tao-modulo-contexto", rutaContexto)
		cmd.Dir = filepath.Dir(exePath)
	}
	cmd.Env = append(os.Environ(), "TAO_MODULO_CONTEXTO="+rutaContexto)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // avoid zombies: Tao stays open long after the módulo exits
	return nil
}
