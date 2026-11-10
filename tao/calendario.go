package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// School calendar import.
//
// The professor pastes the URL of the DOG order that fixes the school calendar and
// the app extracts, by pattern matching, the start and end of the lective activities
// plus the well-known non-lective stretches (Nadal, Entroido, Semana Santa) and
// festive days. What is extracted only seeds the editable table in the frontend: the
// DOG's wording changes from year to year, so anything the patterns miss or misread
// is meant to be fixed there by hand, and the centre's own days are added there too.
//
// The page is fetched here rather than in the webview because the DOG sends no CORS
// headers: doing it in Go avoids bouncing the request off a third-party proxy.

// CalendarPeriod is one non-lective stretch read from the calendar order. End is
// empty for a single day.
type CalendarPeriod struct {
	Type  string `json:"type"` // "vacacions" | "festivo"
	Name  string `json:"name"`
	Start string `json:"start"` // YYYY-MM-DD
	End   string `json:"end"`   // YYYY-MM-DD, "" for a single day
}

// SchoolCalendar is what the frontend turns into the editable table. Warnings names
// what could not be read, so the professor knows what to complete by hand.
type SchoolCalendar struct {
	StartDate string           `json:"startDate"`
	EndDate   string           `json:"endDate"`
	Periods   []CalendarPeriod `json:"periods"`
	Warnings  []string         `json:"warnings"`
}

// ---- Patterns --------------------------------------------------------------------
//
// Every pattern runs over normalize()d text: lowercased, unaccented and with
// whitespace collapsed, so "Xuño"/"xuno" and line breaks inside a sentence do not
// matter. Galician and Spanish month names are both accepted, since the DOG is
// published in both languages.

const monthsPattern = `xaneiro|enero|febreiro|febrero|marzo|abril|maio|mayo|xuno|junio|` +
	`xullo|julio|agosto|setembro|septiembre|setiembre|outubro|octubre|novembro|noviembre|decembro|diciembre`

var monthByName = map[string]int{
	"xaneiro": 1, "enero": 1,
	"febreiro": 2, "febrero": 2,
	"marzo": 3,
	"abril": 4,
	"maio":  5, "mayo": 5,
	"xuno": 6, "junio": 6,
	"xullo": 7, "julio": 7,
	"agosto":   8,
	"setembro": 9, "septiembre": 9, "setiembre": 9,
	"outubro": 10, "octubre": 10,
	"novembro": 11, "noviembre": 11,
	"decembro": 12, "diciembre": 12,
}

var (
	// "desde o día 20 de decembro de 2025 ata o 7 de xaneiro de 2026". The year of the
	// first date is optional: the DOG often writes it only once ("desde o 30 de marzo
	// ata o 6 de abril de 2026").
	rangeRe = regexp.MustCompile(
		`(?:desde|dende|do)\s+(?:(?:o|el)\s+)?(?:dia\s+)?(\d{1,2})\s+de\s+(` + monthsPattern + `)(?:\s+de\s+(\d{4}))?` +
			`\s+(?:ata|ate|hasta|ao|al|a)\s+(?:(?:o|el)\s+)?(?:dia\s+)?(\d{1,2})\s+de\s+(` + monthsPattern + `)\s+de\s+(\d{4})`)

	// "Do día 22 ao 29 de marzo de 2027" / "Desde el día 22 hasta el día 29 de marzo de
	// 2027": a range inside one month, where only the second date carries the month and
	// the year. Both editions of the 2026/27 order write the Semana Santa this way.
	sameMonthRangeRe = regexp.MustCompile(
		`(?:do|del|desde|dende)\s+(?:(?:o|el)\s+)?(?:dia\s+)?(\d{1,2})\s+(?:ata|ate|hasta|ao|al)\s+(?:(?:o|el)\s+)?(?:dia\s+)?(\d{1,2})\s+de\s+(` +
			monthsPattern + `)\s+de\s+(\d{4})`)

	// "16, 17 e 18 de febreiro de 2026" — how the DOG usually writes Entroido.
	listRe = regexp.MustCompile(
		`(\d{1,2}(?:\s*,\s*\d{1,2})*)\s*(?:e|y)\s*(\d{1,2})\s+de\s+(` + monthsPattern + `)\s+de\s+(\d{4})`)

	// "8 de setembro de 2025".
	singleRe = regexp.MustCompile(`(\d{1,2})\s+de\s+(` + monthsPattern + `)\s+de\s+(\d{4})`)

	startDateRe = regexp.MustCompile(`(\d{1,2})\s+de\s+(?:setembro|septiembre|setiembre)\s+de\s+(\d{4})`)
	endDateRe   = regexp.MustCompile(`(\d{1,2})\s+de\s+(?:xuno|junio)\s+de\s+(\d{4})`)
)

// nonLectiveDeclRe matches the sentence by which the order declares a single day
// non-lective, in both of the shapes it uses:
//
//	"O Día do Ensino, que terá a consideración de día non lectivo, celebrarase o 31 de outubro de 2025."
//	"Tamén terá a consideración de día non lectivo o 3 de novembro de 2025."
//	"El Día de la Enseñanza, que tendrá la consideración de día no lectivo, se celebrará el 31 de octubre de 2025."
//
// Reading only these declarations is what keeps the other dates of the article out —
// notably the deadline centres have to ask for their own days ("antes do día 17 de
// outubro de 2025"), which is not a holiday at all.
var nonLectiveDeclRe = regexp.MustCompile(
	`consideracion de dia no[n]? lectivo,?\s*(?:(?:se\s+)?celebrara(?:se)?\s+)?(?:(?:o|el)\s+)?(?:dia\s+)?(\d{1,2})\s+de\s+(` +
		monthsPattern + `)\s+de\s+(\d{4})`)

// The order is a list of numbered articles, and each fact lives in a known one, so
// every pattern is run only over the article that holds it. Articles are addressed by
// their title rather than their number, which changes from year to year.
//
// Anchoring on the title *as an article heading* is what keeps a section from starting
// at the preamble, where the same words appear in passing ("...os períodos lectivos,
// os non lectivos e as vacacións escolares...") and would otherwise swallow half the
// order. It is also what stops the commemorations article ("a semana do 6 de decembro
// de 2025: conmemoración da Constitución" — a classroom topic, not a day off) from
// reaching the table.
var (
	articleHeadingRe = regexp.MustCompile(`(?:artigo|articulo)\s+\d+\.`)

	lectiveTitleRe    = regexp.MustCompile(`(?:artigo|articulo)\s+\d+\.\s*calendario d(?:as|e las)\s+actividades lectivas`)
	vacationTitleRe   = regexp.MustCompile(`(?:artigo|articulo)\s+\d+\.\s*vacacion(?:s|es)\s+escolares`)
	nonLectiveTitleRe = regexp.MustCompile(`(?:artigo|articulo)\s+\d+\.\s*dias no[n]?\s+lectivos`)
)

// vacationKeywords name a period when the order happens to name it. The DOG usually
// does not (it just lists the dates under "Vacacións escolares"), in which case the
// name is derived from the month — see vacationName.
var vacationKeywords = []struct{ key, name string }{
	{"nadal", "Nadal"},
	{"navidad", "Nadal"},
	{"entroido", "Entroido"},
	{"carnaval", "Entroido"},
	{"semana santa", "Semana Santa"},
}

// festiveKeywords name a declared non-lective day when one of them sits beside the
// declaration. Anything else declared non-lective keeps the generic name.
var festiveKeywords = []struct{ key, name string }{
	{"dia do ensino", "Día do Ensino"},
	{"dia de la ensenanza", "Día do Ensino"},
	{"todos os santos", "Todos os Santos"},
	{"todos los santos", "Todos os Santos"},
	{"inmaculada", "Inmaculada Concepción"},
	{"dia das letras galegas", "Letras Galegas"},
	{"santiago apostolo", "Santiago Apóstolo"},
	{"dia de galicia", "Día de Galicia"},
}

// ---- Public API ------------------------------------------------------------------

// ImportSchoolCalendar downloads the calendar order at rawURL and extracts what it
// can of the school calendar. Professor only: it seeds the programación every student
// of the materia will read.
func (a *App) ImportSchoolCalendar(rawURL string) (*SchoolCalendar, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	text, err := fetchCalendarText(rawURL)
	if err != nil {
		return nil, err
	}
	return parseSchoolCalendar(text), nil
}

// fetchCalendarText downloads a page and returns its visible text, normalized.
func fetchCalendarText(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("a URL do calendario está baleira")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("a URL do calendario non é válida: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "+
		"(KHTML, like Gecko) Chrome/120.0 Safari/537.36")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("non se puido descargar o calendario: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("a páxina do calendario devolveu un erro (%d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("non se puido ler a páxina do calendario: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("non se puido analizar a páxina do calendario: %w", err)
	}
	doc.Find("script, style").Remove()
	text := normalize(doc.Find("body").Text())
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("a páxina do calendario non ten texto lexible")
	}
	return text, nil
}

// parseSchoolCalendar extracts the calendar from the already-normalized page text.
// It never fails: whatever cannot be read is reported in Warnings and left for the
// professor to fill in the table.
func parseSchoolCalendar(text string) *SchoolCalendar {
	cal := &SchoolCalendar{Periods: []CalendarPeriod{}, Warnings: []string{}}

	cal.StartDate, cal.EndDate = parseLectiveDates(text)
	if cal.StartDate == "" {
		cal.Warnings = append(cal.Warnings, "Non se puido ler a data de inicio do curso.")
	}
	if cal.EndDate == "" {
		cal.Warnings = append(cal.Warnings, "Non se puido ler a data de remate do curso.")
	}

	vacSec := article(text, vacationTitleRe)
	vacations := parseVacations(vacSec)
	cal.Periods = append(cal.Periods, vacations...)
	cal.Periods = append(cal.Periods, parseNonLectiveDays(article(text, nonLectiveTitleRe))...)
	cal.Periods = dropPeriodsOutsideCourse(cal.Periods, cal.StartDate, cal.EndDate)

	// The order lists the vacation periods as bullets. Comparing how many it lists with
	// how many were understood is the one check that catches a period written in a
	// wording the patterns do not know yet — otherwise a whole week of holidays would
	// go missing from every student's calendar without a word.
	if listed := strings.Count(vacSec, "–"); listed > len(vacations) {
		cal.Warnings = append(cal.Warnings, fmt.Sprintf(
			"O calendario lista %d períodos de vacacións pero só se puideron ler %d: revisa a táboa e engade o que falte.",
			listed, len(vacations)))
	}

	sort.Slice(cal.Periods, func(i, j int) bool { return cal.Periods[i].Start < cal.Periods[j].Start })

	if len(cal.Periods) == 0 {
		cal.Warnings = append(cal.Warnings,
			"Non se atopou ningún período vacacional nin día festivo; engádeos na táboa.")
	}
	return cal
}

// article returns the text of the article whose heading titleRe matches, up to the
// heading of the next one. It returns "" when the order has no such article.
func article(text string, titleRe *regexp.Regexp) string {
	loc := titleRe.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	body := text[loc[0]:]
	// Search for the next heading past this one's, which articleHeadingRe also matches.
	headingLen := loc[1] - loc[0]
	if next := articleHeadingRe.FindStringIndex(body[headingLen:]); next != nil {
		return body[:headingLen+next[0]]
	}
	return body
}

// parseLectiveDates reads the start and the end of the lective activities. The order
// writes them as a single range in its "Calendario das actividades lectivas" article
// ("realizaranse desde o día 8 de setembro de 2025 ata o 19 de xuño de 2026"), and the
// first range of that article is the general one; the ones that follow are for
// particular ensinanzas (FP, réxime especial).
//
// Reading them from that article is what avoids the trap of the previous article,
// which spans the whole academic year ("desde o 1 de setembro de 2025 ata o 31 de
// agosto de 2026") and would otherwise pass as the start of the classes.
func parseLectiveDates(text string) (string, string) {
	if sec := article(text, lectiveTitleRe); sec != "" {
		if m, _, ok := submatches(rangeRe, sec); ok {
			if start, end, ok := rangeFromMatch(m); ok {
				return start, end
			}
		}
	}
	// No such article: fall back to the first September and June dates of the page.
	return findFirstDate(text, startDateRe, "setembro"), findFirstDate(text, endDateRe, "xuno")
}

// parseVacations reads every period listed in the "Vacacións escolares" article, each
// written either as a range ("Desde o día 22 de decembro de 2025 ata o día 7 de
// xaneiro de 2026") or as a list of days ("Os días 16, 17 e 18 de febreiro de 2026").
func parseVacations(sec string) []CalendarPeriod {
	if sec == "" {
		return nil
	}
	type match struct {
		start, end string
		at, until  int
	}
	var found []match

	for _, loc := range rangeRe.FindAllStringSubmatchIndex(sec, -1) {
		if start, end, ok := rangeFromMatch(groupsAt(sec, loc)); ok {
			found = append(found, match{start, end, loc[0], loc[1]})
		}
	}
	for _, loc := range sameMonthRangeRe.FindAllStringSubmatchIndex(sec, -1) {
		if start, end, ok := rangeFromSameMonth(groupsAt(sec, loc)); ok {
			found = append(found, match{start, end, loc[0], loc[1]})
		}
	}
	for _, loc := range listRe.FindAllStringSubmatchIndex(sec, -1) {
		if start, end, ok := rangeFromList(groupsAt(sec, loc)); ok {
			found = append(found, match{start, end, loc[0], loc[1]})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at < found[j].at })

	var out []CalendarPeriod
	lastEnd := -1
	for _, m := range found {
		if m.at < lastEnd {
			continue // overlaps a period already read
		}
		lastEnd = m.until
		out = append(out, CalendarPeriod{
			Type:  "vacacions",
			Name:  vacationName(sec, m.at, m.start),
			Start: m.start,
			End:   m.end,
		})
	}
	return out
}

// vacationName names a period. The order normally lists the dates without naming them,
// so the name comes from the month it falls in — which for the school calendar is
// unambiguous. A period the order does name keeps its own name.
func vacationName(sec string, at int, start string) string {
	context := sec[max(at-90, 0):min(at+90, len(sec))]
	for _, kw := range vacationKeywords {
		if strings.Contains(context, kw.key) {
			return kw.name
		}
	}
	switch month := start[5:7]; month {
	case "12", "01":
		return "Nadal"
	case "02":
		return "Entroido"
	case "03", "04":
		return "Semana Santa"
	}
	return "Vacacións"
}

// parseNonLectiveDays reads the single days the "Días non lectivos" article declares.
func parseNonLectiveDays(sec string) []CalendarPeriod {
	if sec == "" {
		return nil
	}
	var out []CalendarPeriod
	seen := map[string]bool{}
	for _, loc := range nonLectiveDeclRe.FindAllStringSubmatchIndex(sec, -1) {
		m := groupsAt(sec, loc)
		d, ok := dateFrom(m[1], m[2], m[3])
		if !ok || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, CalendarPeriod{Type: "festivo", Name: nonLectiveName(sec, loc[0]), Start: d})
	}
	return out
}

// nonLectiveName names a declared day from the words around the declaration ("O Día do
// Ensino, que terá a consideración de día non lectivo, celebrarase o..."), and falls
// back to the generic name for the ones the order leaves unnamed.
func nonLectiveName(sec string, at int) string {
	context := sec[max(at-90, 0):min(at+40, len(sec))]
	for _, kw := range festiveKeywords {
		if strings.Contains(context, kw.key) {
			return kw.name
		}
	}
	return "Día non lectivo"
}

// submatches returns re's groups and where the match starts, so two competing patterns
// can be compared by position.
func submatches(re *regexp.Regexp, s string) ([]string, int, bool) {
	loc := re.FindStringSubmatchIndex(s)
	if loc == nil {
		return nil, 0, false
	}
	return groupsAt(s, loc), loc[0], true
}

// groupsAt extracts the captured groups of a match index pair list, with "" for the
// groups that did not participate (an optional year).
func groupsAt(s string, loc []int) []string {
	out := make([]string, len(loc)/2)
	for i := range out {
		if loc[2*i] >= 0 {
			out[i] = s[loc[2*i]:loc[2*i+1]]
		}
	}
	return out
}

// dropPeriodsOutsideCourse removes anything that falls outside the course, which is
// what filters out the stray dates an order carries (its own date, references to
// other rules). With no course bounds read, nothing can be judged, so all is kept.
func dropPeriodsOutsideCourse(periods []CalendarPeriod, startDate, endDate string) []CalendarPeriod {
	if startDate == "" || endDate == "" {
		return periods
	}
	out := make([]CalendarPeriod, 0, len(periods))
	for _, p := range periods {
		last := p.End
		if last == "" {
			last = p.Start
		}
		if last < startDate || p.Start > endDate {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ---- Match helpers ---------------------------------------------------------------

// rangeFromMatch turns a rangeRe match into two dates. When the first date carries no
// year it is inferred from the second: a range that runs backwards (decembro → xaneiro)
// must have started the year before.
func rangeFromMatch(m []string) (string, string, bool) {
	day1, mon1, year1 := m[1], m[2], m[3]
	day2, mon2, year2 := m[4], m[5], m[6]

	if year1 == "" {
		m1, ok1 := monthByName[mon1]
		m2, ok2 := monthByName[mon2]
		if !ok1 || !ok2 {
			return "", "", false
		}
		y2, err := strconv.Atoi(year2)
		if err != nil {
			return "", "", false
		}
		if m1 > m2 {
			year1 = strconv.Itoa(y2 - 1)
		} else {
			year1 = year2
		}
	}

	start, ok := dateFrom(day1, mon1, year1)
	if !ok {
		return "", "", false
	}
	end, ok := dateFrom(day2, mon2, year2)
	if !ok {
		return "", "", false
	}
	if end < start {
		return "", "", false
	}
	return start, end, true
}

// rangeFromSameMonth turns "Do día 22 ao 29 de marzo de 2027" into two dates: both
// days belong to the month and year that only the second one spells out.
func rangeFromSameMonth(m []string) (string, string, bool) {
	day1, day2, mon, year := m[1], m[2], m[3], m[4]
	start, ok := dateFrom(day1, mon, year)
	if !ok {
		return "", "", false
	}
	end, ok := dateFrom(day2, mon, year)
	if !ok || end < start {
		return "", "", false
	}
	return start, end, true
}

// rangeFromList turns "16, 17 e 18 de febreiro de 2026" into its first and last day.
func rangeFromList(m []string) (string, string, bool) {
	head, lastDay, mon, year := m[1], m[2], m[3], m[4]
	days := strings.Split(head, ",")
	firstDay := strings.TrimSpace(days[0])

	start, ok := dateFrom(firstDay, mon, year)
	if !ok {
		return "", "", false
	}
	end, ok := dateFrom(lastDay, mon, year)
	if !ok || end < start {
		return "", "", false
	}
	return start, end, true
}

// dateFrom builds a YYYY-MM-DD date, rejecting one that does not exist (31 February).
func dateFrom(day, month, year string) (string, bool) {
	mo, ok := monthByName[month]
	if !ok {
		return "", false
	}
	d, err := strconv.Atoi(day)
	if err != nil {
		return "", false
	}
	y, err := strconv.Atoi(year)
	if err != nil {
		return "", false
	}
	t := time.Date(y, time.Month(mo), d, 12, 0, 0, 0, time.UTC)
	if t.Day() != d || int(t.Month()) != mo {
		return "", false // e.g. "31 de febreiro"
	}
	return t.Format("2006-01-02"), true
}

// findFirstDate returns the first date in text matching re, whose month the pattern
// pins instead of capturing.
func findFirstDate(text string, re *regexp.Regexp, month string) string {
	if m := re.FindStringSubmatch(text); m != nil {
		if d, ok := dateFrom(m[1], month, m[2]); ok {
			return d
		}
	}
	return ""
}
