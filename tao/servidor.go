package main

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A URL do servidor de ficheiros non se pode cablear no código: cada aula ten o
// seu (tux0.local, o portátil do profesorado, un servidor do centro…). Tampouco
// se lle pode pedir ao alumnado que a teclee — é o paso onde máis se falla.
//
// Por iso é a instalación quen a deixa escrita: o playbook
// instalar_tao_clientes.yaml grava a URL pública do File Browser do servidor en
// /opt/piztu/tao-servidor.conf de cada equipo, e Tao lela aquí para preencher o
// campo "servidor" da pantalla de acceso. Se o ficheiro non existe (instalación
// a man, desenvolvemento) volve a DefaultServerURL, o localhost de sempre.

// ficheiroServidorConf é o nome do ficheiro, buscado primeiro a carón do propio
// binario (instalacións standalone, p.ex. /opt/piztu/modulos/tao) e logo no
// /opt/piztu que usa o playbook da aula.
const ficheiroServidorConf = "tao-servidor.conf"

// rutasServidorConf devolve, por orde de preferencia, onde se busca o ficheiro.
func rutasServidorConf() []string {
	rutas := []string{}
	if exe, err := os.Executable(); err == nil {
		rutas = append(rutas, filepath.Join(filepath.Dir(exe), ficheiroServidorConf))
	}
	return append(rutas, filepath.Join("/opt/piztu", ficheiroServidorConf))
}

// servidorPorDefecto devolve a URL escrita na instalación, ou DefaultServerURL.
// O ficheiro é texto simple: a primeira liña que non estea baleira nin comece
// por "#" é a URL.
func servidorPorDefecto() string {
	for _, ruta := range rutasServidorConf() {
		datos, err := os.ReadFile(ruta)
		if err != nil {
			continue
		}
		for _, liña := range strings.Split(string(datos), "\n") {
			liña = strings.TrimSpace(liña)
			if liña == "" || strings.HasPrefix(liña, "#") {
				continue
			}
			if url := normalizeServerURL(liña); url != "" {
				return url
			}
		}
	}
	return DefaultServerURL
}

// ServidorPorDefecto expón servidorPorDefecto ao frontend, para que a pantalla
// de acceso arranque co servidor da aula xa escrito.
func (a *App) ServidorPorDefecto() string {
	return servidorPorDefecto()
}

// ── Detección automática do servidor ─────────────────────────────────────────
//
// A URL gardada nun equipo envellece: o File Browser dun centro pode pasar do
// :8080 ao porto 80 (detrás do proxy de Apache que monta
// playbooks/instalar_filebrowser.yaml) sen que ninguén volva tocar equipo por
// equipo. DetectarServidor proba, sobre os hosts que xa coñecemos, as formas
// habituais da URL e queda coa mellor que responda de verdade.

// rutaArquivos é a subruta baixo a que se publica File Browser cando vai detrás
// do proxy de Apache (o --baseurl que lle pon a instalación).
const rutaArquivos = "/arquivos"

// tempoProbaServidor é o que se agarda por cada candidata. Curto a propósito:
// próbanse todas á vez e isto só retrasa a pantalla de acceso mentres tanto.
const tempoProbaServidor = 2 * time.Second

// probarRaizFileBrowser di se base é REALMENTE a raíz dunha API de File Browser, sexa o
// clásico ou Quantum (ver fbsabor.go), e de paso lembra de cal dos dous se trata.
// Non abonda con pedir "/" nin "/health": File Browser é unha SPA e devolve 200
// coa súa páxina a calquera ruta descoñecida, así que "http://host:8080/arquivos"
// parecería boa aínda que ese servidor non teña subruta ningunha — e despois
// fallarían todas as chamadas á API. O sinal que se busca é o dunha ruta que só existe
// na API real (POST /api/auth/renew en Quantum, POST /api/renew no clásico) e que, sen
// token, responde 401/403 (un GET daría 405, e un servidor que non sexa File Browser
// non ten esas rutas).
func probarRaizFileBrowser(base string) bool {
	sabor := fbSondearSabor(base)
	if sabor == fbSaborIndeterminado {
		return false
	}
	fbRecordarSabor(base, sabor)
	return true
}

// hostsServidorDesde propón nomes para o servidor da aula a partir do nome deste
// equipo. Nas aulas os equipos levan un prefixo común e un número (bac01, bac02…) e o
// do profesorado é o mesmo prefixo co 0 (bac0, bac00): quitando os díxitos finais e
// engadindo o 0 sae o nome do servidor, sen que o alumnado teña que sabelo nin
// teclealo. Úsase o mesmo dominio ca este equipo (bac17.centro.gal → bac0.centro.gal) e,
// se non ten ningún, ".local" (mDNS, que é como se resolven os equipos das aulas). É
// só unha PROPOSTA: cada nome só entra na lista se despois responde de verdade un
// File Browser (ver DetectarServidor), e o campo do servidor segue sendo editable.
func hostsServidorDesde(hostname string) []string {
	nome := strings.ToLower(strings.TrimSpace(hostname))
	dominio := ".local"
	if i := strings.Index(nome, "."); i >= 0 {
		nome, dominio = nome[:i], nome[i:]
	}
	dixitos := len(nome) - len(strings.TrimRight(nome, "0123456789"))
	if dixitos == 0 || dixitos == len(nome) {
		return nil // sen número final, ou só un número: non hai prefixo de aula
	}
	prefixo := nome[:len(nome)-dixitos]
	nomes := []string{prefixo + "0"}
	if dixitos > 1 {
		nomes = append(nomes, prefixo+strings.Repeat("0", dixitos))
	}
	hosts := []string{}
	for _, n := range nomes {
		if n != nome { // se este equipo é o servidor, xa o cobre localhost
			hosts = append(hosts, n+dominio)
		}
	}
	return hosts
}

// hostsServidorInferidos son as propostas para o nome deste equipo (ver hostsServidorDesde).
func hostsServidorInferidos() []string {
	nome, err := os.Hostname()
	if err != nil {
		return nil
	}
	return hostsServidorDesde(nome)
}

// candidatasServidor devolve as URLs a probar, da máis desexable á menos: para
// cada host coñecido, primeiro o porto 80 (o que lle aforra ao alumnado escribir
// un ":8080") e dentro de cada porto primeiro a subruta /arquivos. Os hosts, por orde:
// o que xa usaba o equipo, o que deixou escrito a instalación, os que se deducen do
// nome deste equipo (hostsServidorDesde) e por último este mesmo equipo.
func candidatasServidor(actual string) []string {
	hosts := []string{}
	engadirHost := func(bruto string) {
		if u, err := url.Parse(normalizeServerURL(bruto)); err == nil && u.Hostname() != "" {
			for _, h := range hosts {
				if h == u.Hostname() {
					return
				}
			}
			hosts = append(hosts, u.Hostname())
		}
	}
	engadirHost(actual)               // o que xa usaba este equipo: o host é bo, o porto pode que non
	engadirHost(servidorPorDefecto()) // o que deixou escrito a instalación
	for _, h := range hostsServidorInferidos() {
		engadirHost(h) // o nome que lle correspondería ao servidor desta aula
	}
	engadirHost("localhost") // Tao aberto no propio servidor
	return candidatasDeHosts(hosts, actual)
}

// candidatasDeHosts combina cada host coas formas habituais da URL (portos e subrutas),
// tomando de actual a subruta e o porto personalizados se os ten.
func candidatasDeHosts(hosts []string, actual string) []string {
	// Unha ruta personalizada na URL actual (nin "/" nin /arquivos) próbase
	// tamén: alguén puido publicar File Browser noutra subruta.
	rutas := []string{rutaArquivos, ""}
	if u, err := url.Parse(normalizeServerURL(actual)); err == nil {
		if r := strings.TrimRight(u.Path, "/"); r != "" && r != rutaArquivos {
			rutas = append([]string{r}, rutas...)
		}
	}

	// Portos a probar: primeiro o 80 (a URL máis curta, a que se quere que vexa o
	// alumnado), despois o porto que xa se estaba a usar —que pode ser calquera nun
	// centro con outra instalación— e por último o 8080 de fábrica de File Browser.
	portos := []string{""}
	if u, err := url.Parse(normalizeServerURL(actual)); err == nil {
		if p := u.Port(); p != "" {
			portos = append(portos, ":"+p)
		}
	}
	xaEsta := func(p string) bool {
		for _, x := range portos {
			if x == p {
				return true
			}
		}
		return false
	}
	if !xaEsta(":8080") {
		portos = append(portos, ":8080")
	}

	candidatas := []string{}
	for _, host := range hosts {
		for _, porto := range portos {
			for _, ruta := range rutas {
				candidatas = append(candidatas, "http://"+host+porto+ruta)
			}
		}
	}
	return candidatas
}

// primeiraQueResponde proba todas as candidatas á vez e devolve a primeira (pola orde
// da lista) que sexa de verdade un File Browser, ou "" se ningunha o é.
func primeiraQueResponde(candidatas []string) string {
	ok := make([]bool, len(candidatas))
	var wg sync.WaitGroup
	for i, base := range candidatas {
		wg.Add(1)
		go func(i int, base string) {
			defer wg.Done()
			ok[i] = probarRaizFileBrowser(base)
		}(i, base)
	}
	wg.Wait()
	for i, valida := range ok {
		if valida {
			return candidatas[i]
		}
	}
	return ""
}

// DetectarServidor devolve a mellor URL de File Browser que responda desde este
// equipo, partindo da que se estea a usar (actual). Devolve "" se ningunha
// contesta — daquela quen chama debe quedar coa que xa tiña, que pode ser
// correcta aínda que o servidor estea apagado neste intre.
func (a *App) DetectarServidor(actual string) string {
	return primeiraQueResponde(candidatasServidor(actual))
}

// resolverServidor converte o que escribe o usuario na URL real de File Browser. O
// alumnado só ten que dar o nome do servidor (bac0.local, ou http://bac0.local): sen
// porto nin /arquivos. Primeiro vale o que escribiu tal cal (unha URL completa segue a
// funcionar) e, só se non é un File Browser, próbanse as formas habituais DESE host:
//   - un host sen porto proba o 80 (Apache) e o 8080, con e sen /arquivos;
//   - un porto escrito RESPÉTASE: só se completa a subruta, nunca se salta a outro porto.
//
// Nunca se cambia de servidor en silencio, porque as credenciais do alumno irían alá.
// Serve igual para o clásico e para Quantum. Devolve "" se ningunha responde; quen
// chama segue entón coa URL escrita e o erro real sae do login.
func resolverServidor(entrada string) string {
	base := normalizeServerURL(entrada)
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	// Se o nome é o DESTE equipo (o profesorado abrindo Tao no propio servidor: bac0.local
	// en bac0) vaise polo loopback: un equipo non resolve o seu nome .local por mDNS con
	// fiabilidade (aquí tardaba 10 s), e o servidor está precisamente aquí.
	if eEsteEquipo(u.Hostname()) {
		porto := u.Port() // ler ANTES de cambiar u.Host
		u.Host = "127.0.0.1"
		if porto != "" {
			u.Host += ":" + porto
		}
		if base = strings.TrimRight(u.String(), "/"); base == "" {
			return ""
		}
	}
	// Os nomes .local resólvense por mDNS, que pode tardar máis que o prazo curto das
	// sondaxes (2 s, DNS incluído): resólvese o nome primeiro, con calma, e sonda despois
	// (o resolutor do sistema garda o resultado). Un nome que non existe falla aquí, ao
	// momento, en vez de gastar o prazo de cada candidata.
	if !hostResolve(u.Hostname()) {
		return ""
	}
	if probarRaizFileBrowser(base) {
		return base
	}
	alternativas := []string{}
	for _, c := range candidatasDeHosts([]string{u.Hostname()}, base) {
		if cu, err := url.Parse(c); err != nil || c == base || (u.Port() != "" && cu.Port() != u.Port()) {
			continue
		}
		alternativas = append(alternativas, c)
	}
	return primeiraQueResponde(alternativas)
}

// tempoResolverHost é o prazo para resolver o nome do servidor (mDNS pode tardar).
const tempoResolverHost = 6 * time.Second

// hostResolve di se o nome (ou IP) do servidor se resolve. Un IP non precisa resolverse.
func hostResolve(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), tempoResolverHost)
	defer cancel()
	enderezos, err := net.DefaultResolver.LookupHost(ctx, host)
	return err == nil && len(enderezos) > 0
}

// eEsteEquipo di se host é o nome deste mesmo equipo: o nome curto (bac0), co seu ".local"
// (bac0.local) ou o nome completo que dá o sistema.
func eEsteEquipo(host string) bool {
	nome, err := os.Hostname()
	if err != nil {
		return false
	}
	host, nome = strings.ToLower(strings.TrimSpace(host)), strings.ToLower(strings.TrimSpace(nome))
	curto, _, _ := strings.Cut(nome, ".")
	return curto != "" && (host == nome || host == curto || host == curto+".local")
}
