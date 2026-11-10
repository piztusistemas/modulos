//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestExeDentroDoEcosistemaAPI(t *testing.T) {
	base := "/opt/piztu"
	casos := []struct {
		nome  string
		exe   string
		quere bool
	}{
		{"piztu exacto", "/opt/piztu/piztu", true},
		{"módulo baixo modulos/", "/opt/piztu/modulos/pancho/pancho", true},
		{"módulo nun subcartafol", "/opt/piztu/modulos/yang/bin/yang", true},
		{"binario fóra da instalación", "/usr/bin/curl", false},
		{"nome parecido pero fóra (prefixo, non subcartafol)", "/opt/piztu-outra-cousa/algo", false},
		{"o propio directorio modulos, non un ficheiro dentro", "/opt/piztu/modulos", false},
		{"piztu nun subcartafol (non é exactamente el)", "/opt/piztu/algo/piztu", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := exeDentroDoEcosistemaAPI(c.exe, base, ""); got != c.quere {
				t.Fatalf("exeDentroDoEcosistemaAPI(%q, %q, \"\") = %v, quere %v", c.exe, base, got, c.quere)
			}
		})
	}
}

func TestExeDentroDoEcosistemaAPITaoModulosDir(t *testing.T) {
	taoModulos := "/opt/piztu/modulos/tao/modulos"
	casos := []struct {
		nome  string
		exe   string
		quere bool
	}{
		{"módulo de Tao baixo taoModulosDir", taoModulos + "/exemplo/exemplo", true},
		{"o propio directorio, non un ficheiro dentro", taoModulos, false},
		{"binario fóra de taoModulosDir", "/usr/bin/curl", false},
		{"nome parecido pero fóra (prefixo, non subcartafol)", taoModulos + "-outra-cousa/algo", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			// baseDir="" (sen instalación de Piztu coñecida): só taoModulosDir importa.
			if got := exeDentroDoEcosistemaAPI(c.exe, "", taoModulos); got != c.quere {
				t.Fatalf("exeDentroDoEcosistemaAPI(%q, \"\", %q) = %v, quere %v", c.exe, taoModulos, got, c.quere)
			}
		})
	}
}

// realUnixConnPair opens a genuine Unix domain socket pair (not net.Pipe,
// which is in-memory and has no real peer credentials to query) so
// SO_PEERCRED has something real to answer about — the peer on both ends is
// this very test process.
func realUnixConnPair(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "proba.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: ruta, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	acceptado := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		c, err := l.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		acceptado <- c
	}()

	cli, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: ruta, Net: "unix"})
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	select {
	case c := <-acceptado:
		t.Cleanup(func() { c.Close() })
		return c, cli
	case err := <-acceptErr:
		t.Fatalf("AcceptUnix: %v", err)
		return nil, nil
	}
}

func TestOrixeRecoñecidaAPIMesmoUsuarioSenBaseDir(t *testing.T) {
	server, _ := realUnixConnPair(t)
	// baseDir == "" e taoModulosDir == "": só se esixe o mesmo usuario, que
	// aquí sempre se cumpre (o propio proceso de test conecta consigo mesmo).
	if !orixeRecoñecidaAPI(server, "", "") {
		t.Fatal("debía aceptarse: mesmo usuario, sen baseDir nin taoModulosDir que comprobar")
	}
}

func TestOrixeRecoñecidaAPIRexeitaBinarioFoiDaInstalacion(t *testing.T) {
	server, _ := realUnixConnPair(t)
	// O binario real deste test (o propio `go test`) nunca vive baixo esta
	// instalación de mentira, así que a comprobación de ruta debe fallar
	// aínda que o usuario coincida.
	falsoBaseDir := filepath.Join(os.TempDir(), "instalacion-de-mentira-inexistente")
	if orixeRecoñecidaAPI(server, falsoBaseDir, "") {
		t.Fatal("debía rexeitarse: o binario de test non vive dentro dese baseDir")
	}
}

func TestOrixeRecoñecidaAPIRexeitaBinarioFoiDeTaoModulosDir(t *testing.T) {
	server, _ := realUnixConnPair(t)
	falsoTaoModulosDir := filepath.Join(os.TempDir(), "tao-modulos-de-mentira-inexistente")
	if orixeRecoñecidaAPI(server, "", falsoTaoModulosDir) {
		t.Fatal("debía rexeitarse: o binario de test non vive dentro dese taoModulosDir")
	}
}
