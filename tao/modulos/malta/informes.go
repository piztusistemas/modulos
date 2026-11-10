package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Construción do prompt e parseo da resposta da IA — a orquestración en si
// (que métricas calcular, cando chamar isto, e como devolver o resultado a
// Tao) vive en tarefa.go. O rigor nos días de diferenza e no grao de
// consecución vén SEMPRE do cálculo determinista de materias.go — a IA só
// redacta o resumo e a proposta de mellora a partir dese dato xa calculado,
// nunca o inventa (ver calcularMetricas(Unidade)/grauConsecucion).

// InformeUnidade é o resultado, para UNHA unidade didáctica, do informe
// pedido — parte do ResultadoTarefaMalta que Malta devolve a Tao (tarefa.go).
type InformeUnidade struct {
	UnitID             int    `json:"unitId"`
	Titulo             string `json:"titulo"`
	SesionsProgramadas int    `json:"sesionsProgramadas"`
	SesionsReais       int    `json:"sesionsReais"`
	DiferenciaDias     int    `json:"diferenciaDias"` // reais - programadas (negativo = por detrás do programado)
	PrimeiroDia        string `json:"primeiroDia,omitempty"`
	UltimoDia          string `json:"ultimoDia,omitempty"`
	GrauConsecucion    string `json:"grauConsecucion"`    // 'Alto'|'Medio'|'Baixo'|'' — ver grauConsecucion, materias.go
	Resumo             string `json:"resumo"`             // ≤200 palabras, redactado pola IA
	Proposta           string `json:"proposta,omitempty"` // proposta de modificación/mellora, se procede
	// Só no informe mensual: sesións dadas no mes, acumuladas ata o final do
	// mes, porcentaxe da unidade traballada ata ese momento (determinista,
	// materias.go) e a comparación, redactada pola IA, entre os contidos
	// rexistrados nas sesións do mes e os contidos programados da unidade.
	SesionsMes        int    `json:"sesionsMes,omitempty"`
	SesionsAcumuladas int    `json:"sesionsAcumuladas,omitempty"`
	Porcentaxe        int    `json:"porcentaxe,omitempty"`
	Comparacion       string `json:"comparacion,omitempty"`
}

// construirPrompt compón o par sistema/petición para chamarIA. tipo é
// "unidade" (unha soa entrada en metricas, sen rango) ou "mensual" (varias
// unidades, cun rango explícito) — só cambia como se presenta o encabezado,
// o formato de saída pedido é sempre o mesmo.
func construirPrompt(m MateriaDef, tipo, rangoFrom, rangoTo string, metricas []unidadeMetricas, peticionPersonalizada string) (sistema, peticion string) {
	if tipo == "mensual" {
		return construirPromptMensual(m, rangoFrom, rangoTo, metricas, peticionPersonalizada)
	}
	sistema = `Es un asistente docente que redacta informes de seguimento da programación
didáctica para o profesorado de ensino secundario en Galicia. Compara SEMPRE
o que estaba programado co que realmente se traballou na aula, apoiándote
UNICAMENTE nos datos numéricos xa calculados que se che dan (sesións
programadas, sesións reais, grao de consecución) — NUNCA inventes nin
recalcules eses números, xa veñen listos e son a fonte de verdade.

Para CADA unidade didáctica da lista, escribe:
- "resumo": un resumo profesional, en galego, de NON MÁIS DE 200 PALABRAS,
  que mencione explicitamente a diferenza de días/sesións entre o programado
  e o realmente traballado (co dato xa dado, sen recalculalo), valore o grao
  de consecución dos contidos (co dato xa dado), e describa brevemente os
  contidos efectivamente traballados. Ton profesional, formal, propio dun
  informe de inspección educativa — nunca coloquial.
- "proposta": unha proposta de modificación e/ou mellora da programación
  para esta unidade SE PROCEDE (ex. reaxustar sesións, reforzar contidos cun
  grao de consecución baixo) — cadea baleira "" se non hai nada que propoñer.

Devolve SÓ un obxecto JSON válido, sen texto adicional, sen explicacións,
sen valados markdown, coa forma exacta:
{"unidades":[{"unitId":<número>,"resumo":"<≤200 palabras>","proposta":"<ou \"\">"}, ...],"comentarioGlobal":"<opcional, 1-2 frases valorando o conxunto>"}
Inclúe UNHA entrada en "unidades" por cada unidade da lista que se che dá, na mesma orde.`

	var partes []string
	cabeceira := fmt.Sprintf("MATERIA: %s (%s)\n", m.Name, m.AcademicYear)
	if tipo == "mensual" {
		cabeceira += fmt.Sprintf("RANGO SOLICITADO: %s a %s\n", rangoFrom, rangoTo)
	}
	partes = append(partes, cabeceira)

	for _, um := range metricas {
		grau := grauConsecucion(um.Unit.Sessions, um.SesionsReais)
		partes = append(partes, fmt.Sprintf(
			"UNIDADE %d — %s\nObxectivos: %s\nContidos programados: %s\nSesións programadas: %d | Sesións reais: %d | Diferenza: %+d | Grao de consecución: %s\nPrimeiro/último día traballado: %s / %s\nRexistro de aula:\n%s\n",
			um.Unit.ID, um.Unit.Title,
			nonBaleiro(um.Unit.Obxectivos), nonBaleiro(um.Unit.Contidos),
			um.Unit.Sessions, um.SesionsReais, um.SesionsReais-um.Unit.Sessions, grau,
			nonBaleiro(um.PrimeiroDia), nonBaleiro(um.UltimoDia),
			logsComoTexto(um.Contidos)))
	}

	if p := strings.TrimSpace(peticionPersonalizada); p != "" {
		partes = append(partes, "INSTRUCIÓN ADICIONAL DO PROFESORADO (ten en conta isto ao redactar, sen deixar de cumprir o formato JSON e o límite de 200 palabras por unidade):\n"+p)
	}

	peticion = strings.Join(partes, "\n---\n")
	return sistema, peticion
}

// construirPromptMensual: o informe mensual non valora a unidade enteira,
// senón o que se fixo NESE mes: cantas sesións de cada unidade se deron, que
// porcentaxe da unidade leva traballada, e se os contidos que o profesorado
// rexistrou en "Contidos da sesión" casan cos contidos programados.
func construirPromptMensual(m MateriaDef, rangoFrom, rangoTo string, metricas []unidadeMetricas, peticionPersonalizada string) (sistema, peticion string) {
	sistema = `Es un asistente docente que redacta o informe mensual de seguimento da
programación didáctica para o profesorado de ensino secundario en Galicia.
Para cada unidade didáctica traballada no mes dánseche uns datos numéricos
XA CALCULADOS (sesións dadas no mes, sesións acumuladas desde o inicio,
sesións programadas e porcentaxe da unidade traballada): son a fonte de
verdade, NUNCA os inventes nin os recalcules.

Dánseche tamén os contidos programados da unidade (programación de aula) e
as notas que o profesorado escribiu en "Contidos da sesión" para cada
sesión do mes. Para CADA unidade da lista, escribe:
- "resumo": un resumo profesional, en galego, de NON MÁIS DE 150 PALABRAS,
  do traballado no mes nesa unidade, mencionando as sesións empregadas e a
  porcentaxe da unidade (cos datos xa dados).
- "comparacion": unha comparación, en galego, de NON MÁIS DE 200 PALABRAS,
  entre o rexistrado nas sesións e a programación de aula: que contidos
  programados se traballaron, cales están aínda pendentes (tendo en conta a
  porcentaxe da unidade xa traballada) e que se traballou que NON figura na
  programación. Básate SÓ nos textos dados; se non hai notas de sesión ou
  non hai contidos programados, dio expresamente en vez de inventar.
- "proposta": unha proposta de axuste ou mellora SE PROCEDE (ex. reforzar
  contidos pendentes, reaxustar a temporalización) — "" se non hai nada.
Ton profesional e formal, propio dun informe de departamento.

Devolve SÓ un obxecto JSON válido, sen texto adicional, sen explicacións,
sen valados markdown, coa forma exacta:
{"unidades":[{"unitId":<número>,"resumo":"...","comparacion":"...","proposta":"<ou \"\">"}, ...],"comentarioGlobal":"<1-2 frases valorando o mes no seu conxunto>"}
Inclúe UNHA entrada en "unidades" por cada unidade da lista que se che dá, na mesma orde.`

	partes := []string{fmt.Sprintf("MATERIA: %s (%s)\nMES/RANGO: %s a %s\n", m.Name, m.AcademicYear, rangoFrom, rangoTo)}
	for _, um := range metricas {
		partes = append(partes, fmt.Sprintf(
			"UNIDADE %d — %s\nObxectivos: %s\nContidos programados (programación de aula): %s\nCriterios de avaliación: %s\nSesións dadas no mes: %d | Sesións acumuladas ata o final do mes: %d de %d programadas | Porcentaxe da unidade traballada: %d%%\nContidos da sesión rexistrados no mes:\n%s\n",
			um.Unit.ID, um.Unit.Title,
			nonBaleiro(um.Unit.Obxectivos), nonBaleiro(um.Unit.Contidos), nonBaleiro(um.Unit.Criterios),
			um.SesionsMes, um.SesionsAcumuladas, um.Unit.Sessions,
			porcentaxeUnidade(um.Unit.Sessions, um.SesionsAcumuladas),
			logsComoTexto(um.Contidos)))
	}
	if p := strings.TrimSpace(peticionPersonalizada); p != "" {
		partes = append(partes, "INSTRUCIÓN ADICIONAL DO PROFESORADO (ten en conta isto ao redactar, sen deixar de cumprir o formato JSON e os límites de palabras):\n"+p)
	}
	return sistema, strings.Join(partes, "\n---\n")
}

func nonBaleiro(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(non definido)"
	}
	return s
}

func logsComoTexto(logs []Log) string {
	if len(logs) == 0 {
		return "(sen contido rexistrado)"
	}
	var sb strings.Builder
	for _, l := range logs {
		fmt.Fprintf(&sb, "- %s (%s): %s\n", l.Date, l.Status, l.Content)
	}
	return sb.String()
}

// respostaUnidadeIA é o resumo+proposta que a IA devolve para unha unidade.
type respostaUnidadeIA struct {
	Resumo      string
	Proposta    string
	Comparacion string
}

// respostaIA é a forma JSON que se lle pide á IA en construirPrompt.
type respostaIA struct {
	Unidades []struct {
		UnitID      json.Number `json:"unitId"`
		Resumo      string      `json:"resumo"`
		Proposta    string      `json:"proposta"`
		Comparacion string      `json:"comparacion"`
	} `json:"unidades"`
	ComentarioGlobal string `json:"comentarioGlobal"`
}

// parsearRespostaIA extrae o texto JSON da resposta (a IA pode devolvelo
// limpo grazas ao prompt, pero limparValadosMarkdown xa quitou calquera
// cerco ```; aquí só falta decodificalo) e devolve un mapa unitId->resposta.
func parsearRespostaIA(resposta string) (map[int]respostaUnidadeIA, string, error) {
	var r respostaIA
	if err := json.Unmarshal([]byte(resposta), &r); err != nil {
		return nil, "", fmt.Errorf("resposta non é JSON válido: %w", err)
	}
	respostas := map[int]respostaUnidadeIA{}
	for _, u := range r.Unidades {
		id, err := strconv.Atoi(u.UnitID.String())
		if err != nil {
			continue
		}
		respostas[id] = respostaUnidadeIA{
			Resumo:      strings.TrimSpace(u.Resumo),
			Proposta:    strings.TrimSpace(u.Proposta),
			Comparacion: strings.TrimSpace(u.Comparacion),
		}
	}
	if len(respostas) == 0 {
		return nil, "", fmt.Errorf("a resposta non trae ningún resumo por unidade")
	}
	return respostas, strings.TrimSpace(r.ComentarioGlobal), nil
}
