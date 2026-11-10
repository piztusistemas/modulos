package main

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ProensObxectivo is one teaching objective ("OBX1"...) with the key
// competences it contributes to, derived from which competence columns have
// at least one checked "descritor" checkbox in the Proens table — the
// individual descriptors themselves are too fine-grained to surface here.
type ProensObxectivo struct {
	Codigo       string   `json:"codigo"`
	Texto        string   `json:"texto"`
	Competencias []string `json:"competencias"`
}

// ProensPesoCualificacion is one qualification instrument ("Proba escrita"...)
// with its overall weight in the mark (the "Total programación" column) — the
// per-unit breakdown is too fine-grained to surface here.
type ProensPesoCualificacion struct {
	Instrumento string `json:"instrumento"`
	Total       string `json:"total"`
}

// ProensCriterio is a criterion of evaluation or a content item, as they
// appear in the currículo/avaliación tables (3.2/3.3): a short code
// ("CA1.1", "C1.1"...) plus its free-text description, split apart from the
// single "CODIGO - Texto" (3.2) or "CODIGO\nTexto" (3.3) span Proens renders.
type ProensCriterio struct {
	Codigo     string `json:"codigo"`
	Descricion string `json:"descricion"`
}

// ProensBloqueUD is one currículo block ("Bloque B1. ...") restricted to a
// single UD: only the criterios de avaliación and contidos whose checkbox is
// marked for that UD's column in the 3.2 tables.
type ProensBloqueUD struct {
	Bloque    string           `json:"bloque"`
	Criterios []ProensCriterio `json:"criterios"`
	Contidos  []ProensCriterio `json:"contidos"`
}

// ProensInstrumentoUD is one row of a UD's avaliación table (3.3): a
// criterion together with how it's assessed within that UD.
type ProensInstrumentoUD struct {
	Criterio        ProensCriterio `json:"criterio"`
	TipoInstrumento string         `json:"tipoInstrumento"`
	GraoMinimo      string         `json:"graoMinimo"`
	Peso            string         `json:"peso"`
}

// ProensUnidadeInfo is the detailed, per-UD breakdown shown when the
// professor focuses a single "UD N" chip: its trimestre (from 3.1), its
// currículo blocks (3.2) and its avaliación instruments (3.3).
type ProensUnidadeInfo struct {
	Secuencia  int                   `json:"secuencia"`
	Titulo     string                `json:"titulo"`
	Trimestre  int                   `json:"trimestre"`
	Bloques    []ProensBloqueUD      `json:"bloques"`
	Avaliacion []ProensInstrumentoUD `json:"avaliacion"`
}

// ProensInfo is the general information of the "programación" (as opposed to
// ProensResult, which is just the unit list): introdución, obxectivos with
// their competence mapping, criterios de cualificación, and — per UD — the
// currículo distribution and avaliación instruments (Unidades).
type ProensInfo struct {
	Introducion    []string                  `json:"introducion"`
	Obxectivos     []ProensObxectivo         `json:"obxectivos"`
	CriteriosTexto []string                  `json:"criteriosTexto"`
	CriteriosPesos []ProensPesoCualificacion `json:"criteriosPesos"`
	Unidades       []ProensUnidadeInfo       `json:"unidades"`
}

// ImportProensInfo loads the same Proens public preview page as ImportProens
// (see cargarPaxinaProens in proens.go) and extracts the general programación
// sections — "1. Introdución", "2. Obxectivos e súa contribución ao
// desenvolvemento das competencias" and "5.2. Criterios de cualificación e
// recuperación" — plus, per UD, the currículo distribution ("3.2") and
// avaliación instruments ("3.3"), used to focus the panel on a single UD.
func (a *App) ImportProensInfo(rawURL string) (*ProensInfo, error) {
	doc, err := cargarPaxinaProens(rawURL, func(doc *goquery.Document) bool {
		return atopaSeccionProens(doc, "introducion") != nil &&
			atopaSeccionProens(doc, "criterios de cualificacion") != nil &&
			atopaSeccionProens(doc, "distribucion", "curriculo") != nil &&
			atopaSeccionProens(doc, "avaliacion das unidades") != nil
	}, "non se atopou a información xeral da programación. "+
		"Asegúrate de usar a URL pública de vista preliminar (vistaPreliminar.action)")
	if err != nil {
		return nil, err
	}

	info := &ProensInfo{}
	if sec := atopaSeccionProens(doc, "introducion"); sec != nil {
		info.Introducion = paragrafosProens(sec)
	}
	if sec := atopaSeccionProens(doc, "obxectivos e", "competenc"); sec != nil {
		info.Obxectivos = parseProensObxectivos(sec)
	}
	if sec := atopaSeccionProens(doc, "criterios de cualificacion"); sec != nil {
		info.CriteriosTexto = paragrafosProens(sec)
		info.CriteriosPesos = parseProensCriteriosPesos(sec)
	}
	info.Unidades = parseProensUnidadesInfo(doc)
	return info, nil
}

// parseProensUnidadesInfo cross-references the base unit list (3.1, via
// parseProensUnits — título/secuencia/trimestre) with the per-UD currículo
// blocks (3.2, parseProensDistribucionUD) and avaliación instruments (3.3,
// parseProensAvaliacionUD), both keyed by título. Falls back to positional
// matching (same order as 3.1) if a título doesn't cross-reference exactly —
// same fallback criterion App.tsx's handleSave already uses for
// pendingProgrammingData.
func parseProensUnidadesInfo(doc *goquery.Document) []ProensUnidadeInfo {
	base := parseProensUnits(doc)
	if len(base) == 0 {
		return nil
	}
	porBloques := parseProensDistribucionUD(doc)
	porAvaliacion := parseProensAvaliacionUD(doc)

	unidades := make([]ProensUnidadeInfo, 0, len(base))
	for _, u := range base {
		bloques, ok := porBloques[u.Titulo]
		if !ok {
			for titulo, b := range porBloques {
				if normalize(titulo) == normalize(u.Titulo) {
					bloques = b
					break
				}
			}
		}
		avaliacion, ok := porAvaliacion[u.Titulo]
		if !ok {
			for titulo, a := range porAvaliacion {
				if normalize(titulo) == normalize(u.Titulo) {
					avaliacion = a
					break
				}
			}
		}
		unidades = append(unidades, ProensUnidadeInfo{
			Secuencia:  u.Secuencia,
			Titulo:     u.Titulo,
			Trimestre:  u.Trimestre,
			Bloques:    normalizarBloques(bloques),
			Avaliacion: normalizarInstrumentos(avaliacion),
		})
	}
	return unidades
}

// normalizarBloques e normalizarInstrumentos garanten que estes slices — e os
// Criterios/Contidos aniñados de cada bloque — nunca viaxen como `null` en
// JSON: un `var x []T` en Go que non chega a recibir ningún append segue nil,
// e `len(nil)` é 0 válido en Go (por iso os tests non pillaban isto), pero
// `null.length` en JavaScript lanza — o panel petaba xusto por esta diferenza.
func normalizarBloques(bloques []ProensBloqueUD) []ProensBloqueUD {
	if bloques == nil {
		bloques = []ProensBloqueUD{}
	}
	for i := range bloques {
		if bloques[i].Criterios == nil {
			bloques[i].Criterios = []ProensCriterio{}
		}
		if bloques[i].Contidos == nil {
			bloques[i].Contidos = []ProensCriterio{}
		}
	}
	return bloques
}

func normalizarInstrumentos(avaliacion []ProensInstrumentoUD) []ProensInstrumentoUD {
	if avaliacion == nil {
		return []ProensInstrumentoUD{}
	}
	return avaliacion
}

// atopaSeccionProens locates a Proens "divTituloPaxina" section whose <h1>
// title (normalized) contains every string in `anacos`, and returns its
// content: every sibling node up to the next section header. Proens lays out
// each section as a title div followed by content divs as SIBLINGS, not as
// children, so this sibling-walk (goquery's NextUntil) is how content gets
// scoped to a title. Returns nil if no matching section exists yet (the page
// fills in asynchronously — see cargarPaxinaProens's polling).
func atopaSeccionProens(doc *goquery.Document, anacos ...string) *goquery.Selection {
	var contido *goquery.Selection
	doc.Find("div.divTituloPaxina").EachWithBreak(func(_ int, sec *goquery.Selection) bool {
		titulo := normalize(sec.Find("h1").First().Text())
		for _, anaco := range anacos {
			if !strings.Contains(titulo, anaco) {
				return true // keep looking
			}
		}
		contido = sec.NextUntil("div.divTituloPaxina")
		return false
	})
	return contido
}

// paragrafosProens extracts the <p> text of the free-text block
// (<span class="texto">) inside a section's content, one string per
// non-empty paragraph.
func paragrafosProens(contido *goquery.Selection) []string {
	var paragrafos []string
	contido.Find("span.texto p").Each(func(_ int, p *goquery.Selection) {
		texto := strings.ReplaceAll(p.Text(), " ", " ") // &nbsp;
		texto = strings.Join(strings.Fields(texto), " ")
		if texto != "" {
			paragrafos = append(paragrafos, texto)
		}
	})
	return paragrafos
}

// parseProensObxectivos reads the "2. Obxectivos..." table: one row per
// objective (OBX1, OBX2...), with its text in the first cell and one cell per
// key competence (CCL, CP, STEM, CD, CPSAA, CC, CE, CCEC, in the order the
// header declares them). A competence counts as contributed to when at least
// one of its "descritor" checkboxes is checked in that row.
func parseProensObxectivos(contido *goquery.Selection) []ProensObxectivo {
	var obxectivos []ProensObxectivo

	contido.Find("table.estiloListado").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		var competencias []string
		table.Find("thead th.competencia span").Each(func(_ int, s *goquery.Selection) {
			if nome := strings.TrimSpace(s.Text()); nome != "" {
				competencias = append(competencias, nome)
			}
		})
		if len(competencias) == 0 {
			return true // not the objectives table, keep looking
		}

		table.Find("tbody > tr").Each(func(_ int, row *goquery.Selection) {
			cells := row.Find("td")
			if cells.Length() < len(competencias)+1 {
				return
			}

			primeira := cells.Eq(0)
			codigo := strings.TrimSpace(primeira.Find("span[style*='font-weight']").First().Text())
			texto := strings.Join(strings.Fields(primeira.Text()), " ")
			texto = strings.TrimSpace(strings.TrimPrefix(texto, codigo))
			if texto == "" {
				return
			}

			var propias []string
			for i, competencia := range competencias {
				if cells.Eq(i+1).Find("input[checked]").Length() > 0 {
					propias = append(propias, competencia)
				}
			}

			obxectivos = append(obxectivos, ProensObxectivo{
				Codigo:       codigo,
				Texto:        texto,
				Competencias: propias,
			})
		})
		return false
	})

	return obxectivos
}

// parseProensCriteriosPesos reads the "Pesos na materia" table inside the
// "5.2. Criterios de cualificación e recuperación" section: one row per
// qualification instrument, keeping only its name and its overall weight
// (the last column, "Total programación").
func parseProensCriteriosPesos(contido *goquery.Selection) []ProensPesoCualificacion {
	var pesos []ProensPesoCualificacion

	contido.Find("fieldset table.estiloListado").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		header := normalize(table.Find("thead").Text())
		if !strings.Contains(header, "instrumentos de avaliacion") {
			return true // not the weights table, keep looking
		}

		table.Find("tbody > tr").Each(func(_ int, row *goquery.Selection) {
			cells := row.Find("td")
			if cells.Length() < 2 {
				return
			}
			instrumento := cellText(cells.First())
			total := cellText(cells.Last())
			if instrumento == "" {
				return
			}
			pesos = append(pesos, ProensPesoCualificacion{Instrumento: instrumento, Total: total})
		})
		return false
	})

	return pesos
}

// parseProensDistribucionUD reads the "3.2. Distribución currículo nas
// unidades didácticas" section: a repeating pair of tables per bloque —
// criterios de avaliación (its <thead> has a th.cabeceiraCriterios span
// naming the bloque, e.g. "Bloque B1. O proxecto tecnolóxico") immediately
// followed by contidos (same UD columns, but a plain "Contidos" header, no
// th.cabeceiraCriterios) — each row a criterion/content with one
// checkbox column per UD. Returns, per UD título, only the bloque entries
// (criterios/contidos) whose checkbox is marked for that UD.
func parseProensDistribucionUD(doc *goquery.Document) map[string][]ProensBloqueUD {
	sec := atopaSeccionProens(doc, "distribucion", "curriculo")
	if sec == nil {
		return nil
	}

	porUD := map[string][]ProensBloqueUD{}
	bloqueActual := ""

	sec.Find("table.estiloListado").Each(func(_ int, table *goquery.Selection) {
		var titulos []string
		table.Find("thead th.tdConChecboxDeUds span[title]").Each(func(_ int, s *goquery.Selection) {
			if titulo, existe := s.Attr("title"); existe {
				titulos = append(titulos, titulo)
			}
		})
		if len(titulos) == 0 {
			return // not a criterios/contidos table (e.g. the 3.1 units table)
		}

		// Só a táboa de criterios ten th.cabeceiraCriterios (co nome do
		// bloque); a de contidos que a segue reusa `bloqueActual`.
		ehContidos := true
		if nome := strings.TrimSpace(table.Find("thead th.cabeceiraCriterios span").First().Text()); nome != "" {
			bloqueActual = nome
			ehContidos = false
		}
		if bloqueActual == "" {
			return
		}

		table.Find("tbody > tr").Each(func(_ int, row *goquery.Selection) {
			cells := row.Find("td")
			if cells.Length() < len(titulos)+1 {
				return
			}
			texto := cellText(cells.Eq(0))
			if texto == "" {
				return
			}
			criterio := partirCriterioCurriculo(texto)

			for i, titulo := range titulos {
				if cells.Eq(i+1).Find("input[checked]").Length() == 0 {
					continue
				}
				bloques := porUD[titulo]
				idx := -1
				for j, b := range bloques {
					if b.Bloque == bloqueActual {
						idx = j
						break
					}
				}
				if idx == -1 {
					bloques = append(bloques, ProensBloqueUD{Bloque: bloqueActual})
					idx = len(bloques) - 1
				}
				if ehContidos {
					bloques[idx].Contidos = append(bloques[idx].Contidos, criterio)
				} else {
					bloques[idx].Criterios = append(bloques[idx].Criterios, criterio)
				}
				porUD[titulo] = bloques
			}
		})
	})

	return porUD
}

// partirCriterioCurriculo splits a "CODIGO - Descrición" span (3.2's format)
// into its código and its descrición.
func partirCriterioCurriculo(texto string) ProensCriterio {
	if partes := strings.SplitN(texto, " - ", 2); len(partes) == 2 {
		return ProensCriterio{Codigo: strings.TrimSpace(partes[0]), Descricion: strings.TrimSpace(partes[1])}
	}
	return ProensCriterio{Descricion: texto}
}

// parseProensAvaliacionUD reads the "3.3. Avaliación das unidades
// didácticas" section: a repeating "Unidade didáctica N Título" label
// followed by a table#fila of criterio/tipo de instrumento/grao mínimo/%
// peso rows. "Molde" rows Proens leaves for the teacher to fill in (their
// first cell holds only an empty span.borrarFila) are skipped. Returns, per
// UD título, its list of avaliación instruments.
func parseProensAvaliacionUD(doc *goquery.Document) map[string][]ProensInstrumentoUD {
	sec := atopaSeccionProens(doc, "avaliacion das unidades")
	if sec == nil {
		return nil
	}

	porUD := map[string][]ProensInstrumentoUD{}

	sec.Find("label").Each(func(_ int, label *goquery.Selection) {
		if !strings.Contains(normalize(label.Text()), "unidade didactica") {
			return
		}
		// <div><label>Unidade didáctica</label><span style="font-weight:bolder;">N Título</span></div>
		wrapper := label.Parent()
		nomeSpan := strings.TrimSpace(wrapper.Find("span[style*='font-weight']").First().Text())
		_, titulo, encontrado := strings.Cut(nomeSpan, " ")
		if !encontrado {
			return
		}
		titulo = strings.TrimSpace(titulo)

		table := wrapper.NextAll().Filter("table.estiloListado").First()
		table.Find("tbody > tr").Each(func(_ int, row *goquery.Selection) {
			cells := row.Find("td")
			if cells.Length() < 4 {
				return
			}
			textoCriterio := cellText(cells.Eq(0))
			if textoCriterio == "" {
				return // fila "molde" (span.borrarFila baleiro), aínda sen encher
			}
			porUD[titulo] = append(porUD[titulo], ProensInstrumentoUD{
				Criterio:        partirCriterioAvaliacion(textoCriterio),
				TipoInstrumento: cellText(cells.Eq(1)),
				GraoMinimo:      cellText(cells.Eq(2)),
				Peso:            cellText(cells.Eq(3)),
			})
		})
	})

	return porUD
}

// partirCriterioAvaliacion splits a "CODIGO Texto..." span (3.3's format —
// código and texto sit on separate lines in the HTML, collapsed to a single
// space by cellText) into its código and its descrición.
func partirCriterioAvaliacion(texto string) ProensCriterio {
	if codigo, descricion, encontrado := strings.Cut(texto, " "); encontrado {
		return ProensCriterio{Codigo: strings.TrimSpace(codigo), Descricion: strings.TrimSpace(descricion)}
	}
	return ProensCriterio{Descricion: texto}
}
