package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Pipe Tao→Malta PARA O INFORME ASINADO (ReportBuilder.tsx, "Redacción por
// IA"): distinto e paralelo ao de tarefa.go (que é para os informes de
// seguimento — unidade/mensual, con métricas de temporalización). Aquí non
// hai unidades didácticas nin grao de consecución: Tao manda o rexistro
// diario xa escrito (as entradas do alcance escollido no informe asinado) e
// Malta devolve UN texto en prosa que o redacta — Tao amósao editable e só
// asina o que o usuario confirme (ver handleGenerate en ReportBuilder.tsx),
// nunca se asina directamente o que devolve a IA.

// ContextoTarefaRedaccion é o que Tao escribe cando lanza Malta para
// redactar un informe asinado. Espello de contextoTarefaRedaccion en
// tao/redaccion_launch.go. Lido pola variable de contorno
// TAO_TAREFA_MALTA_ASINADO (main.go).
type ContextoTarefaRedaccion struct {
	RequestID             string             `json:"requestId"`
	MateriaNome           string             `json:"materiaNome"`
	AcademicYear          string             `json:"academicYear"`
	ScopeDescription      string             `json:"scopeDescription"`
	Entradas              []EntradaRedaccion `json:"entradas"`
	PeticionPersonalizada string             `json:"peticionPersonalizada,omitempty"`
	APISocket             string             `json:"apiSocket"`
	APIToken              string             `json:"apiToken"`
}

// EntradaRedaccion é UN día do rexistro que hai que redactar — mesmo
// contido cru que xa escribiu o alumnado/profesorado, sen inventar nada
// novo, só reformulalo en prosa.
type EntradaRedaccion struct {
	Data    string `json:"data"`
	Status  string `json:"status"`
	Content string `json:"content"`
}

// ResultadoTarefaRedaccion é o que Malta devolve a Tao (POST
// /api/v1/malta/resultado-asinado) — éxito (Texto cheo) ou fallo (Erro
// cheo), nunca os dous.
type ResultadoTarefaRedaccion struct {
	RequestID  string `json:"requestId"`
	ProvedorIA string `json:"provedorIA,omitempty"`
	ModeloIA   string `json:"modeloIA,omitempty"`
	Texto      string `json:"texto,omitempty"`
	Erro       string `json:"erro,omitempty"`
}

// lerContextoTarefaRedaccion le o contexto pola variable de contorno
// TAO_TAREFA_MALTA_ASINADO. ok=false se non está presente ou está
// incompleto.
func lerContextoTarefaRedaccion() (ContextoTarefaRedaccion, bool) {
	var ctx ContextoTarefaRedaccion
	ruta := os.Getenv("TAO_TAREFA_MALTA_ASINADO")
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
	if ctx.APISocket == "" || ctx.APIToken == "" || ctx.RequestID == "" {
		return ctx, false
	}
	return ctx, true
}

// ExecutarTarefaRedaccionHeadless é o punto de entrada cando Malta arrinca
// cunha tarefa de redacción (main.go): NON amosa ningunha xanela — chama á
// IA e devolve sempre un resultado a Tao (éxito ou erro).
func ExecutarTarefaRedaccionHeadless(ctx ContextoTarefaRedaccion) {
	settings := loadSettings()
	resultado := ResultadoTarefaRedaccion{RequestID: ctx.RequestID}

	texto, err := redactarInforme(ctx, settings)
	if err != nil {
		log.Printf("[malta] erro redactando informe asinado: %v", err)
		resultado.Erro = err.Error()
	} else {
		resultado.Texto = texto
		resultado.ProvedorIA = settings.IAProvedor
		resultado.ModeloIA = settings.IAModel
	}

	if err := enviarResultadoRedaccion(ctx, resultado); err != nil {
		log.Printf("[malta] erro devolvendo a redacción a Tao: %v", err)
	}
}

// redactarInforme chama á IA para redactar o texto en prosa a partir das
// entradas do rexistro diario.
func redactarInforme(ctx ContextoTarefaRedaccion, settings Settings) (string, error) {
	if len(ctx.Entradas) == 0 {
		return "", fmt.Errorf("non hai rexistros que redactar neste alcance")
	}
	sistema, peticion := construirPromptRedaccion(ctx)
	resposta, err := chamarIA(settings, sistema, peticion)
	if err != nil {
		return "", err
	}
	texto := strings.TrimSpace(resposta)
	if texto == "" {
		return "", fmt.Errorf("a IA devolveu unha resposta baleira")
	}
	return texto, nil
}

// construirPromptRedaccion compón o par sistema/petición para chamarIA —
// aquí non se pide JSON (a diferenza de construirPrompt en informes.go): só
// un bloque de texto en prosa, que é todo o que fai falla.
func construirPromptRedaccion(ctx ContextoTarefaRedaccion) (sistema, peticion string) {
	sistema = `Es un asistente docente que redacta, en galego, o resumo narrativo dun
informe asinado de seguimento de prácticas para ensino secundario. A partir
do rexistro diario que se che dá (xa escrito polo alumnado ou profesorado
tal cal), escribe un texto en prosa correcto e profesional que integre nunha
narrativa fluída o traballado no período indicado — non te limites a
enumerar as datas unha a unha, conecta as ideas e o fío do traballado.

NON inventes contido que non estea no rexistro. NON uses formato Markdown
nin viñetas: só texto plano en parágrafos, separados por unha liña en
branco. Ton profesional e formal, propio dun informe educativo — nunca
coloquial. Devolve UNICAMENTE o texto redactado, sen título nin explicacións
adicionais.`

	var partes []string
	cabeceira := fmt.Sprintf("MATERIA: %s (%s)\nALCANCE: %s\n", ctx.MateriaNome, ctx.AcademicYear, ctx.ScopeDescription)
	partes = append(partes, cabeceira)

	var corpo strings.Builder
	for _, e := range ctx.Entradas {
		fmt.Fprintf(&corpo, "- %s (%s): %s\n", e.Data, e.Status, e.Content)
	}
	partes = append(partes, "REXISTRO DIARIO:\n"+corpo.String())

	if p := strings.TrimSpace(ctx.PeticionPersonalizada); p != "" {
		partes = append(partes, "INSTRUCIÓN ADICIONAL DO USUARIO (ten en conta isto ao redactar):\n"+p)
	}

	peticion = strings.Join(partes, "\n---\n")
	return sistema, peticion
}

const enviarResultadoRedaccionTimeout = 20 * time.Second

// enviarResultadoRedaccion fai POST http://tao/api/v1/malta/resultado-asinado
// sobre o socket Unix de ctx.APISocket, autenticado con ctx.APIToken — mesmo
// transporte cá enviarResultado de taoapi.go, endpoint distinto.
func enviarResultadoRedaccion(ctx ContextoTarefaRedaccion, resultado ResultadoTarefaRedaccion) error {
	corpo, err := json.Marshal(resultado)
	if err != nil {
		return err
	}

	httpClient := &http.Client{
		Timeout: enviarResultadoRedaccionTimeout,
		Transport: &http.Transport{
			DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(dialCtx, "unix", ctx.APISocket)
			},
		},
	}

	req, err := http.NewRequest(http.MethodPost, "http://tao/api/v1/malta/resultado-asinado", bytes.NewReader(corpo))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ctx.APIToken)

	res, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("non se puido contactar con Tao: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		return fmt.Errorf("Tao rexeitou o resultado (%d): %s", res.StatusCode, string(data))
	}
	return nil
}
