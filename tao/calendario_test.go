package main

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// The tests run against the real page of the order that fixed the 2025/26 school
// calendar (DOG 92, 15.5.2025), saved under testdata/ in both of the languages it is
// published in. A calendar written by hand to look like the DOG is worth little here:
// the wording of the real one is what the patterns have to survive, and it turned out
// to say things no invented sample did.

// realDOG parses a saved DOG page the same way ImportSchoolCalendar does.
func realDOG(t *testing.T, file string) *SchoolCalendar {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatalf("non se puido ler o testdata: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("HTML non válido: %v", err)
	}
	doc.Find("script, style").Remove()
	return parseSchoolCalendar(normalize(doc.Find("body").Text()))
}

// The order opens by spanning the whole academic year ("desde o 1 de setembro de 2025
// ata o 31 de agosto de 2026") before giving, further down, the dates of the lective
// activities. It is the second one that the calendar of a materia runs on.
func TestRealDOGReadsLectiveDatesNotTheAcademicYear(t *testing.T) {
	for _, file := range []string{"dog-2025-26-gl.html", "dog-2025-26-es.html"} {
		t.Run(file, func(t *testing.T) {
			cal := realDOG(t, file)
			if cal.StartDate != "2025-09-08" {
				t.Errorf("StartDate = %q, quere 2025-09-08 (o 1 de setembro é o inicio do ano académico, non das clases)", cal.StartDate)
			}
			if cal.EndDate != "2026-06-19" {
				t.Errorf("EndDate = %q, quere 2026-06-19", cal.EndDate)
			}
		})
	}
}

// The DOG lists the vacation periods without naming them ("– Desde o día 22 de
// decembro de 2025 ata o día 7 de xaneiro de 2026"), so the name comes from the month.
func TestRealDOGReadsVacationPeriods(t *testing.T) {
	for _, file := range []string{"dog-2025-26-gl.html", "dog-2025-26-es.html"} {
		t.Run(file, func(t *testing.T) {
			cal := realDOG(t, file)
			want := []CalendarPeriod{
				{Type: "vacacions", Name: "Nadal", Start: "2025-12-22", End: "2026-01-07"},
				{Type: "vacacions", Name: "Entroido", Start: "2026-02-16", End: "2026-02-18"},
				{Type: "vacacions", Name: "Semana Santa", Start: "2026-03-30", End: "2026-04-06"},
			}
			for _, w := range want {
				got := findPeriod(cal, w.Name)
				if got == nil {
					t.Errorf("non se atopou %q; lido: %+v", w.Name, cal.Periods)
					continue
				}
				if *got != w {
					t.Errorf("%s = %+v, quere %+v", w.Name, *got, w)
				}
			}
		})
	}
}

// Article 7 declares two single non-lective days, one named and one not. Both editions
// must yield the same calendar, though they word the declaration differently
// ("celebrarase o 31 de outubro" / "se celebrará el 31 de octubre").
func TestRealDOGReadsDeclaredNonLectiveDays(t *testing.T) {
	for _, file := range []string{"dog-2025-26-gl.html", "dog-2025-26-es.html"} {
		t.Run(file, func(t *testing.T) {
			cal := realDOG(t, file)
			ensino := findPeriod(cal, "Día do Ensino")
			if ensino == nil || ensino.Start != "2025-10-31" || ensino.End != "" {
				t.Errorf("Día do Ensino = %+v, quere o 31-10-2025 como día solto", ensino)
			}
			generic := findPeriod(cal, "Día non lectivo")
			if generic == nil || generic.Start != "2025-11-03" {
				t.Errorf("día non lectivo sen nome = %+v, quere o 03-11-2025", generic)
			}
		})
	}
}

// The two editions of the order say the same thing in two languages: whichever the
// professor pastes, the materia must end up with the same calendar.
func TestRealDOGBothEditionsAgree(t *testing.T) {
	gl := realDOG(t, "dog-2025-26-gl.html")
	es := realDOG(t, "dog-2025-26-es.html")
	if gl.StartDate != es.StartDate || gl.EndDate != es.EndDate {
		t.Fatalf("as datas do curso non coinciden: gl=%s..%s, es=%s..%s",
			gl.StartDate, gl.EndDate, es.StartDate, es.EndDate)
	}
	if len(gl.Periods) != len(es.Periods) {
		t.Fatalf("número de períodos distinto: gl=%d, es=%d\ngl=%+v\nes=%+v",
			len(gl.Periods), len(es.Periods), gl.Periods, es.Periods)
	}
	for i := range gl.Periods {
		if gl.Periods[i] != es.Periods[i] {
			t.Errorf("período %d distinto:\n gl=%+v\n es=%+v", i, gl.Periods[i], es.Periods[i])
		}
	}
}

// The last article lists the commemorations centres may work on in class ("a semana do
// 6 de decembro de 2025: conmemoración da Constitución"). They are lective days, and
// reading one as a holiday would silently delete a class from every student's calendar.
func TestRealDOGNeverReadsCommemorationsAsHolidays(t *testing.T) {
	for _, file := range []string{"dog-2025-26-gl.html", "dog-2025-26-es.html"} {
		t.Run(file, func(t *testing.T) {
			cal := realDOG(t, file)
			for _, p := range cal.Periods {
				if p.Start == "2025-12-06" || strings.Contains(p.Name, "Constitución") {
					t.Errorf("unha conmemoración coouse como día non lectivo: %+v", p)
				}
				// The deadline to ask for the centre's own days lives in article 7 too.
				if p.Start == "2025-10-17" {
					t.Errorf("un prazo administrativo coouse como día non lectivo: %+v", p)
				}
			}
		})
	}
}

func TestRealDOGProducesNoWarningsAndSortedPeriods(t *testing.T) {
	cal := realDOG(t, "dog-2025-26-gl.html")
	if len(cal.Warnings) != 0 {
		t.Errorf("non se esperaban avisos: %v", cal.Warnings)
	}
	if len(cal.Periods) != 5 {
		t.Errorf("esperábanse 5 períodos (3 vacacións + 2 días non lectivos), obtívose %d: %+v",
			len(cal.Periods), cal.Periods)
	}
	for i := 1; i < len(cal.Periods); i++ {
		if cal.Periods[i-1].Start > cal.Periods[i].Start {
			t.Fatalf("períodos sen ordenar: %+v", cal.Periods)
		}
	}
	for _, p := range cal.Periods {
		if p.Start < cal.StartDate || p.Start > cal.EndDate {
			t.Errorf("período fóra do curso: %+v", p)
		}
	}
}

// ---- Curso 2026/27 (DOG 110, 15.6.2026) -----------------------------------------
//
// The order for 2026/27 is worded differently from the one before it, and each
// difference broke something: the Semana Santa is written "Do día 22 ao 29 de marzo de
// 2027" (the first date carries no month), and the Día do Ensino moved to December.

func TestReal2627ReadsTheWholeCourse(t *testing.T) {
	for _, file := range []string{"dog-2026-27-gl.html", "dog-2026-27-es.html"} {
		t.Run(file, func(t *testing.T) {
			cal := realDOG(t, file)
			if cal.StartDate != "2026-09-09" || cal.EndDate != "2027-06-21" {
				t.Errorf("curso = %s..%s, quere 2026-09-09..2027-06-21", cal.StartDate, cal.EndDate)
			}
			want := []CalendarPeriod{
				{Type: "festivo", Name: "Día do Ensino", Start: "2026-12-07"},
				{Type: "vacacions", Name: "Nadal", Start: "2026-12-22", End: "2027-01-07"},
				{Type: "vacacions", Name: "Entroido", Start: "2027-02-08", End: "2027-02-10"},
				// "Do día 22 ao 29 de marzo de 2027": só a segunda data leva o mes.
				{Type: "vacacions", Name: "Semana Santa", Start: "2027-03-22", End: "2027-03-29"},
			}
			for _, w := range want {
				got := findPeriod(cal, w.Name)
				if got == nil {
					t.Errorf("non se atopou %q; lido: %+v", w.Name, cal.Periods)
					continue
				}
				if *got != w {
					t.Errorf("%s = %+v, quere %+v", w.Name, *got, w)
				}
			}
			if len(cal.Periods) != len(want) {
				t.Errorf("esperábanse %d períodos, obtívose %d: %+v", len(want), len(cal.Periods), cal.Periods)
			}
			if len(cal.Warnings) != 0 {
				t.Errorf("non se esperaban avisos: %v", cal.Warnings)
			}
		})
	}
}

func TestReal2627BothEditionsAgree(t *testing.T) {
	gl := realDOG(t, "dog-2026-27-gl.html")
	es := realDOG(t, "dog-2026-27-es.html")
	if gl.StartDate != es.StartDate || gl.EndDate != es.EndDate || len(gl.Periods) != len(es.Periods) {
		t.Fatalf("as edicións non coinciden:\n gl=%+v\n es=%+v", gl, es)
	}
	for i := range gl.Periods {
		if gl.Periods[i] != es.Periods[i] {
			t.Errorf("período %d distinto:\n gl=%+v\n es=%+v", i, gl.Periods[i], es.Periods[i])
		}
	}
}

// The 2026/27 order declares one single non-lective day, and it is no longer the 31st
// of October: reading it from the order rather than assuming the usual date is what
// keeps the calendar right when the Xunta moves it.
func TestReal2627ReadsTheMovedDiaDoEnsino(t *testing.T) {
	cal := realDOG(t, "dog-2026-27-gl.html")
	p := findPeriod(cal, "Día do Ensino")
	if p == nil || p.Start != "2026-12-07" {
		t.Fatalf("Día do Ensino = %+v, quere o 07-12-2026", p)
	}
	if findPeriod(cal, "Día non lectivo") != nil {
		t.Errorf("esta orde non declara ningún outro día non lectivo: %+v", cal.Periods)
	}
}

// A period written in a wording the patterns do not know must be reported, not dropped
// in silence: the professor has to know that a week is missing from the table.
func TestParseSchoolCalendarWarnsAboutAPeriodItCannotRead(t *testing.T) {
	text := normalize(`Artigo 6. Vacacións escolares Os períodos serán os seguintes:
	– Desde o día 22 de decembro de 2026 ata o día 7 de xaneiro de 2027, ambos incluídos.
	– Os días 8, 9 e 10 de febreiro de 2027.
	– Na primavera, arredor da Semana Santa, segundo acorde o centro.
	Artigo 7. Días non lectivos`)
	cal := parseSchoolCalendar(text)
	if len(cal.Periods) != 2 {
		t.Fatalf("esperábanse 2 períodos lidos, obtívose %+v", cal.Periods)
	}
	found := false
	for _, w := range cal.Warnings {
		if strings.Contains(w, "3 períodos") && strings.Contains(w, "ler 2") {
			found = true
		}
	}
	if !found {
		t.Errorf("esperábase un aviso de que quedou un período sen ler; avisos: %v", cal.Warnings)
	}
}

// An order that does name its periods must keep the names it gives them.
func TestParseSchoolCalendarPrefersTheNameTheOrderGives(t *testing.T) {
	text := normalize(`Artigo 6. Vacacións escolares
	a) Nadal: desde o día 20 de decembro de 2025 ata o día 7 de xaneiro de 2026.
	Artigo 7. Días non lectivos`)
	cal := parseSchoolCalendar(text)
	p := findPeriod(cal, "Nadal")
	if p == nil || p.Start != "2025-12-20" {
		t.Fatalf("Nadal = %+v, quere comezar o 20-12-2025", p)
	}
}

// A page that is not a calendar order must not fail: it comes back empty, with the
// warnings that tell the professor to fill the table by hand.
func TestParseSchoolCalendarWarnsWhenNothingIsFound(t *testing.T) {
	cal := parseSchoolCalendar(normalize("Unha páxina calquera sen datas nin calendario."))
	if cal.StartDate != "" || cal.EndDate != "" {
		t.Fatalf("non se esperaban datas: %+v", cal)
	}
	if len(cal.Periods) != 0 {
		t.Fatalf("non se esperaban períodos: %+v", cal.Periods)
	}
	if len(cal.Warnings) != 3 {
		t.Fatalf("esperábanse 3 avisos (inicio, remate, períodos), obtívose %v", cal.Warnings)
	}
}

func TestDateFromRejectsImpossibleDates(t *testing.T) {
	if _, ok := dateFrom("31", "febreiro", "2026"); ok {
		t.Fatal("o 31 de febreiro non existe")
	}
	if _, ok := dateFrom("8", "mes_inventado", "2026"); ok {
		t.Fatal("esperábase rexeitar un mes descoñecido")
	}
	if d, ok := dateFrom("8", "setembro", "2025"); !ok || d != "2025-09-08" {
		t.Fatalf("dateFrom = %q, %v; quere 2025-09-08", d, ok)
	}
}

func TestImportSchoolCalendarRequiresProfessor(t *testing.T) {
	a := NewApp()
	if _, err := a.ImportSchoolCalendar("https://www.xunta.gal/dog/"); err == nil {
		t.Fatal("esperábase erro: só o profesorado importa o calendario")
	}
}

func findPeriod(cal *SchoolCalendar, name string) *CalendarPeriod {
	for i := range cal.Periods {
		if cal.Periods[i].Name == name {
			return &cal.Periods[i]
		}
	}
	return nil
}
