package main

import (
	"fmt"
	"path/filepath"
)

// Métodos bindeados ao frontend para xestionar os módulos externos DE Tao —
// equivalentes, en pequeno, aos de piztu/app.go (ModulosDispo, SetModulo,
// InstalarModulo, ActualizarModulo, AbrirModulo, RutaModulos,
// AbrirCartafolModulos, ComprobarActualizacionsModulos). Todos gateados a
// requireProfessor() (patrón xa usado por InstalarYang/AbrirYangParaTarefa):
// Tao corre tamén en máquinas de alumnado, e instalar/activar/lanzar
// executables non pode estar ao seu alcance.

// ModuloInfoTao é o que ve o frontend por cada módulo — xa instalado ou só
// dispoñible no catálogo remoto (ver ModulosDispoTao).
type ModuloInfoTao struct {
	ID                string   `json:"id"`
	Nome              string   `json:"nome"`
	Icona             string   `json:"icona"`
	IconaImaxeDataURI string   `json:"iconaImaxeDataURI"`
	Activo            bool     `json:"activo"`
	Instalado         bool     `json:"instalado"`
	Actualizable      bool     `json:"actualizable"`
	Version           string   `json:"version"`
	VersionNova       string   `json:"versionNova"`
	Autor             string   `json:"autor"`
	Email             string   `json:"email"`
	Web               string   `json:"web"`
	Descricion        string   `json:"descricion"`
	Permisos          []string `json:"permisos"`
	Ruta              string   `json:"ruta"`
}

// ModulosDispoTao lista os módulos xa instalados (ExternosTao) máis
// calquera outro dispoñible no catálogo remoto (CatalogoModulosTao) que
// aínda non estea instalado — se non hai rede ou o repo está baleiro,
// simplemente non aparece ningún "só dispoñible" nin marca de
// actualización, sen erro.
func (a *App) ModulosDispoTao() []ModuloInfoTao {
	instalados := ExternosTao(a.taoModulosDir)
	catalogo, _ := CatalogoModulosTao() // ok=false → catalogo nil, está ben

	vistos := map[string]bool{}
	resultado := make([]ModuloInfoTao, 0, len(instalados))
	for _, m := range instalados {
		vistos[m.ID] = true
		nova := catalogo[m.ID]
		resultado = append(resultado, ModuloInfoTao{
			ID: m.ID, Nome: m.Nome, Icona: m.Icona, IconaImaxeDataURI: m.IconaImaxeDataURI,
			Activo: a.moduloTaoActivo(m.ID), Instalado: true,
			Version: m.Version, VersionNova: nova,
			Actualizable: nova != "" && versionMaior(nova, m.Version),
			Autor:        m.Autor, Email: m.Email, Web: m.Web, Descricion: m.Descricion, Permisos: m.Permisos, Ruta: m.Dir,
		})
	}
	for id, version := range catalogo {
		if vistos[id] {
			continue
		}
		resultado = append(resultado, ModuloInfoTao{ID: id, Nome: id, Instalado: false, VersionNova: version})
	}
	return resultado
}

// SetModuloTao activa/desactiva id — igual criterio ca SetModulo de Piztu:
// activar concede de golpe os Permisos declarados no seu modulo.json;
// desactivar esquéceos e revoga no acto calquera token de API xa emitido
// para ese módulo (RevogarModulo), sen agardar ao seu TTL.
func (a *App) SetModuloTao(id string, activo bool) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}

	if !activo {
		if err := a.setModuloTaoActivo(id, false, nil); err != nil {
			return err
		}
		if a.api != nil {
			a.api.RevogarModulo(id)
		}
		a.avisarModulosTaoCambiados()
		return nil
	}

	m, ok := BuscarModuloTao(a.taoModulosDir, id)
	if !ok {
		return fmt.Errorf("non se atopa o módulo %q instalado", id)
	}
	if err := a.setModuloTaoActivo(id, true, m.Permisos); err != nil {
		return err
	}
	a.avisarModulosTaoCambiados()
	return nil
}

// InstalarModuloTao descarga id dende repoModulosTaoGitHub (non fai nada se
// xa está instalado — mesmo criterio ca InstalarYang/Instalar() de Piztu).
func (a *App) InstalarModuloTao(id string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if a.taoModulosDir == "" {
		return fmt.Errorf("non se puido determinar o cartafol de módulos de Tao")
	}
	if err := InstalarModuloTaoDesdeRepo(a.taoModulosDir, id); err != nil {
		return err
	}
	a.avisarModulosTaoCambiados()
	return nil
}

// ActualizarModuloTao bota fóra a copia instalada e reinstala a última
// versión. Se o módulo estaba activo, refresca os permisos concedidos (por
// se o modulo.json novo declara outros) e revoga os tokens da versión
// anterior.
func (a *App) ActualizarModuloTao(id string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if a.taoModulosDir == "" {
		return fmt.Errorf("non se puido determinar o cartafol de módulos de Tao")
	}
	activo := a.moduloTaoActivo(id)

	if err := ActualizarModuloTaoDesdeRepo(a.taoModulosDir, id); err != nil {
		return err
	}

	if activo {
		if m, ok := BuscarModuloTao(a.taoModulosDir, id); ok {
			_ = a.setModuloTaoActivo(id, true, m.Permisos)
		}
		if a.api != nil {
			a.api.RevogarModulo(id)
		}
	}
	a.avisarModulosTaoCambiados()
	return nil
}

// AbrirModuloTao lanza id coma proceso fillo, coa mesma lóxica de
// "inactivo é coma se non estivese instalado" (piztu/app.go moduloActivo).
func (a *App) AbrirModuloTao(id string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if !a.moduloTaoActivo(id) {
		return fmt.Errorf("o módulo %q está desactivado (⚙ → Módulos)", id)
	}
	m, ok := BuscarModuloTao(a.taoModulosDir, id)
	if !ok {
		return fmt.Errorf("non se atopa o módulo %q instalado", id)
	}

	ctx := ContextoModuloTao{
		Version:   1,
		ModuloID:  m.ID,
		ModuloDir: m.Dir,
		Idioma:    a.idiomaPiztu,
	}
	if a.api != nil {
		permisos := a.lerEstadoModulosTao()[id].Permisos
		ctx.APISocket = a.api.rutaSocket()
		ctx.APIToken = a.api.EmitirTokenModulo(id, permisos)
		ctx.APIPermisos = permisos
	}

	rutaContexto, err := escribirContextoModuloTao(ctx)
	if err != nil {
		return fmt.Errorf("non se puido preparar o contexto do módulo: %w", err)
	}
	return lanzarAplicacionModuloTao(filepath.Join(m.Dir, m.Executable), rutaContexto)
}

// RutaModulosTao devolve o cartafol de módulos externos de Tao ("" se non se
// puido determinar en startup()).
func (a *App) RutaModulosTao() string {
	return a.taoModulosDir
}

// AbrirCartafolModulosTao abre o cartafol de módulos co xestor de ficheiros
// do sistema — reutiliza OpenFile (app.go), que xa sabe facelo por
// plataforma (xdg-open/open/rundll32).
func (a *App) AbrirCartafolModulosTao() error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	if a.taoModulosDir == "" {
		return fmt.Errorf("non se puido determinar o cartafol de módulos de Tao")
	}
	return a.OpenFile(a.taoModulosDir)
}

// ComprobarActualizacionsModulosTao consulta o catálogo remoto (id →
// versión dispoñible) — só lectura, sen gate de rol, mesmo criterio ca
// YangDispoñible.
func (a *App) ComprobarActualizacionsModulosTao() map[string]string {
	catalogo, _ := CatalogoModulosTao()
	return catalogo
}

func (a *App) avisarModulosTaoCambiados() {
	emitirEvento("modulos_tao_cambiado")
}
