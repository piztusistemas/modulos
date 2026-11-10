package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

// ProensUnit is a single didactic unit extracted from a Proens public
// "vistaPreliminar" page (the "Relación de unidades didácticas" table).
type ProensUnit struct {
	Secuencia  int    `json:"secuencia"`
	Titulo     string `json:"titulo"`
	Descricion string `json:"descricion"`
	Peso       string `json:"peso"`
	Sesions    int    `json:"sesions"`
	// Trimestre é 1, 2 ou 3 segundo cal das tres últimas columnas da táboa
	// ("1º trim."/"2º trim."/"3º trim.") ten o checkbox marcado; 0 se ningunha
	// o está (non debería pasar en programacións completas, pero non se asume).
	Trimestre int `json:"trimestre"`
}

// ProensResult is the structured payload returned to the frontend.
type ProensResult struct {
	Units []ProensUnit `json:"units"`
}

// proensPrazo is the overall time budget for loading the page and waiting for
// its JS-rendered table to appear.
const proensPrazo = 45 * time.Second

// ImportProens loads a Proens public preview URL in a headless Chromium browser
// (see browser.go — the system's Chromium via CDP, no Playwright involved) and
// extracts the list of didactic units from the resulting HTML.
func (a *App) ImportProens(rawURL string) (*ProensResult, error) {
	doc, err := cargarPaxinaProens(rawURL, func(doc *goquery.Document) bool {
		return len(parseProensUnits(doc)) > 0
	}, "non se atopou a táboa de unidades didácticas. "+
		"Asegúrate de usar a URL pública de vista preliminar (vistaPreliminar.action)")
	if err != nil {
		return nil, err
	}
	return &ProensResult{Units: parseProensUnits(doc)}, nil
}

// cargarPaxinaProens normaliza `rawURL` (prefixo https, corrección do erro común
// /proens/prv/AccesoPublico → /proens/AccesoPublico), navega nun Chromium en
// fondo (novoContextoChromium) e sondea o HTML resultante até que `listo`
// devolva true — a páxina complétase de forma asíncrona (JS), así que non hai
// un "network idle" xenérico do que fiarse. Compartida por ImportProens e
// ImportProensInfo (proens_info.go): as dúas len a mesma páxina, só que
// extraen seccións distintas dela.
func cargarPaxinaProens(rawURL string, listo func(*goquery.Document) bool, erroTimeout string) (*goquery.Document, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("a URL de Proens está baleira")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	// The private path (/proens/prv/...) requires CAS login; the public preview
	// lives at /proens/AccesoPublico... — correct the common copy/paste mistake.
	rawURL = strings.Replace(rawURL, "/proens/prv/AccesoPublico", "/proens/AccesoPublico", 1)

	// +5s de marxe sobre proensPrazo: así o propio sondeo é quen de reportar
	// o erro de "listo" antes de que o contexto remate abruptamente.
	ctx, cancel, err := novoContextoChromium(proensPrazo + 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(rawURL)); err != nil {
		return nil, fmt.Errorf("non se puido cargar a páxina de Proens: %w", err)
	}

	prazo := time.Now().Add(proensPrazo)
	for {
		var urlActual, contido string
		if err := chromedp.Run(ctx,
			chromedp.Location(&urlActual),
			chromedp.OuterHTML("html", &contido),
		); err != nil {
			return nil, fmt.Errorf("non se puido ler a páxina de Proens: %w", err)
		}

		// If the request was redirected to the CAS login, the URL is not public.
		if strings.Contains(urlActual, "/cas/login") {
			return nil, fmt.Errorf("a páxina pediu identificación (CAS). " +
				"Asegúrate de usar a URL pública de vista preliminar (/proens/AccesoPublico...)")
		}

		if doc, err := goquery.NewDocumentFromReader(strings.NewReader(contido)); err == nil {
			if listo(doc) {
				return doc, nil
			}
		}

		if time.Now().After(prazo) {
			return nil, errors.New(erroTimeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// parseProensUnits locates the "Relación de unidades didácticas" table by its
// header columns and extracts one ProensUnit per body row.
func parseProensUnits(doc *goquery.Document) []ProensUnit {
	var units []ProensUnit

	doc.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		header := normalize(table.Find("thead").Text())
		// The units table is the one whose header has both the UD title column
		// and the sessions column.
		if !strings.Contains(header, "titulo da ud") || !strings.Contains(header, "n de sesions") {
			return true // keep looking
		}

		table.Find("tbody > tr").Each(func(_ int, row *goquery.Selection) {
			cells := row.Find("td")
			if cells.Length() < 5 {
				return
			}
			seq, _ := strconv.Atoi(cellText(cells.Eq(0)))
			titulo := cellText(cells.Eq(1))
			descr := cellText(cells.Eq(2))
			peso := cellText(cells.Eq(3))
			sesions, _ := strconv.Atoi(cellText(cells.Eq(4)))

			if titulo == "" {
				return
			}
			// As 3 últimas columnas (1º/2º/3º trim.) só existen en programacións
			// completas; en táboas máis vellas/incompletas pode haber só 5 celas,
			// así que isto compróbase á parte sen esixilo no filtro de arriba.
			trimestre := 0
			for i, td := range []int{5, 6, 7} {
				if td >= cells.Length() {
					break
				}
				if _, marcado := cells.Eq(td).Find("input").First().Attr("checked"); marcado {
					trimestre = i + 1
					break
				}
			}
			units = append(units, ProensUnit{
				Secuencia:  seq,
				Titulo:     titulo,
				Descricion: descr,
				Peso:       peso,
				Sesions:    sesions,
				Trimestre:  trimestre,
			})
		})
		return false // found the table, stop iterating
	})

	return units
}

// cellText collapses internal whitespace of a table cell into single spaces.
func cellText(s *goquery.Selection) string {
	return strings.Join(strings.Fields(s.Text()), " ")
}

// normalize lowercases and strips accents so header matching is robust.
func normalize(s string) string {
	s = strings.ToLower(s)
	repl := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n", "º", "", "°", "",
	)
	s = repl.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
