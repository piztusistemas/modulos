package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Lectura, gardado e exportación (PDF/ODT/DOCX), dende o propio Tao, dos
// informes que xera Malta (dixitalizacion/informes-malta/<materiaId>/…, ver
// docs/api-tao.md §12). Tao le/escribe directamente con fbDownload/fbUpload
// coa súa propia sesión FB — Malta nunca toca File Browser, só devolve o
// resultado por POST /api/v1/malta/resultado (malta_resultado.go), que
// chama a gardarInformeMaltaArquivo. Consumido por ReportsModal
// (tao/frontend/src/App.tsx).

const informesMaltaDir = "informes-malta"

// identificadorInformeValido esixe o mesmo criterio ca SaveMateria
// (profesor.go: "strings.ContainsAny(m.ID, \"/\\\\\")") para calquera valor
// que vaia formar UN segmento propio dunha ruta de File Browser aquí
// (materiaID e reportID forman cadanseu segmento en
// ".../informes-malta/<materiaID>/<reportID>.json", a diferenza do ID de
// materia en SaveMateria, que só forma parte dun nome de ficheiro) —
// ademais de rexeitar "/"/"\\", rexeita tamén ".."/"." a secas (un
// segmento de ruta enteiro, non só un substring) para que non poida
// ESCAPAR de informes-malta/ aínda sen ningunha barra. Chamado tanto no
// extremo HTTP (handleMaltaResultado, cuxo corpo non se pode asumir
// confiable) coma nos bindings que xa recibe o propio frontend de Tao
// (ListMaltaReports/GetMaltaReport/Export*), coma segunda liña de defensa.
func identificadorInformeValido(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return !strings.ContainsAny(id, "/\\")
}

// MaltaUnidade/MaltaInforme/MaltaIndiceEntrada espellan (só os campos que
// Tao precisa mostrar/exportar) InformeUnidade/ResultadoTarefaMalta de
// tao/modulos/malta/{informes,tarefa}.go — dous binarios distintos, sen
// paquete Go compartido (mesmo motivo có contexto de lanzamento, ver
// modulos_launch_tao.go): non xustifica un módulo Go novo só para isto.
type MaltaUnidade struct {
	UnitID             int    `json:"unitId"`
	Titulo             string `json:"titulo"`
	SesionsProgramadas int    `json:"sesionsProgramadas"`
	SesionsReais       int    `json:"sesionsReais"`
	DiferenciaDias     int    `json:"diferenciaDias"`
	PrimeiroDia        string `json:"primeiroDia,omitempty"`
	UltimoDia          string `json:"ultimoDia,omitempty"`
	GrauConsecucion    string `json:"grauConsecucion"`
	Resumo             string `json:"resumo"`
	Proposta           string `json:"proposta,omitempty"`
	// Só no informe mensual (ver tao/modulos/malta/tarefa.go).
	SesionsMes        int    `json:"sesionsMes,omitempty"`
	SesionsAcumuladas int    `json:"sesionsAcumuladas,omitempty"`
	Porcentaxe        int    `json:"porcentaxe,omitempty"`
	Comparacion       string `json:"comparacion,omitempty"`
}

// metricasTextoMalta é a liña de datos de cada unidade no PDF/ODT/DOCX: no
// mensual, sesións do mes e porcentaxe da unidade; no de unidade, o
// comparado programado/real de sempre.
func metricasTextoMalta(tipo string, u MaltaUnidade) string {
	if tipo == "mensual" {
		return fmt.Sprintf("Sesións no mes: %d · Sesións acumuladas: %d de %d programadas · Unidade traballada: %d%%",
			u.SesionsMes, u.SesionsAcumuladas, u.SesionsProgramadas, u.Porcentaxe)
	}
	return fmt.Sprintf("Sesións programadas: %d · Sesións reais: %d · Diferenza: %+d · Grao de consecución: %s",
		u.SesionsProgramadas, u.SesionsReais, u.DiferenciaDias, u.GrauConsecucion)
}

type MaltaInforme struct {
	Tipo                  string         `json:"tipo"` // "unidade" | "mensual"
	MateriaID             string         `json:"materiaId"`
	MateriaNome           string         `json:"materiaNome"`
	ReportID              string         `json:"reportId"`
	RangoFrom             string         `json:"rangoFrom,omitempty"`
	RangoTo               string         `json:"rangoTo,omitempty"`
	UnitIDObxectivo       int            `json:"unitIdObxectivo,omitempty"`
	ProvedorIA            string         `json:"provedorIA"`
	ModeloIA              string         `json:"modeloIA"`
	PeticionPersonalizada string         `json:"peticionPersonalizada,omitempty"`
	XeradoEn              string         `json:"xeradoEn"`
	Unidades              []MaltaUnidade `json:"unidades"`
	ComentarioGlobal      string         `json:"comentarioGlobal,omitempty"`
}

type MaltaIndiceEntrada struct {
	ReportID        string `json:"reportId"`
	Tipo            string `json:"tipo"`
	RangoFrom       string `json:"rangoFrom,omitempty"`
	RangoTo         string `json:"rangoTo,omitempty"`
	UnitIDObxectivo int    `json:"unitIdObxectivo,omitempty"`
	Unidades        []int  `json:"unidades"`
	XeradoEn        string `json:"xeradoEn"`
}

// ListMaltaReports devolve o índice de informes xerados por Malta para
// materiaID (lista baleira, sen erro, se aínda non hai ningún ou Malta
// nunca se usou con esta materia).
func (a *App) ListMaltaReports(materiaID string) ([]MaltaIndiceEntrada, error) {
	if !identificadorInformeValido(materiaID) {
		return nil, fmt.Errorf("identificador de materia non válido")
	}
	conteudo, err := a.fbDownload(sharedRoot + "/" + informesMaltaDir + "/" + materiaID + "/index.json")
	if err != nil || conteudo == "" {
		return []MaltaIndiceEntrada{}, nil
	}
	var entradas []MaltaIndiceEntrada
	if err := json.Unmarshal([]byte(conteudo), &entradas); err != nil {
		return nil, fmt.Errorf("índice de informes de Malta non válido: %w", err)
	}
	return entradas, nil
}

// GetMaltaReport devolve un informe concreto xa gardado por Malta.
func (a *App) GetMaltaReport(materiaID, reportID string) (MaltaInforme, error) {
	if !identificadorInformeValido(materiaID) || !identificadorInformeValido(reportID) {
		return MaltaInforme{}, fmt.Errorf("identificador de informe non válido")
	}
	conteudo, err := a.fbDownload(sharedRoot + "/" + informesMaltaDir + "/" + materiaID + "/" + reportID + ".json")
	if err != nil || conteudo == "" {
		return MaltaInforme{}, fmt.Errorf("non se atopou ese informe")
	}
	var informe MaltaInforme
	if err := json.Unmarshal([]byte(conteudo), &informe); err != nil {
		return MaltaInforme{}, fmt.Errorf("informe de Malta non válido: %w", err)
	}
	return informe, nil
}

// gardarInformeMaltaArquivo garda `informe` en
// dixitalizacion/informes-malta/<materiaId>/<reportId>.json e actualiza o
// índice dese cartafol (index.json, máis recente primeiro) — chamado por
// handleMaltaResultado (malta_resultado.go) cando Malta devolve un informe
// xerado con éxito. Usa a sesión FB que xa ten Tao; Malta nunca chega a
// tocar File Browser. informe.MateriaID/ReportID veñen do corpo POST de
// /api/v1/malta/resultado — non se pode asumir que veñan sempre do propio
// Malta (ver identificadorInformeValido), así que valídanse aquí antes de
// compoñelos nunha ruta de File Browser.
func (a *App) gardarInformeMaltaArquivo(informe MaltaInforme) error {
	if !identificadorInformeValido(informe.MateriaID) || !identificadorInformeValido(informe.ReportID) {
		return fmt.Errorf("identificador de informe non válido")
	}
	dir := sharedRoot + "/" + informesMaltaDir + "/" + informe.MateriaID
	if err := a.fbEnsureDir(sharedRoot); err != nil {
		return err
	}
	if err := a.fbEnsureDir(sharedRoot + "/" + informesMaltaDir); err != nil {
		return err
	}
	if err := a.fbEnsureDir(dir); err != nil {
		return err
	}

	dados, err := json.MarshalIndent(informe, "", "  ")
	if err != nil {
		return err
	}
	if err := a.fbUpload(dir+"/"+informe.ReportID+".json", dados); err != nil {
		return err
	}
	return a.actualizarIndiceMalta(dir, informe)
}

// actualizarIndiceMalta le index.json (se xa existe) dentro de dir, engade
// ou substitúe a entrada de `informe` e gárdao de novo, máis recente
// primeiro — porte directo da lóxica que antes vivía en
// tao/modulos/malta/informes.go (actualizarIndice), agora aquí porque é Tao
// quen escribe en File Browser.
func (a *App) actualizarIndiceMalta(dir string, informe MaltaInforme) error {
	var entradas []MaltaIndiceEntrada
	if conteudo, err := a.fbDownload(dir + "/index.json"); err == nil && conteudo != "" {
		_ = json.Unmarshal([]byte(conteudo), &entradas)
	}

	var unidades []int
	for _, u := range informe.Unidades {
		unidades = append(unidades, u.UnitID)
	}
	nova := MaltaIndiceEntrada{
		ReportID: informe.ReportID, Tipo: informe.Tipo,
		RangoFrom: informe.RangoFrom, RangoTo: informe.RangoTo,
		UnitIDObxectivo: informe.UnitIDObxectivo,
		Unidades:        unidades, XeradoEn: informe.XeradoEn,
	}
	filtradas := entradas[:0]
	for _, e := range entradas {
		if e.ReportID != nova.ReportID {
			filtradas = append(filtradas, e)
		}
	}
	entradas = append(filtradas, nova)
	sort.Slice(entradas, func(i, j int) bool { return entradas[i].XeradoEn > entradas[j].XeradoEn })

	dados, err := json.MarshalIndent(entradas, "", "  ")
	if err != nil {
		return err
	}
	return a.fbUpload(dir+"/index.json", dados)
}

// ── Exportación PDF (fpdf, patrón pancho/informe.go — sen dependencias) ──

// ExportMaltaReportPDF amosa "Gardar como" e escribe o informe en PDF.
func (a *App) ExportMaltaReportPDF(materiaID, reportID string) (string, error) {
	informe, err := a.GetMaltaReport(materiaID, reportID)
	if err != nil {
		return "", err
	}
	path, err := gardarFicheiro("Gardar informe (PDF)", nomeInformeMalta(informe, "pdf"), application.FileFilter{DisplayName: "PDF (*.pdf)", Pattern: "*.pdf"})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, construirPDFMalta(informe), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func nomeInformeMalta(informe MaltaInforme, ext string) string {
	return fmt.Sprintf("informe-malta-%s-%s.%s", informe.MateriaID, informe.ReportID, ext)
}

// toLatin1Malta é a mesma conversión ca pancho/informe.go:toLatin1 (fontes
// básicas de fpdf só falan Latin-1/cp1252; áéíóúñü do galego caben nel).
func toLatin1Malta(s string) string {
	runes := []rune(s)
	buf := make([]byte, len(runes))
	for i, r := range runes {
		if r > 255 {
			r = '?'
		}
		buf[i] = byte(r)
	}
	return string(buf)
}

func construirPDFMalta(informe MaltaInforme) []byte {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(toLatin1Malta("Informe - "+informe.MateriaNome), false)
	pdf.SetMargins(18, 18, 18)
	pdf.AddPage()

	novaPaxinaSePreciso := func(alto float64) {
		if pdf.GetY()+alto > 275 {
			pdf.AddPage()
		}
	}

	pdf.SetFont("Helvetica", "B", 16)
	pdf.MultiCell(0, 8, toLatin1Malta(informe.MateriaNome), "", "L", false)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(0, 6, toLatin1Malta(fmt.Sprintf("Rango: %s a %s", informe.RangoFrom, informe.RangoTo)), "", 1, "L", false, 0, "")
	if informe.XeradoEn != "" {
		if t, err := time.Parse(time.RFC3339, informe.XeradoEn); err == nil {
			pdf.CellFormat(0, 6, toLatin1Malta("Xerado: "+t.Local().Format("02/01/2006 15:04")), "", 1, "L", false, 0, "")
		}
	}
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	if informe.ComentarioGlobal != "" {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.MultiCell(0, 5, toLatin1Malta(informe.ComentarioGlobal), "", "L", false)
		pdf.Ln(3)
	}

	for _, u := range informe.Unidades {
		novaPaxinaSePreciso(30)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.MultiCell(0, 7, toLatin1Malta(fmt.Sprintf("UD %d - %s", u.UnitID, u.Titulo)), "", "L", false)

		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(90, 90, 90)
		pdf.MultiCell(0, 5, toLatin1Malta(metricasTextoMalta(informe.Tipo, u)), "", "L", false)
		pdf.SetTextColor(0, 0, 0)

		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(0, 5, toLatin1Malta(u.Resumo), "", "L", false)
		if u.Comparacion != "" {
			pdf.Ln(2)
			pdf.SetFont("Helvetica", "B", 9)
			pdf.CellFormat(0, 5, toLatin1Malta("Comparación coa programación de aula:"), "", 1, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 10)
			pdf.MultiCell(0, 5, toLatin1Malta(u.Comparacion), "", "L", false)
		}
		if u.Proposta != "" {
			pdf.Ln(2)
			pdf.SetFont("Helvetica", "B", 9)
			pdf.CellFormat(0, 5, toLatin1Malta("Proposta de mellora:"), "", 1, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 10)
			pdf.MultiCell(0, 5, toLatin1Malta(u.Proposta), "", "L", false)
		}
		pdf.Ln(4)
	}

	var buf bytes.Buffer
	_ = pdf.Output(&buf)
	return buf.Bytes()
}

// ── Exportación ODT/DOCX (Pandoc externo, patrón yang/docdoc.go) ─────────
//
// A diferenza de yang/docdoc.go, non se aplica ningunha plantilla de
// estilos propia (--reference-doc): son os estilos por defecto de Pandoc.
// Simplificación deliberada — engadir unha plantilla de deseño propia para
// Malta é traballo de deseño á parte, non un requisito funcional.

func markdownInformeMalta(informe MaltaInforme) string {
	var sb bytes.Buffer
	fmt.Fprintf(&sb, "# %s\n\n", informe.MateriaNome)
	fmt.Fprintf(&sb, "**Rango:** %s a %s\n\n", informe.RangoFrom, informe.RangoTo)
	if informe.ComentarioGlobal != "" {
		fmt.Fprintf(&sb, "*%s*\n\n", informe.ComentarioGlobal)
	}
	for _, u := range informe.Unidades {
		fmt.Fprintf(&sb, "## UD %d — %s\n\n", u.UnitID, u.Titulo)
		fmt.Fprintf(&sb, "%s\n\n", metricasTextoMalta(informe.Tipo, u))
		fmt.Fprintf(&sb, "%s\n\n", u.Resumo)
		if u.Comparacion != "" {
			fmt.Fprintf(&sb, "**Comparación coa programación de aula:** %s\n\n", u.Comparacion)
		}
		if u.Proposta != "" {
			fmt.Fprintf(&sb, "**Proposta de mellora:** %s\n\n", u.Proposta)
		}
	}
	return sb.String()
}

// exportarInformeMaltaViaPandoc é o núcleo compartido por
// ExportMaltaReportODT/ExportMaltaReportDOCX.
func exportarInformeMaltaViaPandoc(informe MaltaInforme, path, format string) error {
	if _, err := exec.LookPath("pandoc"); err != nil {
		return fmt.Errorf("non se atopou Pandoc. Instálao con 'sudo apt install pandoc'")
	}
	tmpDir, err := os.MkdirTemp("", "malta-"+format+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	mdPath := tmpDir + "/informe.md"
	if err := os.WriteFile(mdPath, []byte(markdownInformeMalta(informe)), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("pandoc", mdPath, "-f", "markdown", "-o", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pandoc fallou: %w: %s", err, out)
	}
	return nil
}

// ExportMaltaReportODT amosa "Gardar como" e escribe o informe en
// OpenDocument (.odt) vía Pandoc.
func (a *App) ExportMaltaReportODT(materiaID, reportID string) (string, error) {
	informe, err := a.GetMaltaReport(materiaID, reportID)
	if err != nil {
		return "", err
	}
	path, err := gardarFicheiro("Gardar informe (OpenDocument)", nomeInformeMalta(informe, "odt"), application.FileFilter{DisplayName: "OpenDocument (*.odt)", Pattern: "*.odt"})
	if err != nil || path == "" {
		return "", err
	}
	if err := exportarInformeMaltaViaPandoc(informe, path, "odt"); err != nil {
		return "", err
	}
	return path, nil
}

// ExportMaltaReportDOCX amosa "Gardar como" e escribe o informe en Word
// (.docx) vía Pandoc.
func (a *App) ExportMaltaReportDOCX(materiaID, reportID string) (string, error) {
	informe, err := a.GetMaltaReport(materiaID, reportID)
	if err != nil {
		return "", err
	}
	path, err := gardarFicheiro("Gardar informe (Word)", nomeInformeMalta(informe, "docx"), application.FileFilter{DisplayName: "Word (*.docx)", Pattern: "*.docx"})
	if err != nil || path == "" {
		return "", err
	}
	if err := exportarInformeMaltaViaPandoc(informe, path, "docx"); err != nil {
		return "", err
	}
	return path, nil
}
