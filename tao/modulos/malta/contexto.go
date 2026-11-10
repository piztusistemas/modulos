package main

import (
	"encoding/json"
	"os"
)

// ContextoModuloTao é o mesmo tipo que escribe Tao ao lanzar un módulo
// externo seu (ver tao/modulos_launch_tao.go — este é o único consumidor
// real que existe hoxe dese contrato). Copiado en vez de compartido por
// módulo Go: non hai SDK previo para este subsistema (é o primeiro módulo
// que o usa), e replicar catro campos non xustifica un módulo Go novo só
// para isto.
type ContextoModuloTao struct {
	Version     int      `json:"version"`
	ModuloID    string   `json:"modulo_id"`
	ModuloDir   string   `json:"modulo_dir"`
	APISocket   string   `json:"api_socket"`
	APIToken    string   `json:"api_token,omitempty"`
	APIPermisos []string `json:"api_permisos,omitempty"`
	Idioma      string   `json:"idioma"`
}

// lerContextoModuloTao le o contexto pola variable de contorno
// TAO_MODULO_CONTEXTO (mesmo criterio ca yang/contexto_tao.go: Tao pasa o
// mesmo contido tamén por argumento --tao-modulo-contexto, pero o env abonda
// sempre que Tao arrinca o proceso — ver lanzarAplicacionModuloTao). Devolve
// ok=false se Malta se abriu sen pasar por Tao (ex. execución á man en
// desenvolvemento) — nese caso non hai materias nin API dispoñibles.
func lerContextoModuloTao() (ContextoModuloTao, bool) {
	var ctx ContextoModuloTao
	ruta := os.Getenv("TAO_MODULO_CONTEXTO")
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
	return ctx, true
}
