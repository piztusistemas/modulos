package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// repoModulosGitHub é onde viven os módulos descargables de Piztu — o mesmo
// repositorio ao que xa apunta piztu/internal/modulos (config.yaml,
// modulos.catalogo_local "*"): calquera "<id>_<versión>.zip" na súa raíz
// aparece coma módulo descargable. Tao reimplementa só a fatiada que
// precisa (atopar+descargar+descomprimir "yang_<versión>.zip") en vez de
// importar piztu/internal/modulos: son binarios/módulos Go
// independentes, sen paquete compartido (mesmo motivo ca yangModuloManifest
// en yang_launch.go). Os helpers de zip/versión/listaxe GitHub compártense
// (dentro deste mesmo binario) con modulos_instalar_tao.go — ver
// zip_util_tao.go.
const repoModulosGitHub = "piztusistemas/modulos"

// reZipYang recoñece "yang_<versión>.zip" (ou "yang-<versión>.zip") na raíz
// do repo — mesmo patrón ca mellorZipPrefixo en piztu/internal/modulos,
// pero co prefixo "yang" cravado (é o único módulo específico que Tao sabe
// instalar deste xeito; para módulos externos xenéricos calquera, ver
// modulos_instalar_tao.go).
var reZipYang = regexp.MustCompile(`^yang[_-]([0-9]+(?:\.[0-9]+)+)\.zip$`)

// resolverUltimaVersionYang lista a raíz de repoModulosGitHub e devolve a
// URL de descarga do "yang_<versión>.zip" máis novo que atope. ok=false sen
// rede, sen coincidencias, ou se a API de GitHub falla (ex.: límite de
// peticións sen autenticar).
func resolverUltimaVersionYang() (url, version string, ok bool) {
	entradas, okList := listarContidoGitHubRepo(repoModulosGitHub)
	if !okList {
		return "", "", false
	}

	var mellorVersion string
	for _, e := range entradas {
		m := reZipYang.FindStringSubmatch(e.Name)
		if m == nil || e.DownloadURL == "" {
			continue
		}
		if url == "" || versionMaior(m[1], mellorVersion) {
			url, mellorVersion = e.DownloadURL, m[1]
		}
	}
	return url, mellorVersion, url != ""
}

// destinoInstalacionYang decide onde debe quedar Yang: <base_dir>/modulos/yang
// se Tao coñece unha instalación de Piztu (así queda rexistrado coma
// calquera outro módulo, visible en ⚙ Aula → Módulos do lado de Piztu), ou o
// camiño fixo destinoYangStandalone (yang_launch.go) se non — mesmo
// criterio que localizarYang usa para BUSCAR, aquí para decidir onde
// ESCRIBIR.
func destinoInstalacionYang(baseDir string) string {
	if baseDir != "" {
		return filepath.Join(baseDir, "modulos", "yang")
	}
	return destinoYangStandalone
}

// InstalarYang descarga o .zip de Yang máis recente publicado en
// repoModulosGitHub e desprégao neste equipo — o botón "Instalar Yang" que
// aparece en Tao (TaskFilesSection, App.tsx) cando YangDispoñible() dá
// false. Profesor só, mesmo gate ca AbrirYangParaTarefa: é a mesma
// funcionalidade, só que primeiro hai que traer Yang.
func (a *App) InstalarYang() error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if _, ok := a.localizarYang(); ok {
		return nil // xa está (dous clics seguidos, ou outra xanela xa o instalou): nada que facer
	}

	url, version, ok := resolverUltimaVersionYang()
	if !ok {
		return fmt.Errorf("non se atopou ningún \"yang_<versión>.zip\" en github.com/%s — revisa a conexión a internet", repoModulosGitHub)
	}

	destino := destinoInstalacionYang(a.baseDir)
	if err := baixarEDescomprimirYang(url, destino, version); err != nil {
		return err
	}
	if _, ok := a.localizarYang(); !ok {
		return fmt.Errorf("descargouse Yang %s pero non se atopa o executable declarado no seu modulo.json en %s", version, destino)
	}
	return nil
}

// baixarEDescomprimirYang é o núcleo de InstalarYang: descarga o .zip,
// descomprímeo en destino (extraerZip/aplanarSeEnvoltoNunCartafol, ver
// zip_util_tao.go) e completa a versión no modulo.json se non a trouxese.
func baixarEDescomprimirYang(url, destino, version string) error {
	cliente := http.Client{Timeout: timeoutDescargaModulo}
	resp, err := cliente.Get(url)
	if err != nil {
		return fmt.Errorf("non se puido descargar Yang: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non se puido descargar Yang: HTTP %d", resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "tao-yang-modulo-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpZip.Name())
	if _, err := io.Copy(tmpZip, resp.Body); err != nil {
		tmpZip.Close()
		return fmt.Errorf("erro gardando a descarga de Yang: %w", err)
	}
	tmpZip.Close()

	if err := os.MkdirAll(destino, 0o755); err != nil {
		return fmt.Errorf("non se puido crear %q: %w", destino, err)
	}
	if err := extraerZip(tmpZip.Name(), destino); err != nil {
		return fmt.Errorf("o ficheiro de Yang descargado non se puido descomprimir: %w", err)
	}
	if err := aplanarSeEnvoltoNunCartafol(destino); err != nil {
		return fmt.Errorf("non se puido preparar Yang despois de descomprimir: %w", err)
	}
	return completarVersionSeFaltaYang(destino, version)
}

// completarVersionSeFaltaYang engade "version" ao modulo.json xa extraído se
// non a trae declarada — normalmente non fai falla (o modulo.json de Yang
// xa a declara), pero cobre un .zip publicado sen ela.
func completarVersionSeFaltaYang(destino, version string) error {
	ficheiro := filepath.Join(destino, "modulo.json")
	datos, err := os.ReadFile(ficheiro)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	m := map[string]any{}
	if len(datos) > 0 {
		if err := json.Unmarshal(datos, &m); err != nil {
			m = map[string]any{}
		}
	}
	if v, ok := m["version"].(string); ok && strings.TrimSpace(v) != "" {
		return nil // xa a declara: non se toca
	}
	if version == "" {
		return nil
	}
	if _, ok := m["id"]; !ok {
		m["id"] = "yang"
	}
	m["version"] = version

	saida, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ficheiro, saida, 0o644)
}
