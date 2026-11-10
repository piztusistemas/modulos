package main

import (
	"encoding/json"
	"sort"
	"time"
)

// Espello, en Go, do subconxunto de MateriaDef (tao/profesor.go) e dos tipos
// do frontend (tao/frontend/src/types.ts) que Malta precisa interpretar:
// Tao xa devolve MateriaDef completo por GET /api/v1/modulos/materias (ver
// docs/api-tao.md §6.1), pero co JSON real de Units/Config (o que escribiu
// o frontend), non coa forma `any` solta que ten o struct Go de Tao — así
// que Malta tipa eses dous campos á súa maneira para poder calcular.

type MateriaDef struct {
	ID                        string          `json:"id"`
	Name                      string          `json:"name"`
	AcademicYear              string          `json:"academicYear"`
	Groups                    []string        `json:"groups"`
	Config                    AppConfig       `json:"config"`
	Units                     []Unit          `json:"units"`
	Logs                      map[string]Log  `json:"logs,omitempty"`
	ProgramacionInfo          json.RawMessage `json:"programacionInfo,omitempty"`
	ProgramacionInfoUpdatedAt string          `json:"programacionInfoUpdatedAt,omitempty"`
}

type Unit struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Sessions   int    `json:"sessions"`
	Obxectivos string `json:"obxectivos,omitempty"`
	Contidos   string `json:"contidos,omitempty"`
	Criterios  string `json:"criterios,omitempty"`
}

type Log struct {
	Date    string `json:"date"`
	Content string `json:"content"`
	Status  string `json:"status"` // 'lectivo'|'folga'|'excursion'|'ausencia'|'exame'
}

type holidayEntry struct {
	Date string `json:"date"`
	Name string `json:"name"`
}

type calendarEntry struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

type AppConfig struct {
	StartDate      string          `json:"startDate"`
	EndDate        string          `json:"endDate"`
	ScheduleDays   []int           `json:"scheduleDays"`
	Holidays       []holidayEntry  `json:"holidays,omitempty"`
	CustomHolidays []holidayEntry  `json:"customHolidays,omitempty"`
	Calendar       []calendarEntry `json:"calendar,omitempty"`
}

const layoutData = "2006-01-02"

func parseData(s string) (time.Time, bool) {
	t, err := time.Parse(layoutData, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// nonLectiveDays porta data.ts:nonLectiveDays — a táboa do calendario
// (Calendar) manda se existe (aínda que veña baleira), senón as listas do
// formato antigo (Holidays+CustomHolidays).
func nonLectiveDays(cfg AppConfig) map[string]string {
	days := map[string]string{}
	if cfg.Calendar != nil {
		for _, e := range cfg.Calendar {
			if e.Start == "" {
				continue
			}
			start, ok := parseData(e.Start)
			if !ok {
				continue
			}
			last := e.Start
			if e.End != "" && e.End >= e.Start {
				last = e.End
			}
			end, ok := parseData(last)
			if !ok {
				continue
			}
			nome := e.Name
			if nome == "" {
				nome = "Día non lectivo"
			}
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				k := d.Format(layoutData)
				if _, existe := days[k]; !existe {
					days[k] = nome
				}
			}
		}
		return days
	}
	for _, h := range append(append([]holidayEntry{}, cfg.Holidays...), cfg.CustomHolidays...) {
		if h.Date == "" {
			continue
		}
		if _, existe := days[h.Date]; !existe {
			days[h.Date] = h.Name
		}
	}
	return days
}

// diaProgramado é o resultado, para UNHA data, de camiñar a programación:
// que unidade lle corresponde (se algunha) e se foi unha sesión de clase
// efectivamente consumida (non excepción).
type diaProgramado struct {
	Data      string
	Unidade   *Unit
	Exception bool
	ClassDay  bool
}

// scheduleDaysOuDefecto replica o "config.scheduleDays || [1,3,5]" do
// frontend (data.ts) — unha materia sen configurar aínda cae en L/X/V.
func scheduleDaysOuDefecto(cfg AppConfig) map[int]bool {
	dias := cfg.ScheduleDays
	if len(dias) == 0 {
		dias = []int{1, 3, 5}
	}
	m := map[int]bool{}
	for _, d := range dias {
		m[d] = true
	}
	return m
}

// programarCalendario porta a parte de asignación secuencial de
// generateMonthlySchedule (data.ts) — camiña TODO o rango [startDate,
// endDate] día a día, consumindo sesións de Units en orde. Non fai falla
// replicar o agrupamento por meses (só é para a grella visual de Tao); a
// Malta interésalle só a lista plana de días programados.
func programarCalendario(m MateriaDef) []diaProgramado {
	start, ok1 := parseData(m.Config.StartDate)
	end, ok2 := parseData(m.Config.EndDate)
	if !ok1 || !ok2 || end.Before(start) {
		return nil
	}
	nonLective := nonLectiveDays(m.Config)
	scheduleDays := scheduleDaysOuDefecto(m.Config)

	var out []diaProgramado
	unitIdx := 0
	sesionsAsignadas := 0

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dataStr := d.Format(layoutData)
		weekday := int(d.Weekday()) // 0=domingo...6=sábado, igual ca JS Date.getDay()
		isWeekend := weekday == 0 || weekday == 6
		_, isHoliday := nonLective[dataStr]
		isClassDay := !isHoliday && !isWeekend && scheduleDays[weekday]

		log, hasLog := m.Logs[dataStr]
		isException := hasLog && (log.Status == "folga" || log.Status == "excursion" || log.Status == "ausencia")

		dp := diaProgramado{Data: dataStr, ClassDay: isClassDay, Exception: isException}
		if isClassDay && !isException && unitIdx < len(m.Units) {
			u := m.Units[unitIdx]
			dp.Unidade = &u
			sesionsAsignadas++
			if sesionsAsignadas >= u.Sessions {
				unitIdx++
				sesionsAsignadas = 0
			}
		}
		out = append(out, dp)
	}
	return out
}

// unidadeMetricas resume, para UNHA unidade, o comparado programado/real que
// pide o informe: sesións programadas (Unit.Sessions), sesións reais
// (días de clase que lle tocaron e SE deron, exame incluído — mesmo
// criterio ca App.tsx: "if it's an exam, it still consumes a session"),
// primeiro/último día traballado, e o grao de consecución derivado
// DETERMINISTICAMENTE da proporción real/programado (a IA nunca inventa este
// número — só redacta o resumo en prosa a partir del, ver informes.go).
type unidadeMetricas struct {
	Unit         Unit
	SesionsReais int
	PrimeiroDia  string
	UltimoDia    string
	// Contidos leva os logs con contido asignados a esta unidade — todos os
	// que ten, en calcularMetricasUnidade (a unidade enteira é o ámbito); só
	// os do rango pedido, en calcularMetricas (varias unidades, un mes/rango).
	Contidos []Log
	// Só no informe mensual (calcularMetricas): sesións da unidade dadas
	// DENTRO do rango pedido, e sesións acumuladas desde o inicio do curso
	// ata o final do rango — a base do "% da unidade" traballado ata ese mes.
	SesionsMes        int
	SesionsAcumuladas int
}

// porcentaxeUnidade: sesións dadas sobre as programadas, en enteiro
// redondeado e topado a 100. 0 se a unidade non ten sesións programadas.
func porcentaxeUnidade(programadas, dadas int) int {
	if programadas <= 0 {
		return 0
	}
	p := (dadas*100 + programadas/2) / programadas
	if p > 100 {
		p = 100
	}
	return p
}

// calcularMetricas devolve, para cada unidade coa que polo menos un día do
// rango [dende, ata] coincide, as súas métricas completas (sobre TODO o
// calendario da materia, non recortadas ao rango: o grao de consecución
// dunha unidade non ten sentido partido). A orde de saída é a de Units.
func calcularMetricas(m MateriaDef, dende, ata string) []unidadeMetricas {
	dias := programarCalendario(m)
	porUnidade := map[int]*unidadeMetricas{}
	tocadasNoRango := map[int]bool{}

	for _, d := range dias {
		if d.Unidade == nil {
			continue
		}
		id := d.Unidade.ID
		um, existe := porUnidade[id]
		if !existe {
			um = &unidadeMetricas{Unit: *d.Unidade}
			porUnidade[id] = um
		}
		if !d.Exception {
			um.SesionsReais++
			if um.PrimeiroDia == "" {
				um.PrimeiroDia = d.Data
			}
			um.UltimoDia = d.Data
		}
		if !d.Exception && d.Data <= ata {
			um.SesionsAcumuladas++
		}
		if d.Data >= dende && d.Data <= ata {
			tocadasNoRango[id] = true
			if !d.Exception {
				um.SesionsMes++
			}
			if log, ok := m.Logs[d.Data]; ok && log.Content != "" {
				um.Contidos = append(um.Contidos, log)
			}
		}
	}

	var out []unidadeMetricas
	for _, u := range m.Units {
		if tocadasNoRango[u.ID] {
			out = append(out, *porUnidade[u.ID])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Unit.ID < out[j].Unit.ID
	})
	return out
}

// calcularMetricasUnidade é a variante de calcularMetricas para a petición
// expresa "Seguimento por unidade" (App.tsx): computa as métricas de UNHA
// soa unidade (unitID), collendo TODOS os contidos traballados que ten
// asignados en todo o calendario da materia — aquí non hai rango de datas,
// a unidade enteira é o ámbito. ok=false se unitID non existe na materia.
func calcularMetricasUnidade(m MateriaDef, unitID int) (unidadeMetricas, bool) {
	var alvo *Unit
	for i := range m.Units {
		if m.Units[i].ID == unitID {
			alvo = &m.Units[i]
			break
		}
	}
	if alvo == nil {
		return unidadeMetricas{}, false
	}
	um := unidadeMetricas{Unit: *alvo}
	for _, d := range programarCalendario(m) {
		if d.Unidade == nil || d.Unidade.ID != unitID {
			continue
		}
		if !d.Exception {
			um.SesionsReais++
			if um.PrimeiroDia == "" {
				um.PrimeiroDia = d.Data
			}
			um.UltimoDia = d.Data
		}
		if log, ok := m.Logs[d.Data]; ok && log.Content != "" {
			um.Contidos = append(um.Contidos, log)
		}
	}
	return um, true
}

// grauConsecucion deriva 'Alto'|'Medio'|'Baixo' (mesmo vocabulario ca
// UnitReport.graoCumprimento, tao/frontend/src/types.ts) da proporción
// sesións reais/programadas — heurística explícita, non un xuízo da IA:
// ≥90% Alto, ≥60% Medio, resto Baixo. Unidade sen sesións programadas
// (dato incompleto na programación) cae en "" — nin a IA nin este código
// inventan un grao sen dato base.
func grauConsecucion(programadas, reais int) string {
	if programadas <= 0 {
		return ""
	}
	ratio := float64(reais) / float64(programadas)
	switch {
	case ratio >= 0.9:
		return "Alto"
	case ratio >= 0.6:
		return "Medio"
	default:
		return "Baixo"
	}
}
