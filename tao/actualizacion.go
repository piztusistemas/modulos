package main

// Autoactualización do propio Tao, mesmo mecanismo ca Piztu e Yang (ver
// piztu/internal/actualizacion/actualizacion.go e yang/actualizacion.go,
// dos que este ficheiro é unha adaptación directa — Tao non pode importar
// ningún dos dous porque vive noutro módulo/binario Go).
//
// A versión instalada lese do ficheiro VERSION embebido na raíz deste
// módulo. A versión publicada consúltase no repositorio compartido de
// módulos de Piztu, piztusistemas/modulos — cada publicación de Tao crea a súa
// .zip soltos na RAÍZ dese repo, non en Releases (o repo non ten ningunha):
// lístase "/contents/" e búscase o "tao_<versión>.zip" de versión máis
// alta — a versión remota parséase dese nome de ficheiro. É a mesma fonte
// que consulta Piztu para os seus módulos (ver
// piztu/internal/modulos/instalar.go, resolverGitHubRepoCompleto), así que
// publicar un módulo é só subir o seu .zip: nin tags, nin Releases, nin
// manifests. A carón ten que ir "<nome-do-zip>.sha256", verificado antes
// de instalar nada.
import (
	"archive/zip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

//go:embed VERSION
var versionTaoFicheiro string

const repoTaoActualizacion = "piztusistemas/modulos"
const urlContidosTao = "https://api.github.com/repos/" + repoTaoActualizacion + "/contents/"
const timeoutComprobarTao = 5 * time.Second
const timeoutDescargaTao = 60 * time.Second

// EstadoActualizacionTao é o resultado de comprobar unha actualización de
// Tao — mesma forma ca EstadoActualizacionYang, para que o frontend (React)
// a consuma igual.
type EstadoActualizacionTao struct {
	Disponible    bool   `json:"disponible"`
	VersionLocal  string `json:"versionLocal"`
	VersionRemota string `json:"versionRemota"`
	URLDescarga   string `json:"urlDescarga"`
	URLChecksum   string `json:"urlChecksum"`
	Notas         string `json:"notas"`
}

type ficheiroGitHubTao struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

// escollerZipTao busca, no listado da raíz do repo de módulos, o .zip máis
// novo do propio Tao e o seu .sha256 irmán. O prefixo "tao_" vai cravado: o
// repo é compartido con outros módulos e "calquera .zip" collería o errado.
//
// A diferenza de cando isto miraba as Releases (unha Release = unha versión,
// e a API xa as devolvía ordenadas), nun listado de ficheiros conviven todas
// as versións publicadas á vez: hai que quedar coa MAIOR, non coa primeira
// que apareza.
func escollerZipTao(ficheiros []ficheiroGitHubTao) (urlZip, nomeZip, urlChecksum string) {
	mellorVersion := ""
	for _, f := range ficheiros {
		nome := strings.ToLower(f.Name)
		if !strings.HasPrefix(nome, "tao_") || !strings.HasSuffix(nome, ".zip") || f.DownloadURL == "" {
			continue
		}
		v := versionDesdeNomeZipTao(f.Name)
		if v == "" {
			continue
		}
		if mellorVersion != "" && !versionMaiorTaoActualizacion(v, mellorVersion) {
			continue
		}
		mellorVersion, urlZip, nomeZip = v, f.DownloadURL, f.Name
	}
	if urlZip == "" {
		return "", "", ""
	}
	for _, f := range ficheiros {
		if strings.EqualFold(f.Name, nomeZip+".sha256") {
			urlChecksum = f.DownloadURL
			break
		}
	}
	return urlZip, nomeZip, urlChecksum
}

func versionDesdeNomeZipTao(nomeZip string) string {
	nome := strings.ToLower(nomeZip)
	nome = strings.TrimPrefix(nome, "tao_")
	nome = strings.TrimSuffix(nome, ".zip")
	return strings.TrimSpace(nome)
}

func partesVersionTaoActualizacion(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return nil, false
	}
	anacos := strings.Split(v, ".")
	partes := make([]int, 0, len(anacos))
	for _, a := range anacos {
		n, err := strconv.Atoi(strings.TrimSpace(a))
		if err != nil {
			return nil, false
		}
		partes = append(partes, n)
	}
	return partes, true
}

func versionMaiorTaoActualizacion(remota, local string) bool {
	r, okR := partesVersionTaoActualizacion(remota)
	if !okR {
		return false
	}
	l, okL := partesVersionTaoActualizacion(local)
	if !okL {
		l = []int{0}
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

// VersionLocalTao devolve a versión instalada de Tao.
func VersionLocalTao() string {
	return strings.TrimSpace(versionTaoFicheiro)
}

// GetActualizacionTao devolve a última foto de se hai actualización de Tao
// dispoñible, sen tocar a rede (← diálogo "Sobre Tao" ao abrirse) — bound ao
// frontend. A comprobación real faise unha vez en fondo ao arrincar (ver
// comprobarActualizacionTao, chamada dende startup en app.go) e cada vez que
// cambia emítese o evento "actualizacion_tao_cambiada".
func (a *App) GetActualizacionTao() EstadoActualizacionTao {
	a.actualizacionMu.Lock()
	defer a.actualizacionMu.Unlock()
	return a.actualizacionTao
}

// comprobarActualizacionTao consulta a rede e garda o resultado, emitindo o
// evento se cambiou dende a última comprobación. Chamado en fondo (go
// a.comprobarActualizacionTao()) dende startup — nunca bloquea o arranque.
func (a *App) comprobarActualizacionTao() {
	estado := comprobarActualizacionTaoRede()
	a.actualizacionMu.Lock()
	a.actualizacionTao = estado
	a.actualizacionMu.Unlock()
	emitirEvento("actualizacion_tao_cambiada", estado)
}

// comprobarActualizacionTaoRede lista a raíz de piztusistemas/modulos en
// GitHub e compara o .zip de Tao máis novo coa versión instalada. Nunca
// devolve erro: sen rede, sen .zip de Tao, ou resposta inválida, Disponible
// queda false.
func comprobarActualizacionTaoRede() EstadoActualizacionTao {
	estado := EstadoActualizacionTao{VersionLocal: VersionLocalTao()}

	req, err := http.NewRequest(http.MethodGet, urlContidosTao, nil)
	if err != nil {
		return estado
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	cliente := http.Client{Timeout: timeoutComprobarTao}
	resp, err := cliente.Do(req)
	if err != nil {
		return estado
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return estado
	}

	var ficheiros []ficheiroGitHubTao
	if err := json.NewDecoder(resp.Body).Decode(&ficheiros); err != nil {
		return estado
	}

	urlZip, nomeZip, urlChecksum := escollerZipTao(ficheiros)
	if urlZip == "" {
		return estado // na raíz do repo aínda non hai ningún .zip deste módulo
	}
	versionRemota := versionDesdeNomeZipTao(nomeZip)
	if versionRemota == "" {
		return estado
	}
	estado.VersionRemota = versionRemota
	estado.URLDescarga = urlZip
	estado.URLChecksum = urlChecksum
	estado.Disponible = versionMaiorTaoActualizacion(versionRemota, estado.VersionLocal)
	return estado
}

func nomeExecutableTao() string {
	if runtime.GOOS == "windows" {
		return "tao.exe"
	}
	return "tao"
}

func descargarTao(url, destinoDir, nomeFicheiro string) (string, error) {
	if err := os.MkdirAll(destinoDir, 0o755); err != nil {
		return "", fmt.Errorf("non se puido crear %q: %w", destinoDir, err)
	}
	destino := filepath.Join(destinoDir, nomeFicheiro)

	cliente := http.Client{Timeout: timeoutDescargaTao}
	resp, err := cliente.Get(url)
	if err != nil {
		return "", fmt.Errorf("non se puido descargar a actualización: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non se puido descargar a actualización: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destino)
	if err != nil {
		return "", fmt.Errorf("non se puido crear %q: %w", destino, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("non se puido gardar a actualización: %w", err)
	}
	return destino, nil
}

func extraerExecutableTao(zipRuta, destDir string) (string, error) {
	zr, err := zip.OpenReader(zipRuta)
	if err != nil {
		return "", fmt.Errorf("o ficheiro descargado non é un zip válido: %w", err)
	}
	defer zr.Close()

	buscado := nomeExecutableTao()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != buscado {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("non se puido abrir %q dentro do zip: %w", f.Name, err)
		}
		defer rc.Close()

		destino := filepath.Join(destDir, buscado)
		out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", fmt.Errorf("non se puido crear %q: %w", destino, err)
		}
		defer out.Close()
		if _, err := io.Copy(out, rc); err != nil {
			return "", fmt.Errorf("non se puido extraer %q: %w", f.Name, err)
		}
		return destino, nil
	}
	return "", fmt.Errorf("o zip da actualización non contén %q — revisa como o empaquetaches", buscado)
}

func lerHashEsperadoTao(r io.Reader) (string, error) {
	datos, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil {
		return "", fmt.Errorf("non se puido ler o .sha256: %w", err)
	}
	campos := strings.Fields(string(datos))
	if len(campos) == 0 {
		return "", fmt.Errorf("o ficheiro .sha256 publicado está baleiro")
	}
	return strings.ToLower(campos[0]), nil
}

func comprobarChecksumTao(ficheiroRuta, urlChecksum string) error {
	if urlChecksum == "" {
		return fmt.Errorf("non hai un .sha256 a carón do .zip no repo — non se pode verificar a súa integridade, así que se cancela a actualización por seguridade")
	}

	cliente := http.Client{Timeout: timeoutComprobarTao}
	resp, err := cliente.Get(urlChecksum)
	if err != nil {
		return fmt.Errorf("non se puido descargar o .sha256 da actualización: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non se puido descargar o .sha256 da actualización: HTTP %d", resp.StatusCode)
	}
	esperado, err := lerHashEsperadoTao(resp.Body)
	if err != nil {
		return err
	}

	f, err := os.Open(ficheiroRuta)
	if err != nil {
		return fmt.Errorf("non se puido abrir %q para verificalo: %w", ficheiroRuta, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("non se puido calcular o SHA-256 de %q: %w", ficheiroRuta, err)
	}
	obtido := hex.EncodeToString(h.Sum(nil))

	if obtido != esperado {
		return fmt.Errorf("o SHA-256 do .zip descargado non coincide co publicado no repo — podería estar adulterado; cancélase a actualización")
	}
	return nil
}

func copiarFicheiroTao(orixe, destino string) error {
	in, err := os.Open(orixe)
	if err != nil {
		return fmt.Errorf("non se puido abrir %q: %w", orixe, err)
	}
	defer in.Close()
	out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("non se puido crear %q: %w", destino, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("non se puido copiar a %q: %w", destino, err)
	}
	return nil
}

// AplicarActualizacionTao descarga a última versión de Tao (estado xa
// coñecido por GetActualizacionTao), substitúe o executable en execución e
// arrinca xa a nova copia (← botón "Actualizar" no diálogo "Sobre Tao"). O
// frontend é quen peche esta xanela despois, en canto isto devolva sen
// erro. Profesor ou administración só (mesmo gate ca
// AbrirYangParaTarefa/InstalarYang): unha actualización que reinicia Tao
// non a debe disparar calquera sesión de alumnado.
func (a *App) AplicarActualizacionTao() error {
	if !a.apiAutorizado() {
		return fmt.Errorf("precísase unha sesión de profesorado ou administración para actualizar Tao")
	}
	estado := a.GetActualizacionTao()
	if !estado.Disponible || estado.URLDescarga == "" {
		return fmt.Errorf("non hai ningunha actualización de Tao dispoñible")
	}
	if err := aplicarActualizacionTaoEnCaliente(estado.URLDescarga, estado.URLChecksum, estado.VersionRemota); err != nil {
		return err
	}
	return reiniciarTao()
}

// aplicarActualizacionTaoEnCaliente descarga o .zip publicado en urlZip,
// verifica o seu SHA-256 contra urlChecksum, extrae o executable de Tao e
// substitúeo polo que está en execución agora mesmo.
func aplicarActualizacionTaoEnCaliente(urlZip, urlChecksum, versionNova string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("a actualización automática aínda non está soportada en Windows")
	}

	exeActual, err := os.Executable()
	if err != nil {
		return fmt.Errorf("non se puido localizar o executable actual: %w", err)
	}
	exeActual, err = filepath.EvalSymlinks(exeActual)
	if err != nil {
		return fmt.Errorf("non se puido resolver o executable actual: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "tao-actualizacion-")
	if err != nil {
		return fmt.Errorf("non se puido crear un cartafol temporal: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	zipRuta, err := descargarTao(urlZip, tmpDir, "actualizacion.zip")
	if err != nil {
		return err
	}
	if err := comprobarChecksumTao(zipRuta, urlChecksum); err != nil {
		return err
	}
	novoExe, err := extraerExecutableTao(zipRuta, tmpDir)
	if err != nil {
		return err
	}

	swapTmp := exeActual + ".novo"
	defer os.Remove(swapTmp)
	if err := copiarFicheiroTao(novoExe, swapTmp); err != nil {
		return err
	}

	_ = copiarFicheiroTao(exeActual, exeActual+".anterior")

	if err := os.Rename(swapTmp, exeActual); err != nil {
		return fmt.Errorf("non se puido instalar o binario novo: %w", err)
	}

	if versionNova != "" {
		actualizarVersionModuloJSONTao(filepath.Dir(exeActual), versionNova)
	}
	return nil
}

// actualizarVersionModuloJSONTao reescribe (best-effort) o "version" do
// modulo.json canda o executable, para que ⚙ Aula → Módulos de Piztu
// reflicta a versión nova sen agardar ao seguinte escaneo do catálogo.
func actualizarVersionModuloJSONTao(dirModulo, versionNova string) {
	ruta := filepath.Join(dirModulo, "modulo.json")
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(datos, &m); err != nil {
		return
	}
	m["version"] = versionNova
	novosDatos, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(ruta, novosDatos, 0o644)
}

// reiniciarTao arrinca unha nova copia de Tao (xa a versión instalada por
// aplicarActualizacionTaoEnCaliente) coma proceso independente, para que o
// frontend poida pechar esta xanela decontado.
func reiniciarTao() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("non se puido localizar o executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("non se puido resolver o executable: %w", err)
	}

	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("non se puido reiniciar Tao: %w", err)
	}
	return nil
}
