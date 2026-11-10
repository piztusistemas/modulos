package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings persiste a configuración de Malta entre execucións — de momento
// só a IA (calquera provedor que fale un dos tres formatos de fío
// soportados, ver ia_client.go, porta exacta de yang/settings.go +
// yang/ia_client.go, xa que é o patrón multi-provedor máis parecido que
// existe no ecosistema — non hai ningunha clave de IA centralizada en
// Piztu que copiar, ver DESIGN/decisión do módulo).
type Settings struct {
	IAProvedor string `json:"iaProvedor"` // "gemini" | "anthropic" | "openai"
	IAAPIKey   string `json:"iaApiKey"`
	IAModel    string `json:"iaModel"`
	IABaseURL  string `json:"iaBaseUrl"`
	IAPreset   string `json:"iaPreset"` // só o valor do selector do frontend, ver yang/settings.go
	// ImportadaDeYang marca que IAAPIKey veu do auto-import da configuración
	// de Yang (ver importarDeYangSeVacio) e non dun formulario propio — o
	// frontend úsao para amosar "importada de Yang" en vez do formulario de
	// configuración, sen que iso impida editala despois.
	ImportadaDeYang bool `json:"importadaDeYang,omitempty"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "malta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func loadSettings() Settings {
	var s Settings
	path, err := settingsPath()
	if err == nil {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	if s.IAAPIKey == "" {
		importarDeYangSeVacio(&s)
	}
	return s
}

// importarDeYangSeVacio le ~/.config/yang/settings.json (se existe e ten
// IAAPIKey) e cópiaa a s — só a primeira vez que Malta arrinca sen clave
// propia (loadSettings xa comproba IAAPIKey==""). Non escribe nada no
// ficheiro de Yang, e o valor cópiase (non se relé cada vez): a partir de
// aquí Malta é independente, editable dende o seu propio formulario aínda
// que Yang cambie ou se desinstale despois. Best-effort: calquera fallo
// (sen Yang instalado, sen clave configurada aí, ficheiro non lexible)
// dá simplemente s sen tocar, e Malta amosa o seu propio formulario.
func importarDeYangSeVacio(s *Settings) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, "yang", "settings.json"))
	if err != nil {
		return
	}
	var deYang struct {
		IAProvedor string `json:"iaProvedor"`
		IAAPIKey   string `json:"iaApiKey"`
		IAModel    string `json:"iaModel"`
		IABaseURL  string `json:"iaBaseUrl"`
		IAPreset   string `json:"iaPreset"`
	}
	if json.Unmarshal(b, &deYang) != nil || deYang.IAAPIKey == "" {
		return
	}
	s.IAProvedor = deYang.IAProvedor
	s.IAAPIKey = deYang.IAAPIKey
	s.IAModel = deYang.IAModel
	s.IABaseURL = deYang.IABaseURL
	s.IAPreset = deYang.IAPreset
	s.ImportadaDeYang = true
	_ = saveSettingsToDisk(*s) // así queda xa independente de Yang para a próxima vez
}

func saveSettingsToDisk(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// GetSettings/SaveSettings son a interface que usa o formulario de
// configuración do frontend.
func (a *App) GetSettings() Settings {
	return a.settings
}

func (a *App) SaveSettings(s Settings) error {
	s.ImportadaDeYang = false // edición explícita: xa non é "a de Yang sen tocar"
	a.settings = s
	return saveSettingsToDisk(s)
}
