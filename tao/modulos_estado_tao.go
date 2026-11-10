package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Persistencia local de activo/inactivo + permisos concedidos por módulo
// externo de Tao. Tao non ten SQLite nin unha abstración "Axustes" xenérica
// coma Piztu (internal/db, táboa axuste) — sigue o mesmo patrón xa usado
// para outro estado local propio (tao-api.json, profiles.json): un ficheiro
// JSON plano en a.dataDir.

const nomeFicheiroModulosEstadoTao = "modulos_tao.json"

// estadoModuloTao é o que se persiste por módulo: se está activo, e que
// permisos ten concedidos (copia do Permisos do modulo.json no momento de
// activar — coma Piztu, desactivar borra os permisos concedidos, non abonda
// con marcar activo=false).
type estadoModuloTao struct {
	Activo   bool     `json:"activo"`
	Permisos []string `json:"permisos,omitempty"`
}

func (a *App) modulosEstadoPath() string {
	return filepath.Join(a.dataDir, nomeFicheiroModulosEstadoTao)
}

// lerEstadoModulosTao é best-effort: un ficheiro ausente ou ilexible devolve
// un mapa baleiro (todos os módulos contan coma inactivos), nunca erro.
func (a *App) lerEstadoModulosTao() map[string]estadoModuloTao {
	estado := map[string]estadoModuloTao{}
	datos, err := os.ReadFile(a.modulosEstadoPath())
	if err != nil {
		return estado
	}
	_ = json.Unmarshal(datos, &estado)
	if estado == nil {
		estado = map[string]estadoModuloTao{}
	}
	return estado
}

func (a *App) gardarEstadoModulosTao(estado map[string]estadoModuloTao) error {
	datos, err := json.MarshalIndent(estado, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.modulosEstadoPath(), datos, 0o644)
}

// moduloTaoActivo: id descoñecido conta coma inactivo, mesmo criterio ca
// ActivoPorID en piztu/internal/modulos.
func (a *App) moduloTaoActivo(id string) bool {
	return a.lerEstadoModulosTao()[id].Activo
}

// setModuloTaoActivo persiste o novo estado (activo + permisos concedidos
// nesta mesma operación — máis simple que as dúas claves separadas de Piztu,
// que existen alí porque o almacén subxacente é un par chave-valor xenérico;
// aquí non hai esa restrición).
func (a *App) setModuloTaoActivo(id string, activo bool, permisos []string) error {
	estado := a.lerEstadoModulosTao()
	if !activo {
		estado[id] = estadoModuloTao{Activo: false}
	} else {
		estado[id] = estadoModuloTao{Activo: true, Permisos: permisos}
	}
	return a.gardarEstadoModulosTao(estado)
}
