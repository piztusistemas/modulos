package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Sistema de módulos externos DE Tao — clon, en pequeno, do que xa ten Piztu
// (piztu/internal/modulos): un cartafol de sub-módulos, cada un cun
// modulo.json, activables/desactivables á vontade, instalables dende un
// repositorio de GitHub (modulos_instalar_tao.go). Non confundir con Yang
// (yang_instalar.go/yang_launch.go): Yang é un caso único, cravado, que
// Piztu tamén recoñece coma módulo seu; isto é xenérico, para calquera
// terceiro que queira estender Tao, gardado á parte en
// <cartafol-de-Tao>/modulos/ e sen relación ningunha co cartafol de módulos
// de Piztu.
//
// Simplificación deliberada fronte a Piztu: sen ACCIÓNS/RECOLECTOR/PANEL (Tao
// non ten motor nin frota de equipos que xustifique esas capacidades) — só
// EXECUTABLE (proceso fillo lanzado por AbrirModuloTao) e PERMISOS (que
// endpoints da API de Tao pode chamar, ver api_auth_modulos.go).

// ModuloTao é un módulo externo xa descuberto en disco.
type ModuloTao struct {
	ID                string `json:"id"`
	Nome              string `json:"nome"`
	Icona             string `json:"icona"`             // emoji/texto curto
	IconaImaxeDataURI string `json:"iconaImaxeDataURI"` // "" se non declara IconaImaxe ou non se puido ler
	Executable        string `json:"executable"`
	Dir               string `json:"-"`
	Version           string `json:"version"`
	Autor             string `json:"autor"`
	Email             string `json:"email"`
	Web               string `json:"web"`
	// Descricion é un texto curto opcional que o módulo declara no seu
	// modulo.json, amosado no modal "Sobre" de ⚙ → Módulos (botón ℹ).
	Descricion string   `json:"descricion"`
	Permisos   []string `json:"permisos"`
}

// manifestTao é a forma de modulo.json dun módulo externo de Tao.
type manifestTao struct {
	ID         string   `json:"id"`
	Nome       string   `json:"nome"`
	Icona      string   `json:"icona"`
	IconaImaxe string   `json:"iconaImaxe"`
	Executable string   `json:"executable"`
	Version    string   `json:"version"`
	Autor      string   `json:"autor"`
	Email      string   `json:"email"`
	Web        string   `json:"web"`
	Descricion string   `json:"descricion"`
	Permisos   []string `json:"permisos"`
}

// taoInstallDir devolve o cartafol onde vive o executable de Tao (resolvendo
// symlinks) — mesmo patrón ca exeActual en actualizacion.go. Funciona igual
// tanto se Tao está instalado coma módulo de Piztu (/opt/piztu/modulos/tao)
// coma standalone (Tao_Yang_installer/install.go instala nese mesmo camiño
// relativo en ambos os dous casos: dirTao = destDir/modulos/tao).
func taoInstallDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// taoModulosDir devolve <taoInstallDir>/modulos, o cartafol dos módulos
// externos de Tao.
func taoModulosDir() (string, error) {
	dir, err := taoInstallDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "modulos"), nil
}

const leemeModulosTao = `Módulos externos de Tao
=======================

Cada módulo vive nun subcartafol propio con, polo menos, un ficheiro
modulo.json:

{
  "id": "exemplo",
  "nome": "Exemplo",
  "icona": "🧩",
  "executable": "exemplo",
  "version": "1.0",
  "permisos": ["materias.ler"]
}

"executable" é o único ficheiro que se lanza — mantén un só binario por
cartafol de módulo. "permisos" declara que endpoints da API de Tao pode
chamar este módulo unha vez activado (⚙ → Módulos); sen activar, é coma se
non estivese instalado.
`

// PrepararModulosTao crea dir se non existe e escribe un LEEME.md
// explicativo na primeira execución (non sobrescribe un xa existente).
func PrepararModulosTao(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	leeme := filepath.Join(dir, "LEEME.md")
	if _, err := os.Stat(leeme); os.IsNotExist(err) {
		_ = os.WriteFile(leeme, []byte(leemeModulosTao), 0o644)
	}
	return nil
}

// ExternosTao escanea dir en busca de subcartafoles con modulo.json válido.
// Un manifest ilexible ou incompleto (sen id ou sen executable) ignórase en
// silencio — mesmo criterio ca Externos() de piztu/internal/modulos.
func ExternosTao(dir string) []ModuloTao {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var modulos []ModuloTao
	for _, e := range entradas {
		info, err := e.Info()
		if err != nil {
			continue
		}
		nomeCartafol := e.Name()
		subDir := filepath.Join(dir, nomeCartafol)
		if info.Mode()&os.ModeSymlink != 0 {
			// resolveDestino segue o symlink para saber se apunta realmente a
			// un cartafol (mesmo criterio ca Piztu, para instalacións que usan
			// un enlace en vez de copiar o módulo).
			destino, err := filepath.EvalSymlinks(subDir)
			if err != nil {
				continue
			}
			subDir = destino
			info, err = os.Stat(subDir)
			if err != nil {
				continue
			}
		}
		if !info.IsDir() {
			continue
		}

		m, ok := lerManifestTao(subDir)
		if !ok {
			continue
		}
		m.Dir = subDir
		modulos = append(modulos, m)
	}
	return modulos
}

// BuscarModuloTao localiza un único módulo externo por id dentro de dir.
func BuscarModuloTao(dir, id string) (ModuloTao, bool) {
	for _, m := range ExternosTao(dir) {
		if m.ID == id {
			return m, true
		}
	}
	return ModuloTao{}, false
}

// lerManifestTao le e valida <subDir>/modulo.json.
func lerManifestTao(subDir string) (ModuloTao, bool) {
	datos, err := os.ReadFile(filepath.Join(subDir, "modulo.json"))
	if err != nil {
		return ModuloTao{}, false
	}
	var man manifestTao
	if err := json.Unmarshal(datos, &man); err != nil {
		return ModuloTao{}, false
	}
	man.ID = strings.TrimSpace(man.ID)
	man.Executable = strings.TrimSpace(man.Executable)
	if man.ID == "" || man.Executable == "" {
		return ModuloTao{}, false
	}

	nome := strings.TrimSpace(man.Nome)
	if nome == "" {
		nome = man.ID
	}

	return ModuloTao{
		ID:                man.ID,
		Nome:              nome,
		Icona:             man.Icona,
		IconaImaxeDataURI: iconaImaxeDataURITao(subDir, man.IconaImaxe),
		Executable:        man.Executable,
		Version:           strings.TrimSpace(man.Version),
		Autor:             man.Autor,
		Email:             man.Email,
		Web:               man.Web,
		Descricion:        man.Descricion,
		Permisos:          man.Permisos,
	}, true
}

// iconaImaxeDataURITao le <subDir>/<rel> (se declarado) e devólvea coma data
// URI para amosala directamente no frontend sen expoñer rutas de disco.
// Cadea baleira se non hai IconaImaxe ou non se puido ler/recoñecer.
func iconaImaxeDataURITao(subDir, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	datos, err := os.ReadFile(filepath.Join(subDir, rel))
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
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(datos)
}
