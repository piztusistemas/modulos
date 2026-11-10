package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// Purga de rexistros orfos.
//
// Un alumno non vive nun só sitio: a conta está en File Browser, pero a identidade
// está espallada polo roster compartido (dixitalizacion/alumnos.json), o mapeo de
// UUID (alumnos-uuid.json), a lista de profesores (profesores.json), os perfís locais
// deste equipo (profiles.json), o marcador de sesión que ve o mapa de Piztu
// (dixitalizacion/sesions/<equipo>.json) e o cartafol do servidor (users/<login>).
//
// DeleteUser (admin_fb.go) limpa todo iso á vez, pero só se a baixa se fai DENDE Tao.
// Se as contas se borran por fóra (a interface web de File Browser, a súa CLI...), os
// rexistros quedan orfos e o alumnado segue aparecendo na lista do profesorado, que le
// alumnos.json e non as contas reais (ver ListRoster en profesor.go).
//
// Isto repárao: ListarOrfos amosa que quedaría por limpar (non toca nada) e PurgarOrfos
// elimínao. O borrado dos cartafoles de traballo vai aparte (incluirCartafoles), porque
// eses si conteñen o traballo do alumnado e non se recuperan.

// InformeOrfos lista os rexistros que quedaron sen conta en File Browser, agrupados
// polo sitio onde están. Devólvese tanto no modo consulta como despois de purgar (aí
// describe o que se eliminou).
type InformeOrfos struct {
	Roster       []string `json:"roster"`       // entradas de alumnos.json
	UUID         []string `json:"uuid"`         // entradas de alumnos-uuid.json
	Profesores   []string `json:"profesores"`   // entradas de profesores.json
	PerfisLocais []string `json:"perfisLocais"` // entradas de profiles.json (só neste equipo)
	Sesions      []string `json:"sesions"`      // ficheiros sesions/<equipo>.json
	Cartafoles   []string `json:"cartafoles"`   // cartafoles users/<login> do servidor
	Total        int      `json:"total"`        // suma de todo o anterior
	Contas       int      `json:"contas"`       // contas reais atopadas en File Browser (contexto)
}

// datosOrfos é a foto dos rexistros que se comparan contra as contas reais. Sepárase
// da lóxica de decisión para poder probala sen servidor (ver calcularOrfos).
type datosOrfos struct {
	contas     []string // usernames con conta real en File Browser
	roster     map[string]RosterEntry
	uuids      map[string]string
	profesores []string
	perfis     []storedProfile
	sesions    map[string]string // ficheiro sesions/<equipo>.json -> usuario que declara
	cartafoles []string          // nomes de cartafol dentro de users/
	protexidos []string          // nunca se consideran orfos (director, admin de FB...)
}

// calcularOrfos decide, sen tocar a rede, que rexistros non teñen conta. É unha función
// pura: recibe a foto e devolve o informe.
func calcularOrfos(d datosOrfos) InformeOrfos {
	viva := map[string]bool{}
	for _, c := range d.contas {
		viva[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for _, p := range d.protexidos {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			viva[p] = true
		}
	}
	orfo := func(username string) bool {
		username = strings.ToLower(strings.TrimSpace(username))
		return username != "" && !viva[username]
	}

	inf := InformeOrfos{Contas: len(d.contas)}
	for username := range d.roster {
		if orfo(username) {
			inf.Roster = append(inf.Roster, username)
		}
	}
	for username := range d.uuids {
		if orfo(username) {
			inf.UUID = append(inf.UUID, username)
		}
	}
	for _, username := range d.profesores {
		if orfo(username) {
			inf.Profesores = append(inf.Profesores, strings.TrimSpace(username))
		}
	}
	for _, p := range d.perfis {
		if orfo(p.Username) {
			inf.PerfisLocais = append(inf.PerfisLocais, p.Username)
		}
	}
	for ficheiro, username := range d.sesions {
		if orfo(username) {
			inf.Sesions = append(inf.Sesions, ficheiro)
		}
	}
	for _, nome := range d.cartafoles {
		if orfo(nome) {
			inf.Cartafoles = append(inf.Cartafoles, nome)
		}
	}

	for _, l := range [][]string{inf.Roster, inf.UUID, inf.Profesores, inf.PerfisLocais, inf.Sesions, inf.Cartafoles} {
		sort.Strings(l)
		inf.Total += len(l)
	}
	return inf
}

// ListarOrfos consulta o servidor e devolve o que quedaría por limpar, sen modificar
// nada. É o "modo simulación" que amosa o panel antes de deixar purgar.
func (a *App) ListarOrfos() (*InformeOrfos, error) {
	var inf InformeOrfos
	err := a.withAdminFB(func() error {
		d, err := a.recollerDatosOrfos()
		if err != nil {
			return err
		}
		inf = calcularOrfos(*d)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &inf, nil
}

// PurgarOrfos elimina os rexistros sen conta e devolve o informe do que se eliminou.
// Os cartafoles users/<login> só se borran con incluirCartafoles=true: conteñen os
// traballos entregados e o borrado non ten volta.
func (a *App) PurgarOrfos(incluirCartafoles bool) (*InformeOrfos, error) {
	var inf InformeOrfos
	err := a.withAdminFB(func() error {
		d, err := a.recollerDatosOrfos()
		if err != nil {
			return err
		}
		inf = calcularOrfos(*d)
		if !incluirCartafoles {
			// Non se tocan: quítanse do informe para non dicir que se borraron.
			inf.Total -= len(inf.Cartafoles)
			inf.Cartafoles = nil
		}
		if inf.Total == 0 {
			return nil
		}

		if len(inf.Roster) > 0 {
			roster := d.roster
			for _, u := range inf.Roster {
				for k := range roster {
					if strings.EqualFold(k, u) {
						delete(roster, k)
					}
				}
			}
			if err := a.fbWriteSharedJSON(sharedAlumnos, roster); err != nil {
				return fmt.Errorf("non se puido actualizar %s: %w", sharedAlumnos, err)
			}
		}
		if len(inf.UUID) > 0 {
			uuids := d.uuids
			for _, u := range inf.UUID {
				for k := range uuids {
					if strings.EqualFold(k, u) {
						delete(uuids, k)
					}
				}
			}
			if err := a.fbWriteSharedJSON(sharedAlumnosUUID, uuids); err != nil {
				return fmt.Errorf("non se puido actualizar %s: %w", sharedAlumnosUUID, err)
			}
		}
		if len(inf.Profesores) > 0 {
			quitar := map[string]bool{}
			for _, u := range inf.Profesores {
				quitar[strings.ToLower(u)] = true
			}
			kept := []string{}
			for _, p := range d.profesores {
				if !quitar[strings.ToLower(strings.TrimSpace(p))] {
					kept = append(kept, strings.TrimSpace(p))
				}
			}
			if err := a.fbWriteSharedJSON(sharedProfesores, kept); err != nil {
				return fmt.Errorf("non se puido actualizar %s: %w", sharedProfesores, err)
			}
		}
		for _, u := range inf.PerfisLocais {
			if err := a.removeIdentity(u); err != nil {
				log.Printf("[purga] non se puido limpar o perfil local de %q: %v", u, err)
			}
		}
		// Best-effort a partir de aquí: son ficheiros soltos, un fallo nun non debe
		// deixar a purga a medias nos demais.
		for _, f := range inf.Sesions {
			if err := a.fbDelete(sharedRoot + "/" + fbSesionsDir + "/" + f); err != nil {
				log.Printf("[purga] non se puido borrar a sesión %q: %v", f, err)
			}
		}
		for _, nome := range inf.Cartafoles {
			if err := a.fbDelete(fbUsersRoot + "/" + nome); err != nil {
				log.Printf("[purga] non se puido borrar o cartafol de %q: %v", nome, err)
			}
		}
		log.Printf("[purga] eliminados %d rexistros orfos (cartafoles: %v)", inf.Total, incluirCartafoles)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &inf, nil
}

// recollerDatosOrfos le a foto completa do servidor. Require unha sesión de
// administrador activa (chámase sempre dentro de withAdminFB).
func (a *App) recollerDatosOrfos() (*datosOrfos, error) {
	users, err := a.fbListUsers()
	if err != nil {
		return nil, err
	}
	d := &datosOrfos{
		roster:  map[string]RosterEntry{},
		uuids:   map[string]string{},
		sesions: map[string]string{},
	}
	for _, u := range users {
		if name, _ := u["username"].(string); strings.TrimSpace(name) != "" {
			d.contas = append(d.contas, name)
		}
	}
	// Salvagarda: se o servidor responde cunha lista baleira (permisos, versión de
	// File Browser distinta...) TODO rexistro parecería orfo. Mellor non facer nada.
	if len(d.contas) == 0 {
		return nil, fmt.Errorf("o servidor non devolveu ningunha conta; non se purga nada por seguridade")
	}

	a.fbReadSharedJSON(sharedAlumnos, &d.roster)
	a.fbReadSharedJSON(sharedAlumnosUUID, &d.uuids)
	a.fbReadSharedJSON(sharedProfesores, &d.profesores)
	d.perfis, _ = a.loadProfiles()

	// Marcadores de sesión: sesions/<equipo>.json di quen está a usar cada equipo; se
	// ese usuario xa non existe, o nome queda pegado no mapa de Piztu.
	if items, err := a.fbList(sharedRoot + "/" + fbSesionsDir); err == nil {
		for _, it := range items {
			if it.IsDir || !strings.HasSuffix(strings.ToLower(it.Name), ".json") {
				continue
			}
			var s sesionTao
			if _, err := a.fbReadSharedJSON(fbSesionsDir+"/"+it.Name, &s); err != nil {
				continue
			}
			if strings.TrimSpace(s.Usuario) != "" {
				d.sesions[it.Name] = s.Usuario
			}
		}
	}

	// Cartafoles persoais do servidor.
	if items, err := a.fbList(fbUsersRoot); err == nil {
		for _, it := range items {
			if it.IsDir {
				d.cartafoles = append(d.cartafoles, it.Name)
			}
		}
	}

	d.protexidos = []string{directorUser, a.fbUser}
	if cfg, _ := a.GetAdminConfig(); cfg != nil {
		d.protexidos = append(d.protexidos, cfg.FBAdminUser)
	}
	return d, nil
}
