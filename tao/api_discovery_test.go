package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestEscribirDescubrimentoAPICreaFicheiroConPermisos0600(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	if err := a.escribirDescubrimentoAPI("/tmp/algo.sock", "tao_abc123"); err != nil {
		t.Fatalf("escribirDescubrimentoAPI: %v", err)
	}

	info, err := os.Stat(a.apiDiscoveryPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permisos = %o, quere 0600", perm)
	}

	raw, err := os.ReadFile(a.apiDiscoveryPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var d descubrimentoAPI
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Version != VersionAPI || d.Socket != "/tmp/algo.sock" || d.Token != "tao_abc123" {
		t.Fatalf("contido = %+v", d)
	}
}

func TestBorrarDescubrimentoAPIElimina(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	if err := a.escribirDescubrimentoAPI("/tmp/algo.sock", "tao_abc123"); err != nil {
		t.Fatalf("escribirDescubrimentoAPI: %v", err)
	}
	a.borrarDescubrimentoAPI()
	if _, err := os.Stat(a.apiDiscoveryPath()); !os.IsNotExist(err) {
		t.Fatalf("o ficheiro debía desaparecer, err = %v", err)
	}
}
