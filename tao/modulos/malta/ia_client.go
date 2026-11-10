package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Cliente de IA xenérico — porte case literal de yang/ia_client.go (mesmos
// tres formatos de fío, mesma lóxica de despacho por provedor): fala con
// calquera provedor que use un destes formatos, o que cobre practicamente
// todo o mercado sen escribir un adaptador por marca.
type ProvedorIA string

const (
	ProvedorGemini    ProvedorIA = "gemini"
	ProvedorAnthropic ProvedorIA = "anthropic"
	ProvedorOpenAI    ProvedorIA = "openai" // tamén calquera "compatible con OpenAI"
)

var iaHTTPClient = &http.Client{Timeout: 120 * time.Second}

func baseURLPorDefecto(provedor ProvedorIA) string {
	switch provedor {
	case ProvedorGemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	case ProvedorAnthropic:
		return "https://api.anthropic.com/v1"
	default:
		return "https://api.openai.com/v1"
	}
}

// chamarIA é o punto de entrada único (ver GerarInforme, informes.go).
func chamarIA(s Settings, sistema, peticion string) (string, error) {
	if s.IAAPIKey == "" {
		return "", fmt.Errorf("falta a clave da API de IA (configúraa en Axustes)")
	}
	if s.IAModel == "" {
		return "", fmt.Errorf("falta o modelo de IA (configúrao en Axustes)")
	}

	provedor := ProvedorIA(s.IAProvedor)
	if provedor == "" {
		provedor = ProvedorGemini
	}
	baseURL := strings.TrimRight(strings.TrimSpace(s.IABaseURL), "/")
	if baseURL == "" {
		baseURL = baseURLPorDefecto(provedor)
	}

	switch provedor {
	case ProvedorAnthropic:
		return chamarAnthropic(baseURL, s.IAAPIKey, s.IAModel, sistema, peticion)
	case ProvedorGemini:
		return chamarGemini(baseURL, s.IAAPIKey, s.IAModel, sistema, peticion)
	default:
		return chamarOpenAICompatible(baseURL, s.IAAPIKey, s.IAModel, sistema, peticion)
	}
}

// ── Gemini (nativo) ──────────────────────────────────────────────────────

type geminiPeticion struct {
	SystemInstruction *geminiContido  `json:"systemInstruction,omitempty"`
	Contents          []geminiContido `json:"contents"`
}

type geminiContido struct {
	Role  string        `json:"role,omitempty"`
	Parts []geminiParte `json:"parts"`
}

type geminiParte struct {
	Text string `json:"text,omitempty"`
}

type geminiResposta struct {
	Candidates []struct {
		Content geminiContido `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func chamarGemini(baseURL, apiKey, model, sistema, peticion string) (string, error) {
	corpo := geminiPeticion{
		SystemInstruction: &geminiContido{Role: "user", Parts: []geminiParte{{Text: sistema}}},
		Contents: []geminiContido{
			{Role: "user", Parts: []geminiParte{{Text: peticion}}},
		},
	}
	dados, err := json.Marshal(corpo)
	if err != nil {
		return "", err
	}

	url := baseURL + "/models/" + model + ":generateContent"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(dados))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := iaHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("non se puido contactar coa IA: %w", err)
	}
	defer resp.Body.Close()

	corpoResposta, _ := io.ReadAll(resp.Body)
	var r geminiResposta
	if err := json.Unmarshal(corpoResposta, &r); err != nil {
		return "", fmt.Errorf("resposta non válida da IA (HTTP %d)", resp.StatusCode)
	}
	if r.Error != nil {
		return "", fmt.Errorf("erro da IA: %s", r.Error.Message)
	}
	if resp.StatusCode != http.StatusOK || len(r.Candidates) == 0 || len(r.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("a IA non devolveu resultado (HTTP %d)", resp.StatusCode)
	}

	return limparValadosMarkdown(r.Candidates[0].Content.Parts[0].Text), nil
}

// ── Anthropic (nativo, Claude) ───────────────────────────────────────────

type anthropicPeticion struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMensaxe `json:"messages"`
}

type anthropicMensaxe struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResposta struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func chamarAnthropic(baseURL, apiKey, model, sistema, peticion string) (string, error) {
	corpo := anthropicPeticion{
		Model:     model,
		MaxTokens: 8192,
		System:    sistema,
		Messages:  []anthropicMensaxe{{Role: "user", Content: peticion}},
	}
	dados, err := json.Marshal(corpo)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/messages", bytes.NewReader(dados))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := iaHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("non se puido contactar coa IA: %w", err)
	}
	defer resp.Body.Close()

	corpoResposta, _ := io.ReadAll(resp.Body)
	var r anthropicResposta
	if err := json.Unmarshal(corpoResposta, &r); err != nil {
		return "", fmt.Errorf("resposta non válida da IA (HTTP %d)", resp.StatusCode)
	}
	if r.Error != nil {
		return "", fmt.Errorf("erro da IA: %s", r.Error.Message)
	}
	if resp.StatusCode != http.StatusOK || len(r.Content) == 0 {
		return "", fmt.Errorf("a IA non devolveu resultado (HTTP %d)", resp.StatusCode)
	}

	var texto strings.Builder
	for _, bloque := range r.Content {
		if bloque.Type == "text" {
			texto.WriteString(bloque.Text)
		}
	}
	return limparValadosMarkdown(texto.String()), nil
}

// ── OpenAI / "compatible con OpenAI" ─────────────────────────────────────

type openAIPeticion struct {
	Model    string          `json:"model"`
	Messages []openAIMensaxe `json:"messages"`
}

type openAIMensaxe struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResposta struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func chamarOpenAICompatible(baseURL, apiKey, model, sistema, peticion string) (string, error) {
	corpo := openAIPeticion{
		Model: model,
		Messages: []openAIMensaxe{
			{Role: "system", Content: sistema},
			{Role: "user", Content: peticion},
		},
	}
	dados, err := json.Marshal(corpo)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(dados))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := iaHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("non se puido contactar coa IA: %w", err)
	}
	defer resp.Body.Close()

	corpoResposta, _ := io.ReadAll(resp.Body)
	var r openAIResposta
	if err := json.Unmarshal(corpoResposta, &r); err != nil {
		return "", fmt.Errorf("resposta non válida da IA (HTTP %d)", resp.StatusCode)
	}
	if r.Error != nil {
		return "", fmt.Errorf("erro da IA: %s", r.Error.Message)
	}
	if resp.StatusCode != http.StatusOK || len(r.Choices) == 0 {
		return "", fmt.Errorf("a IA non devolveu resultado (HTTP %d)", resp.StatusCode)
	}

	return limparValadosMarkdown(r.Choices[0].Message.Content), nil
}

// limparValadosMarkdown quita un posible cerco ```...``` que a IA engada
// malia que llo pedimos explicitamente que non o faga.
func limparValadosMarkdown(texto string) string {
	t := strings.TrimSpace(texto)
	if strings.HasPrefix(t, "```") {
		primeiraLiña := strings.IndexByte(t, '\n')
		if primeiraLiña != -1 {
			t = t[primeiraLiña+1:]
		}
		t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	}
	return strings.TrimSpace(t)
}
