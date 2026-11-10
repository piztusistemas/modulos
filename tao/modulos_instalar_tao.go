package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// repoModulosTaoGitHub é o repositorio compartido de módulos externos DE
// Tao — distinto de repoModulosGitHub (yang_instalar.go), que é o de Piztu.
// Pode que aínda non exista/estea baleiro: CatalogoModulosTao e
// resolverUltimaVersionModuloTao devolven ok=false con gracia nese caso,
// nunca un erro que rompa a UI.
const repoModulosTaoGitHub = "piztusistemas/tao_modulos"

// reZipModuloTao recoñece "<id>_<versión>.zip" (ou "<id>-<versión>.zip") na
// raíz do repo — mesmo patrón cás entradas "*" de
// piztu/internal/modulos/instalar.go: calquera módulo publicado alí
// descóbrese polo seu propio nome de ficheiro, sen ter que coñecer os ids de
// antemán.
var reZipModuloTao = regexp.MustCompile(`^(.+?)[_-]([0-9]+(?:\.[0-9]+)+)\.zip$`)

// entradaZipModuloTao é un "<id>_<versión>.zip" xa recoñecido na raíz do
// repo.
type entradaZipModuloTao struct {
	ID      string
	Version string
	URL     string
}

// todosOsZipsModuloTao lista repoModulosTaoGitHub e devolve, por id, o zip de
// versión máis alta atopado (pode haber varias versións convivindo no
// repo). ok=false sen rede, repo inexistente/baleiro, ou API de GitHub
// inaccesible.
func todosOsZipsModuloTao() (map[string]entradaZipModuloTao, bool) {
	entradas, ok := listarContidoGitHubRepo(repoModulosTaoGitHub)
	if !ok {
		return nil, false
	}

	mellores := map[string]entradaZipModuloTao{}
	for _, e := range entradas {
		m := reZipModuloTao.FindStringSubmatch(e.Name)
		if m == nil || e.DownloadURL == "" {
			continue
		}
		id, version := m[1], m[2]
		actual, xaHai := mellores[id]
		if !xaHai || versionMaior(version, actual.Version) {
			mellores[id] = entradaZipModuloTao{ID: id, Version: version, URL: e.DownloadURL}
		}
	}
	return mellores, true
}

// CatalogoModulosTao devolve, por id, a última versión dispoñible en
// repoModulosTaoGitHub — usado pola UI para amosar "hai actualización" sen
// descargar nada. ok=false (repo baleiro/inexistente/sen rede) non é un
// erro: simplemente non hai nada que ofrecer.
func CatalogoModulosTao() (map[string]string, bool) {
	zips, ok := todosOsZipsModuloTao()
	if !ok {
		return nil, false
	}
	catalogo := make(map[string]string, len(zips))
	for id, e := range zips {
		catalogo[id] = e.Version
	}
	return catalogo, true
}

// resolverUltimaVersionModuloTao busca, dentro do catálogo do repo, a última
// versión publicada de id.
func resolverUltimaVersionModuloTao(id string) (url, version string, ok bool) {
	zips, okList := todosOsZipsModuloTao()
	if !okList {
		return "", "", false
	}
	e, achado := zips[id]
	if !achado {
		return "", "", false
	}
	return e.URL, e.Version, true
}

// InstalarModuloTaoDesdeRepo descarga o zip máis recente de id en
// repoModulosTaoGitHub e desprégao en <dir>/<id>. Non fai nada se xa hai un
// módulo instalado con ese id (mesmo criterio ca Instalar() de
// piztu/internal/modulos: instalar non actualiza, para iso está
// ActualizarModuloTaoDesdeRepo).
func InstalarModuloTaoDesdeRepo(dir, id string) error {
	if _, xaInstalado := BuscarModuloTao(dir, id); xaInstalado {
		return nil
	}
	return baixarEDescomprimirModuloTao(dir, id)
}

// ActualizarModuloTaoDesdeRepo bota fóra a copia instalada de id (se a hai) e
// reinstala a última versión do repo — mesmo criterio ca Actualizar() de
// piztu/internal/modulos: non toca o estado activo/inactivo, iso vive á
// parte (modulos_estado_tao.go).
func ActualizarModuloTaoDesdeRepo(dir, id string) error {
	destino := filepath.Join(dir, id)
	if err := os.RemoveAll(destino); err != nil {
		return fmt.Errorf("non se puido eliminar a versión anterior de %q: %w", id, err)
	}
	return baixarEDescomprimirModuloTao(dir, id)
}

func baixarEDescomprimirModuloTao(dir, id string) error {
	url, version, ok := resolverUltimaVersionModuloTao(id)
	if !ok {
		return fmt.Errorf("non se atopou ningún %q en github.com/%s — revisa a conexión a internet ou se o módulo segue publicado", id+"_<versión>.zip", repoModulosTaoGitHub)
	}

	cliente := http.Client{Timeout: timeoutDescargaModulo}
	resp, err := cliente.Get(url)
	if err != nil {
		return fmt.Errorf("non se puido descargar %q: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non se puido descargar %q: HTTP %d", id, resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "tao-modulo-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpZip.Name())
	if _, err := io.Copy(tmpZip, resp.Body); err != nil {
		tmpZip.Close()
		return fmt.Errorf("erro gardando a descarga de %q: %w", id, err)
	}
	tmpZip.Close()

	destino := filepath.Join(dir, id)
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return fmt.Errorf("non se puido crear %q: %w", destino, err)
	}
	if err := extraerZip(tmpZip.Name(), destino); err != nil {
		return fmt.Errorf("o ficheiro de %q descargado non se puido descomprimir: %w", id, err)
	}
	if err := aplanarSeEnvoltoNunCartafol(destino); err != nil {
		return fmt.Errorf("non se puido preparar %q despois de descomprimir: %w", id, err)
	}

	if _, ok := BuscarModuloTao(dir, id); !ok {
		return fmt.Errorf("descargouse %q %s pero non se atopa un modulo.json válido en %s", id, version, destino)
	}
	return nil
}
