package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// NomeFicheiroDescubrimentoAPI is where other Piztu modules on the same
// machine find Tao's socket and token — a deterministic path
// (os.UserConfigDir()/dixitalizacion/tao-api.json), no coordination with
// piztu needed.
const NomeFicheiroDescubrimentoAPI = "tao-api.json"

// descubrimentoAPI is the published contract a consumer module reads.
type descubrimentoAPI struct {
	Version string `json:"version"`
	Socket  string `json:"socket"`
	Token   string `json:"token"`
}

func (a *App) apiDiscoveryPath() string {
	return filepath.Join(a.dataDir, NomeFicheiroDescubrimentoAPI)
}

// escribirDescubrimentoAPI publishes socket+token for other modules. 0600,
// like profiles.json: the token grants professor-level API access to
// whoever can read it (same user/machine boundary).
func (a *App) escribirDescubrimentoAPI(socket, token string) error {
	raw, err := json.MarshalIndent(descubrimentoAPI{Version: VersionAPI, Socket: socket, Token: token}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.apiDiscoveryPath(), raw, 0o600)
}

// borrarDescubrimentoAPI removes it in shutdown() (best-effort).
func (a *App) borrarDescubrimentoAPI() {
	_ = os.Remove(a.apiDiscoveryPath())
}
