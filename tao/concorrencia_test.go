package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// servidorConLatencia devolve un File Browser falso que tarda `lat` en cada resposta,
// para medir o custo das idas e voltas (non o dos datos). Serve un cartafol de materias
// con `n` definicións.
func servidorConLatencia(t testing.TB, n int, lat time.Duration) (*httptest.Server, *int32) {
	t.Helper()
	var maxSimultaneas, actuais int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		en := atomic.AddInt32(&actuais, 1)
		for {
			mx := atomic.LoadInt32(&maxSimultaneas)
			if en <= mx || atomic.CompareAndSwapInt32(&maxSimultaneas, mx, en) {
				break
			}
		}
		time.Sleep(lat)
		atomic.AddInt32(&actuais, -1)

		if r.URL.Path == "/api/resources" { // listado; a descarga vai a /api/resources/download
			files := make([]fbEntry, 0, n)
			for i := 0; i < n; i++ {
				files = append(files, fbEntry{Name: matNome(i)})
			}
			json.NewEncoder(w).Encode(map[string]any{"files": files, "folders": []fbEntry{}})
			return
		}
		json.NewEncoder(w).Encode(MateriaDef{ID: "m", Name: "Materia", Groups: []string{"eso3a"}})
	}))
	return srv, &maxSimultaneas
}

func matNome(i int) string { return string(rune('a'+i%26)) + "materia.json" }

// TestListMateriasRawEnParalelo comproba o efecto medible da concorrencia: con 8
// materias e 100 ms de latencia, en serie serían ~900 ms (1 listado + 8 descargas);
// con fbMaxParalelo=4 ten que baixar claramente, sen pasar nunca dese tope de peticións
// simultáneas.
func TestListMateriasRawEnParalelo(t *testing.T) {
	const n, lat = 8, 100 * time.Millisecond
	srv, maxSim := servidorConLatencia(t, n, lat)
	defer srv.Close()

	a := NewApp()
	a.fbURL, a.fbToken = srv.URL, "tok"

	inicio := time.Now()
	mats, err := a.listMateriasRaw()
	tardou := time.Since(inicio)
	if err != nil {
		t.Fatalf("listMateriasRaw: %v", err)
	}
	if len(mats) != n {
		t.Fatalf("materias = %d, quere %d", len(mats), n)
	}
	enSerie := time.Duration(n+1) * lat // 900 ms
	if tardou >= enSerie {
		t.Errorf("tardou %v: non hai paralelismo (en serie serían %v)", tardou, enSerie)
	}
	if got := atomic.LoadInt32(maxSim); got > fbMaxParalelo {
		t.Errorf("%d peticións simultáneas, o tope é %d: sen límite afógase o servidor", got, fbMaxParalelo)
	}
	t.Logf("8 materias con %v de latencia: %v (en serie serían %v, tope de %d simultáneas)", lat, tardou.Round(time.Millisecond), enSerie, atomic.LoadInt32(maxSim))
}

// BenchmarkListMateriasRaw mide o mesmo camiño con 12 materias e 20 ms de latencia.
func BenchmarkListMateriasRaw(b *testing.B) {
	srv, _ := servidorConLatencia(b, 12, 20*time.Millisecond)
	defer srv.Close()
	a := NewApp()
	a.fbURL, a.fbToken = srv.URL, "tok"
	for b.Loop() {
		if _, err := a.listMateriasRaw(); err != nil {
			b.Fatal(err)
		}
	}
}
