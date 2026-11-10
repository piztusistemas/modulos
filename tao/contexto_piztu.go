package main

import (
	"encoding/json"
	"os"
)

// contextoPiztu is the subset of the contract Piztu writes when it launches
// Tao (see ContextoModulo/escribirContexto in piztu/app.go) — same
// pattern as yang/contexto.go. BaseDir locates Yang's executable when the
// professor asks to edit a session's task material with it (see
// yang_launch.go); Idioma is the professorado's language choice for Piztu
// itself, used as Tao's own default (see IdiomaPiztu, app.go). Nothing else
// from that contract is read here.
type contextoPiztu struct {
	BaseDir string `json:"base_dir"`
	Idioma  string `json:"idioma"`
}

// lerContextoPiztu reads the context Piztu wrote when it opened Tao
// (PIZTU_CONTEXTO env var). ok=false when Tao was opened without going
// through Piztu (development, running the binary by hand) — in that case
// there's no known base_dir and AbrirYangParaTarefa simply can't locate
// Yang, same "no context, feature quietly unavailable" criterion as Yang's
// own lerContextoLanzamento.
func lerContextoPiztu() (contextoPiztu, bool) {
	var ctx contextoPiztu
	ruta := os.Getenv("PIZTU_CONTEXTO")
	if ruta == "" {
		return ctx, false
	}
	dados, err := os.ReadFile(ruta)
	if err != nil {
		return ctx, false
	}
	if err := json.Unmarshal(dados, &ctx); err != nil {
		return ctx, false
	}
	if ctx.BaseDir == "" {
		return ctx, false
	}
	return ctx, true
}
