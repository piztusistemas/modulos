package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// Pipe Tao→Malta: distinto e paralelo a ContextoModuloTao (contexto.go, só
// para a apertura normal → configuración de IA). Mesmo patrón có pipe
// Tao→Yang xa existente (yang/contexto_tao.go, TAO_CONTEXTO, ver
// docs/api-tao.md §10) — coa diferenza clave de que aquí NON hai interface:
// Tao xa manda TODOS os datos (a materia completa: programación + contidos
// traballados) e a instrución do que xerar; Malta procesa e remata sen
// amosar ningunha xanela, devolvendo o resultado por chamada de retorno
// (enviarResultado, taoapi.go). O profesorado nunca ve Malta neste fluxo.

// ContextoTarefaMalta é o que Tao escribe cando lanza Malta para xerar UN
// informe de forma autónoma. Espello de contextoTarefaMalta en
// tao/malta_launch.go. Lido pola variable de contorno TAO_TAREFA_MALTA
// (main.go decide, ao arrincar, entre este camiño headless e a apertura
// normal — nunca os dous á vez).
type ContextoTarefaMalta struct {
	Tipo                  string     `json:"tipo"` // "unidade" | "mensual"
	Materia               MateriaDef `json:"materia"`
	RangoFrom             string     `json:"rangoFrom,omitempty"`
	RangoTo               string     `json:"rangoTo,omitempty"`
	UnitIDObxectivo       int        `json:"unitIdObxectivo,omitempty"` // só tipo "unidade"
	PeticionPersonalizada string     `json:"peticionPersonalizada,omitempty"`
	APISocket             string     `json:"apiSocket"`
	APIToken              string     `json:"apiToken"`
}

// ResultadoTarefaMalta é o que Malta devolve a Tao (POST
// /api/v1/malta/resultado) — éxito (Unidades cheo) ou fallo (Erro cheo),
// nunca os dous. Tao é quen decide que facer con el: gardalo en File
// Browser e/ou reflectilo na interface de Informes (ver
// tao/malta_resultado.go).
type ResultadoTarefaMalta struct {
	Tipo                  string           `json:"tipo"`
	MateriaID             string           `json:"materiaId"`
	MateriaNome           string           `json:"materiaNome"`
	RangoFrom             string           `json:"rangoFrom,omitempty"`
	RangoTo               string           `json:"rangoTo,omitempty"`
	UnitIDObxectivo       int              `json:"unitIdObxectivo,omitempty"`
	ProvedorIA            string           `json:"provedorIA,omitempty"`
	ModeloIA              string           `json:"modeloIA,omitempty"`
	PeticionPersonalizada string           `json:"peticionPersonalizada,omitempty"`
	Unidades              []InformeUnidade `json:"unidades,omitempty"`
	ComentarioGlobal      string           `json:"comentarioGlobal,omitempty"`
	Erro                  string           `json:"erro,omitempty"`
}

// lerContextoTarefaMalta le o contexto pola variable de contorno
// TAO_TAREFA_MALTA. ok=false (arranque normal, mostra a interface de
// axustes) se non está presente ou está incompleto.
func lerContextoTarefaMalta() (ContextoTarefaMalta, bool) {
	var ctx ContextoTarefaMalta
	ruta := os.Getenv("TAO_TAREFA_MALTA")
	if ruta == "" {
		return ctx, false
	}
	dados, err := os.ReadFile(ruta)
	if err != nil {
		return ctx, false
	}
	if err := json.Unmarshal(dados, &ctx); err != nil {
		return ctx, false
	}
	if ctx.APISocket == "" || ctx.APIToken == "" || ctx.Materia.ID == "" {
		return ctx, false
	}
	return ctx, true
}

// ExecutarTarefaHeadless é o punto de entrada cando Malta arrinca cunha
// tarefa (main.go): NON amosa ningunha xanela — calcula as métricas
// (deterministicamente, materias.go), chama á IA, e devolve sempre un
// resultado a Tao (éxito ou erro) para que a interface de Informes nunca
// quede agardando sen máis.
func ExecutarTarefaHeadless(ctx ContextoTarefaMalta) {
	settings := loadSettings()
	resultado := ResultadoTarefaMalta{
		Tipo: ctx.Tipo, MateriaID: ctx.Materia.ID, MateriaNome: ctx.Materia.Name,
		RangoFrom: ctx.RangoFrom, RangoTo: ctx.RangoTo, UnitIDObxectivo: ctx.UnitIDObxectivo,
		PeticionPersonalizada: ctx.PeticionPersonalizada,
	}

	unidades, comentarioGlobal, err := xerarInforme(ctx, settings)
	if err != nil {
		log.Printf("[malta] erro xerando informe: %v", err)
		resultado.Erro = err.Error()
	} else {
		resultado.Unidades = unidades
		resultado.ComentarioGlobal = comentarioGlobal
		resultado.ProvedorIA = settings.IAProvedor
		resultado.ModeloIA = settings.IAModel
	}

	if err := enviarResultado(ctx, resultado); err != nil {
		log.Printf("[malta] erro devolvendo o resultado a Tao: %v", err)
	}
}

// xerarInforme escolle as métricas segundo ctx.Tipo, chama á IA e monta a
// lista de InformeUnidade — núcleo compartido polos dous tipos de tarefa.
func xerarInforme(ctx ContextoTarefaMalta, settings Settings) ([]InformeUnidade, string, error) {
	var metricas []unidadeMetricas
	switch ctx.Tipo {
	case "unidade":
		um, ok := calcularMetricasUnidade(ctx.Materia, ctx.UnitIDObxectivo)
		if !ok {
			return nil, "", fmt.Errorf("non se atopou a unidade %d na materia", ctx.UnitIDObxectivo)
		}
		metricas = []unidadeMetricas{um}
	case "mensual":
		metricas = calcularMetricas(ctx.Materia, ctx.RangoFrom, ctx.RangoTo)
		if len(metricas) == 0 {
			return nil, "", fmt.Errorf("non hai contidos traballados nin unidades programadas nese rango de datas")
		}
	default:
		return nil, "", fmt.Errorf("tipo de tarefa descoñecido: %q", ctx.Tipo)
	}

	sistema, peticion := construirPrompt(ctx.Materia, ctx.Tipo, ctx.RangoFrom, ctx.RangoTo, metricas, ctx.PeticionPersonalizada)
	resposta, err := chamarIA(settings, sistema, peticion)
	if err != nil {
		return nil, "", err
	}
	respostas, comentarioGlobal, err := parsearRespostaIA(resposta)
	if err != nil {
		return nil, "", fmt.Errorf("a IA non devolveu un informe interpretable: %w", err)
	}

	var unidades []InformeUnidade
	for _, um := range metricas {
		r := respostas[um.Unit.ID]
		iu := InformeUnidade{
			UnitID: um.Unit.ID, Titulo: um.Unit.Title,
			SesionsProgramadas: um.Unit.Sessions, SesionsReais: um.SesionsReais,
			DiferenciaDias: um.SesionsReais - um.Unit.Sessions,
			PrimeiroDia:    um.PrimeiroDia, UltimoDia: um.UltimoDia,
			GrauConsecucion: grauConsecucion(um.Unit.Sessions, um.SesionsReais),
			Resumo:          r.Resumo, Proposta: r.Proposta,
		}
		if ctx.Tipo == "mensual" {
			// No mensual o que conta é o traballado ATA ese mes: "reais" son
			// as acumuladas ata o final do rango, non as de todo o curso
			// (que, polo reparto secuencial do calendario, sempre coinciden
			// coas programadas e non dicían nada).
			iu.SesionsReais = um.SesionsAcumuladas
			iu.DiferenciaDias = um.SesionsAcumuladas - um.Unit.Sessions
			iu.GrauConsecucion = ""
			iu.SesionsMes = um.SesionsMes
			iu.SesionsAcumuladas = um.SesionsAcumuladas
			iu.Porcentaxe = porcentaxeUnidade(um.Unit.Sessions, um.SesionsAcumuladas)
			iu.Comparacion = r.Comparacion
		}
		unidades = append(unidades, iu)
	}
	return unidades, comentarioGlobal, nil
}
