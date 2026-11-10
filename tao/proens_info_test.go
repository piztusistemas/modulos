package main

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// The tests run against a real Proens "vistaPreliminar" page (materia de
// Tecnoloxía e Dixitalización, 3º ESO), saved under testdata/. Same rationale
// as calendario_test.go: the real page's markup quirks (the "Bloque"/"Contidos"
// table pairing, the empty "molde" rows in 3.3...) are what the parsers have to
// survive, and a hand-written sample wouldn't reproduce them.
func realProens(t *testing.T) *goquery.Document {
	t.Helper()
	raw, err := os.ReadFile("testdata/proens_vistaPreliminar.html")
	if err != nil {
		t.Fatalf("non se puido ler o testdata: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("HTML non válido: %v", err)
	}
	return doc
}

func TestParseProensUnitsReadsTrimestre(t *testing.T) {
	units := parseProensUnits(realProens(t))
	if len(units) != 9 {
		t.Fatalf("len(units) = %d, quere 9", len(units))
	}

	casos := map[string]int{
		"A tecnoloxía":               1, // primeiras 4 UDs no 1º trimestre
		"Deseño e fabricación":       1,
		"Documentación de proxectos": 2, // migra ao 2º trimestre
		"Programación":               3, // últimas UDs no 3º trimestre
	}
	for _, u := range units {
		if quere, ok := casos[u.Titulo]; ok && u.Trimestre != quere {
			t.Errorf("UD %q: Trimestre = %d, quere %d", u.Titulo, u.Trimestre, quere)
		}
	}
}

func TestParseProensDistribucionUD(t *testing.T) {
	porUD := parseProensDistribucionUD(realProens(t))

	bloques := porUD["A tecnoloxía"]
	if len(bloques) == 0 {
		t.Fatal("UD 'A tecnoloxía' sen bloques de currículo")
	}
	b1 := bloques[0]
	if !strings.Contains(b1.Bloque, "Bloque B1") {
		t.Errorf("primeiro bloque de 'A tecnoloxía' = %q, quere que conteña 'Bloque B1'", b1.Bloque)
	}
	if len(b1.Criterios) == 0 {
		t.Error("Bloque B1 de 'A tecnoloxía' sen criterios marcados")
	}
	for _, c := range b1.Criterios {
		if c.Codigo == "" || c.Descricion == "" {
			t.Errorf("criterio mal partido: %+v", c)
		}
	}
	// CA1.1 non está marcado para "A tecnoloxía" no HTML de orixe (só para UD3/4/6/7).
	for _, c := range b1.Criterios {
		if c.Codigo == "CA1.1" {
			t.Error("CA1.1 non debería estar marcado para 'A tecnoloxía'")
		}
	}
}

func TestParseProensAvaliacionUD(t *testing.T) {
	porUD := parseProensAvaliacionUD(realProens(t))

	instrumentos := porUD["A tecnoloxía"]
	if len(instrumentos) != 3 {
		t.Fatalf("len(instrumentos['A tecnoloxía']) = %d, quere 3 (as filas 'molde' baleiras deberían ignorarse)", len(instrumentos))
	}
	primeiro := instrumentos[0]
	if primeiro.Criterio.Codigo != "CA1.2" {
		t.Errorf("primeiro instrumento: Criterio.Codigo = %q, quere CA1.2", primeiro.Criterio.Codigo)
	}
	if primeiro.TipoInstrumento != "Táboa de indicadores" {
		t.Errorf("TipoInstrumento = %q, quere 'Táboa de indicadores'", primeiro.TipoInstrumento)
	}
	if primeiro.Peso != "25" {
		t.Errorf("Peso = %q, quere '25'", primeiro.Peso)
	}
}

// TestParseProensUnidadesInfoNonDevolveSlicesNil comproba que ningún dos
// slices aniñados viaxa como Go nil (JSON `null`): en Go, `len(nil)` é 0
// válido, así que un test que só mire lonxitudes non pillaría isto — o bug
// real (o panel petaba en produción) só se manifesta en JavaScript, onde
// `null.length` lanza. Comprobar explicitamente != nil é o que o detecta.
func TestParseProensUnidadesInfoNonDevolveSlicesNil(t *testing.T) {
	unidades := parseProensUnidadesInfo(realProens(t))
	for _, u := range unidades {
		if u.Bloques == nil {
			t.Errorf("UD %q: Bloques é nil (serializaría como JSON null)", u.Titulo)
		}
		if u.Avaliacion == nil {
			t.Errorf("UD %q: Avaliacion é nil (serializaría como JSON null)", u.Titulo)
		}
		for _, b := range u.Bloques {
			if b.Criterios == nil {
				t.Errorf("UD %q, bloque %q: Criterios é nil", u.Titulo, b.Bloque)
			}
			if b.Contidos == nil {
				t.Errorf("UD %q, bloque %q: Contidos é nil", u.Titulo, b.Bloque)
			}
		}
	}
}

func TestParseProensUnidadesInfoCombinaAsTresSeccions(t *testing.T) {
	unidades := parseProensUnidadesInfo(realProens(t))
	if len(unidades) != 9 {
		t.Fatalf("len(unidades) = %d, quere 9", len(unidades))
	}
	ud1 := unidades[0]
	if ud1.Titulo != "A tecnoloxía" {
		t.Fatalf("unidades[0].Titulo = %q, quere 'A tecnoloxía'", ud1.Titulo)
	}
	if ud1.Trimestre != 1 {
		t.Errorf("ud1.Trimestre = %d, quere 1", ud1.Trimestre)
	}
	if len(ud1.Bloques) == 0 {
		t.Error("ud1.Bloques baleiro, o cruzamento por título fallou")
	}
	if len(ud1.Avaliacion) != 3 {
		t.Errorf("len(ud1.Avaliacion) = %d, quere 3", len(ud1.Avaliacion))
	}
}
