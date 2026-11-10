package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// timeoutGitHubAPI e timeoutDescargaModulo son os tempos límite compartidos
// por calquera instalador de módulos de Tao — o caso específico de Yang
// (yang_instalar.go) e o sistema xenérico de módulos externos
// (modulos_instalar_tao.go).
const timeoutGitHubAPI = 5 * time.Second
const timeoutDescargaModulo = 30 * time.Second

// ghContentEntry é unha entrada de "GET /repos/{repo}/contents/" (API pública
// de GitHub, sen autenticar) — o mesmo formato que devolve para calquera
// ficheiro na raíz dun repo.
type ghContentEntry struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

// listarContidoGitHubRepo lista a raíz de repo ("propietario/repo") vía a API
// pública de GitHub. ok=false sen rede, repo inexistente/baleiro, ou se a API
// falla (ex.: límite de peticións sen autenticar) — nunca devolve erro, para
// que instalar/actualizar un módulo falle con gracia ("non hai actualización
// dispoñible") en vez de romper a UI.
func listarContidoGitHubRepo(repo string) (entradas []ghContentEntry, ok bool) {
	api := "https://api.github.com/repos/" + repo + "/contents/"
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	cliente := http.Client{Timeout: timeoutGitHubAPI}
	resp, err := cliente.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	if err := json.NewDecoder(resp.Body).Decode(&entradas); err != nil {
		return nil, false
	}
	return entradas, true
}

// versionMaior compara dúas versións "semver-lite" (números separados por
// puntos; un "v" inicial ignórase) — porte mínimo de modulos.VersionMaior
// (piztu/internal/modulos/instalar.go).
func versionMaior(remota, local string) bool {
	r, okR := partesVersion(remota)
	if !okR {
		return false
	}
	l, okL := partesVersion(local)
	if !okL {
		return true
	}
	for i := 0; i < len(r) || i < len(l); i++ {
		var rv, lv int
		if i < len(r) {
			rv = r[i]
		}
		if i < len(l) {
			lv = l[i]
		}
		if rv != lv {
			return rv > lv
		}
	}
	return false
}

func partesVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	partes := strings.Split(v, ".")
	res := make([]int, 0, len(partes))
	for _, p := range partes {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, false
		}
		res = append(res, n)
	}
	return res, true
}

// aplanarSeEnvoltoNunCartafol corrixe o caso típico de GitHub: un .zip cuxo
// contido non está na raíz, senón dentro dun único cartafol contedor (ex.
// "yang/yang" en vez de "yang" a secas) — porte de
// aplanarSeEnvoltoNunCartafol (piztu/internal/modulos/instalar.go). Só actúa
// se destino ten exactamente UNHA entrada e é un cartafol.
func aplanarSeEnvoltoNunCartafol(destino string) error {
	entradas, err := os.ReadDir(destino)
	if err != nil || len(entradas) != 1 || !entradas[0].IsDir() {
		return nil
	}
	envoltorio := filepath.Join(destino, entradas[0].Name())

	temporal := envoltorio + ".aplanando"
	if err := os.Rename(envoltorio, temporal); err != nil {
		return err
	}
	internas, err := os.ReadDir(temporal)
	if err != nil {
		return err
	}
	for _, e := range internas {
		orixe := filepath.Join(temporal, e.Name())
		if err := os.Rename(orixe, filepath.Join(destino, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(temporal)
}

// extraerZip descomprime zipPath en destino, evitando "zip slip" (entradas
// que escapen do cartafol destino) — porte de extraerZip
// (piztu/internal/modulos/instalar.go).
func extraerZip(zipPath, destino string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	destinoAbs, err := filepath.Abs(destino)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		rutaAbs, err := filepath.Abs(filepath.Join(destino, f.Name))
		if err != nil || (rutaAbs != destinoAbs && !strings.HasPrefix(rutaAbs, destinoAbs+string(os.PathSeparator))) {
			return fmt.Errorf("entrada insegura no zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(rutaAbs, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(rutaAbs), 0o755); err != nil {
			return err
		}
		if err := extraerFicheiroZip(f, rutaAbs); err != nil {
			return err
		}
	}
	return nil
}

func extraerFicheiroZip(f *zip.File, destino string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(destino, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}
