package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Tao fala con DOUS servidores de ficheiros que non son compatibles entre si:
//
//   - File Browser clásico  (github.com/filebrowser/filebrowser, v2.x): login por
//     POST /api/login con JSON, token na cabeceira X-Auth, rutas na URL
//     (/api/resources/a/b), contas con "perm" e regras de acceso DENTRO do usuario.
//   - File Browser Quantum  (github.com/gtsteffaniak/filebrowser): login por
//     POST /api/auth/login?username=, "Authorization: Bearer", rutas na query
//     (?source=&path=), contas con "permissions" e regras de acceso por CAMIÑO.
//
// O profesorado non ten que saber cal ten instalado nin configurar nada: Tao
// descóbreo el solo mirando que rutas de API existen no servidor (fbDetectarSabor)
// e lembra o resultado por URL. O que é IGUAL nos dous (o fluxo de backups, o
// roster, os traballos…) non se toca; só as funcións que constrúen a URL, o corpo
// ou a lectura dunha resposta bifúrcanse segundo o "sabor" (fbSabor).
//
// Como distinguilos — comprobado contra o clásico v2.63.18 e Quantum v1.5.6: cada
// un ten unha ruta de renovación de token que o outro non ten, e sen token as dúas
// responden 401 se existen e 404 se non:
//
//	POST /api/auth/renew  → 401 en Quantum,  404 no clásico
//	POST /api/renew       → 404 en Quantum,  401 no clásico
type fbSabor int

const (
	// fbSaborIndeterminado: a sondaxe non concluíu (servidor apagado, proxy raro).
	fbSaborIndeterminado fbSabor = iota
	fbClasico
	fbQuantum
)

func (s fbSabor) String() string {
	switch s {
	case fbClasico:
		return "File Browser clásico"
	case fbQuantum:
		return "File Browser Quantum"
	}
	return "File Browser (tipo sen determinar)"
}

const (
	// fbSaborTTL é canto tempo se fía Tao dun tipo detectado antes de volver
	// sondear. Un servidor non cambia de tipo de repente, pero unha aula pode migrar
	// do clásico a Quantum na mesma URL, e Tao non debe quedar días coa idea vella.
	// (Cada login sondea de novo igualmente: ver fbLogin.)
	fbSaborTTL = 30 * time.Minute

	// fbSaborReintento é canto agarda Tao antes de volver sondear un servidor que non
	// respondeu de forma concluínte. Sen isto, un servidor apagado gastaría 2 s de
	// sondaxe en CADA operación da cola sen conexión (fbqueue.go).
	fbSaborReintento = 20 * time.Second
)

type fbSaborEntrada struct {
	sabor fbSabor
	cando time.Time
}

var (
	fbSaborMu    sync.Mutex
	fbSaborCache = map[string]fbSaborEntrada{}
)

func fbSaborChave(base string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/")
}

// fbSaborDe devolve o tipo de servidor que hai en base, sondeándoo se aínda non se
// sabe. Cando non se pode saber (servidor inaccesible) devolve Quantum, o tipo actual
// e por onde sempre andou Tao: unha chamada que falle por outro motivo fallará igual
// e a cola sen conexión reintentará máis tarde, xa coa sondaxe resolta.
func fbSaborDe(base string) fbSabor {
	fbSaborMu.Lock()
	e, ok := fbSaborCache[fbSaborChave(base)]
	fbSaborMu.Unlock()
	if ok {
		if e.sabor != fbSaborIndeterminado && time.Since(e.cando) < fbSaborTTL {
			return e.sabor
		}
		if e.sabor == fbSaborIndeterminado && time.Since(e.cando) < fbSaborReintento {
			return fbQuantum
		}
	}
	if s := fbDetectarSabor(base); s != fbSaborIndeterminado {
		return s
	}
	return fbQuantum
}

// fbDetectarSabor sonda base AGORA (sen mirar a caché), garda o resultado e o devolve.
// Se a sondaxe non conclúe pero antes se soubo o tipo, mantense ese: un fallo de rede
// de dous segundos non pode facer que un clásico pase a tratarse como Quantum.
func fbDetectarSabor(base string) fbSabor {
	s := fbSondearSabor(base)
	fbSaborMu.Lock()
	defer fbSaborMu.Unlock()
	k := fbSaborChave(base)
	if prev, ok := fbSaborCache[k]; s == fbSaborIndeterminado && ok && prev.sabor != fbSaborIndeterminado {
		s = prev.sabor
	}
	fbSaborCache[k] = fbSaborEntrada{sabor: s, cando: time.Now()}
	return s
}

// fbRecordarSabor fixa o tipo de base sen sondear (o login acaba de demostralo).
func fbRecordarSabor(base string, s fbSabor) {
	fbSaborMu.Lock()
	fbSaborCache[fbSaborChave(base)] = fbSaborEntrada{sabor: s, cando: time.Now()}
	fbSaborMu.Unlock()
}

// fbEsquecerSabor bota fóra o que se lembra de base (tests: os servidores de proba
// reciclan portos e o mesmo enderezo pode ser un clásico nun test e Quantum no seguinte).
func fbEsquecerSabor(base string) {
	fbSaborMu.Lock()
	delete(fbSaborCache, fbSaborChave(base))
	fbSaborMu.Unlock()
}

// fbSondearSabor pregunta ao servidor, sen token, polas dúas rutas de renovación á vez.
// Só conta un 401/403 (a ruta EXISTE e pide autenticación): un 200 non serve porque
// File Browser é unha SPA e devolve a súa páxina a calquera ruta descoñecida, e un
// 301/302 tampouco (non se seguen redireccións: unha páxina de login doutro servizo
// non pode pasar por File Browser). Se as dúas contestan igual (p.ex. un proxy que
// esixe autenticación para todo) non hai forma de saber cal é.
func fbSondearSabor(base string) fbSabor {
	cliente := &http.Client{
		Timeout:       tempoProbaServidor,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	existe := func(ruta string) bool {
		resp, err := cliente.Post(base+ruta, "application/json", nil)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
	}
	var quantum, clasico bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); quantum = existe("/api/auth/renew") }()
	go func() { defer wg.Done(); clasico = existe("/api/renew") }()
	wg.Wait()
	switch {
	case quantum && !clasico:
		return fbQuantum
	case clasico && !quantum:
		return fbClasico
	}
	return fbSaborIndeterminado
}

// fbUserURL é a URL dunha conta polo seu id: /api/users/3 no clásico, /api/users?id=3
// en Quantum.
func fbUserURL(base string, id int) string {
	if fbSaborDe(base) == fbClasico {
		return fmt.Sprintf("%s/api/users/%d", base, id)
	}
	return fmt.Sprintf("%s/api/users?id=%d", base, id)
}

// fbResourceURLClasico constrúe base/api/resources/a/b co camiño na URL. Un isDir=true
// (pedido de crear cartafol) tradúcese na barra final coa que o clásico distingue un
// cartafol dun ficheiro; o resto de parámetros (override…) van na query.
func fbResourceURLClasico(base, remotePath string, extra url.Values) string {
	ruta := fbEscapePath(remotePath)
	q := url.Values{}
	for k, v := range extra {
		if k == "isDir" {
			if len(v) > 0 && v[0] == "true" && ruta != "" {
				ruta += "/"
			}
			continue
		}
		q[k] = v
	}
	u := base + "/api/resources/" + ruta
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// fbDownloadURLs son as URLs candidatas para descargar remotePath, por orde. O clásico
// serve os ficheiros por /api/raw/<ruta> (e un cartafol como zip con ?algo=zip, que ás
// veces só acepta coa barra final: proba as dúas); Quantum por /api/resources/download.
func fbDownloadURLs(base, remotePath string, zipped bool) []string {
	if fbSaborDe(base) != fbClasico {
		return []string{fbDownloadURLQuantum(base, remotePath, zipped)}
	}
	u := base + "/api/raw/" + fbEscapePath(remotePath)
	if !zipped {
		return []string{u}
	}
	return []string{u + "?algo=zip", u + "/?algo=zip"}
}

// fbURLWeb é a URL da interface web de File Browser para abrir un cartafol no
// navegador (practicas.go).
func fbURLWeb(base, dir string) string {
	if fbSaborDe(base) == fbClasico {
		return base + "/files/" + fbEscapePath(dir) + "/"
	}
	// A interface de Quantum enderéza un cartafol como /files/<source>/<ruta>/.
	return base + "/files/" + fbSource + "/" + fbEscapePath(dir) + "/"
}
