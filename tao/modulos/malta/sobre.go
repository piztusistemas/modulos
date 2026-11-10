package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SobreInfo é o que ve o frontend do botón "i" (Sobre Malta) — os metadatos
// que o propio módulo declara no seu modulo.json, mesmo patrón cás
// pantallas "Sobre Piztu" (piztu/frontend/src/main.js) e "Sobre Pancho"
// (pancho/frontend/src/main.js), pero sen comprobación de actualizacións:
// esa xa a fai Tao dende ⚙ → Módulos (modulos_binding_tao.go).
type SobreInfo struct {
	Nome              string `json:"nome"`
	Version           string `json:"version"`
	Autor             string `json:"autor"`
	Email             string `json:"email"`
	Web               string `json:"web"`
	Descricion        string `json:"descricion"`
	IconaImaxeDataURI string `json:"iconaImaxeDataURI"`
}

// manifestSobreMalta é o subconxunto de modulo.json que precisamos aquí —
// copiado en vez de compartido có de Tao (manifestTao en modulos_tao.go):
// dous binarios distintos, sen paquete Go compartido, mesmo motivo ca
// ContextoModuloTao en contexto.go.
type manifestSobreMalta struct {
	Nome       string `json:"nome"`
	IconaImaxe string `json:"iconaImaxe"`
	Version    string `json:"version"`
	Autor      string `json:"autor"`
	Email      string `json:"email"`
	Web        string `json:"web"`
	Descricion string `json:"descricion"`
}

// dirPropio devolve o cartafol onde vive o propio modulo.json de Malta: o
// que indicou Tao ao lanzala (a.contexto.ModuloDir, ver contexto.go) ou, se
// se abriu á man fóra de Tao (desenvolvemento), o cartafol do executable.
func (a *App) dirPropio() string {
	if a.contexto.ModuloDir != "" {
		return a.contexto.ModuloDir
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// GetSobre exponse ao frontend para o modal "Sobre Malta" (botón ℹ na
// barra) — le o propio modulo.json e o logo que declara, coma
// iconaImaxeDataURITao (tao/modulos_tao.go) pero lido dende dentro do
// propio proceso: nunca falla en silencio coma alí, se non hai nada que
// amosar o frontend cae no nome de reserva "Malta".
func (a *App) GetSobre() SobreInfo {
	info := SobreInfo{Nome: "Malta"}
	dir := a.dirPropio()
	if dir == "" {
		return info
	}
	dados, err := os.ReadFile(filepath.Join(dir, "modulo.json"))
	if err != nil {
		return info
	}
	var m manifestSobreMalta
	if err := json.Unmarshal(dados, &m); err != nil {
		return info
	}
	if nome := strings.TrimSpace(m.Nome); nome != "" {
		info.Nome = nome
	}
	info.Version = strings.TrimSpace(m.Version)
	info.Autor = m.Autor
	info.Email = m.Email
	info.Web = m.Web
	info.Descricion = m.Descricion
	info.IconaImaxeDataURI = imaxeDataURI(dir, m.IconaImaxe)
	return info
}

// imaxeDataURI le <dir>/<rel> e devólvea coma data URI autocontida — "" se
// rel está baleiro ou o ficheiro non se pode ler (módulo mal configurado ou
// sen logo propio: o frontend cae no emoji de sempre, non rompe nada).
func imaxeDataURI(dir, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	dados, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return ""
	}
	mime := "image/png"
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".svg":
		mime = "image/svg+xml"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".gif":
		mime = "image/gif"
	case ".webp":
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(dados)
}
