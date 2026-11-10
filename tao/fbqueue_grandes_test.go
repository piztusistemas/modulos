package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFBQueueContidoGrandeVaiAparte: un traballo grande que se encola NON pode acabar
// dentro do JSON da cola. Ese JSON reescríbese enteiro en cada cambio e co mutex
// collido, así que un ficheiro de decenas de MB alí dentro (en base64) deixaba a app
// parada uns segundos xusto despois de subir — o "cólgase ao subir" que se reportou.
func TestFBQueueContidoGrandeVaiAparte(t *testing.T) {
	// Servidor que sempre falla con 503: a subida queda encolada.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	dir := t.TempDir()
	a := &App{dataDir: dir, fbURL: server.URL, fbToken: "tok", fbUser: "est"}
	a.fbQueueInit()

	grande := make([]byte, fbQueueInlineMax+1024)
	for i := range grande {
		grande[i] = byte('a' + i%26)
	}
	if err := a.fbUpload("users/est/traballos/m1/2026-09-17/memoria.odt", grande); err != nil {
		t.Fatalf("fbUpload: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, fbQueueFile))
	if err != nil {
		t.Fatalf("a cola debería estar en disco: %v", err)
	}
	if len(raw) > 4096 {
		t.Errorf("o JSON da cola ocupa %d bytes: o contido grande non se sacou fóra", len(raw))
	}
	if !strings.Contains(string(raw), "dataFile") {
		t.Errorf("esperábase unha referencia a dataFile na cola, obtívose: %s", raw)
	}

	entradas, err := os.ReadDir(filepath.Join(dir, fbQueueDataDir))
	if err != nil || len(entradas) != 1 {
		t.Fatalf("esperábase 1 ficheiro de contido en %s, obtívose %v (err=%v)", fbQueueDataDir, entradas, err)
	}
	gardado, _ := os.ReadFile(filepath.Join(dir, fbQueueDataDir, entradas[0].Name()))
	if len(gardado) != len(grande) {
		t.Errorf("o contido gardado ten %d bytes, esperábanse %d", len(gardado), len(grande))
	}
}

// TestFBQueueNonReenviaOXaSubido: o caso real que motivou o arranxo. A subida esgota
// o tempo mentres se agarda a resposta, PERO o ficheiro xa chegou enteiro ao
// servidor. Ao reintentar, a cola ten que darse conta de que xa está alí e
// descartar a operación, en vez de mover os mesmos megas outra vez.
func TestFBQueueNonReenviaOXaSubido(t *testing.T) {
	fb := newFakeFileBrowser("tok")
	fb.dirs["users/est/traballos/m1/2026-09-17"] = true
	contido := []byte(strings.Repeat("x", 2048))
	ruta := "users/est/traballos/m1/2026-09-17/memoria.odt"
	fb.files[ruta] = contido // xa chegara, aínda que o cliente non se enterase

	var subidas int32
	inner := fb.handler("est", "segredo")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/resources" && r.URL.Query().Get("isDir") != "true" {
			atomic.AddInt32(&subidas, 1)
			w.WriteHeader(http.StatusServiceUnavailable) // o primeiro intento tamén "falla"
			return
		}
		inner(w, r)
	}))
	defer server.Close()

	a := &App{dataDir: t.TempDir(), fbURL: server.URL, fbToken: fb.token, fbUser: "est"}
	a.fbQueueInit()
	if err := a.fbUpload(ruta, contido); err != nil {
		t.Fatalf("fbUpload: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && a.FBQueueStatus().Pendentes > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if st := a.FBQueueStatus(); st.Pendentes != 0 {
		t.Fatalf("a cola debería baleirarse ao ver o ficheiro xa no servidor, quedan %d", st.Pendentes)
	}
	// Só o intento síncrono inicial: o reintento da cola non chegou a enviar nada.
	if n := atomic.LoadInt32(&subidas); n != 1 {
		t.Errorf("enviáronse %d subidas ao servidor, esperábase 1 (o reintento debía evitarse)", n)
	}
}

// TestTempoTransferenciaEscalaCoTamano: un ficheiro grande ten que ter máis marxe que
// un pequeno, e nunca un tope tan curto coma o dos 30 s das chamadas normais.
func TestTempoTransferenciaEscalaCoTamano(t *testing.T) {
	pequeno := tempoTransferencia(1024)
	grande := tempoTransferencia(maxWorkFileBytes)
	if pequeno < 60*time.Second {
		t.Errorf("tempoTransferencia(1 KiB) = %v, curto de máis", pequeno)
	}
	if grande <= pequeno {
		t.Errorf("tempoTransferencia non medra co tamaño: %v vs %v", grande, pequeno)
	}
	if grande > 20*time.Minute {
		t.Errorf("tempoTransferencia(50 MiB) = %v, pasa do tope de 20 min", grande)
	}
}
