package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestAPIEscoitarEPecharSocketReal is the only end-to-end test: it exercises
// escoitar/mux/pechar together over a real Unix socket. The rest of the API
// tests go straight against mux() with httptest for speed.
func TestAPIEscoitarEPecharSocketReal(t *testing.T) {
	a := NewApp()
	a.dataDir = t.TempDir()
	s := novoAPIServidor(a)
	if err := s.escoitar(); err != nil {
		t.Fatalf("escoitar: %v", err)
	}

	info, err := os.Stat(s.rutaSocket())
	if err != nil {
		t.Fatalf("stat do socket: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permisos do socket = %o, quere 0600", perm)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.rutaSocket())
			},
		},
		Timeout: 5 * time.Second,
	}
	req, _ := http.NewRequest(http.MethodGet, "http://local/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("petición real ao socket: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quere 200", resp.StatusCode)
	}

	s.pechar()
	if _, err := os.Stat(s.rutaSocket()); !os.IsNotExist(err) {
		t.Fatalf("o socket debía desaparecer tras pechar, err = %v", err)
	}
	if _, err := client.Do(req); err == nil {
		t.Fatal("unha nova petición tras pechar debía fallar")
	}
}
