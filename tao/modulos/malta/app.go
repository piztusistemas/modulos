package main

import "context"

// App é o núcleo da interface de Malta — hoxe só a configuración da IA
// (settings.go): a xeración de informes en si non pasa por aquí, faina
// ExecutarTarefaHeadless (tarefa.go) sen amosar ningunha xanela.
type App struct {
	ctx      context.Context
	contexto ContextoModuloTao
	settings Settings
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.contexto, _ = lerContextoModuloTao()
	a.settings = loadSettings()
}

// ContextoVista é o subconxunto de ContextoModuloTao seguro de expoñer ao
// frontend — sen APIToken: o token só o usa taoapi.go dende Go, nunca fai
// falla no JS, e non hai motivo para aumentar a superficie se algún día
// houbese un fallo de XSS no propio frontend de Malta.
type ContextoVista struct {
	ModuloID  string `json:"moduloId"`
	Idioma    string `json:"idioma"`
	Conectado bool   `json:"conectado"`
}

// GetContexto exponse ao frontend para saber se Malta se abriu correctamente
// dende Tao (Conectado=true) ou fóra del — nese caso o frontend amosa un
// aviso en vez de intentar chamar á API.
func (a *App) GetContexto() ContextoVista {
	return ContextoVista{
		ModuloID:  a.contexto.ModuloID,
		Idioma:    a.contexto.Idioma,
		Conectado: a.contexto.APISocket != "" && a.contexto.APIToken != "",
	}
}
