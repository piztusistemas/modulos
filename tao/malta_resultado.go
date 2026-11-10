package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// handleMaltaResultado atende POST /api/v1/malta/resultado — o outro
// extremo do pipe Tao→Malta (malta_launch.go, docs/api-tao.md §12): recibe
// o informe (ou o erro) que Malta acaba de xerar de forma autónoma, gárdao
// en File Browser e emite un evento Wails para que ReportsModal (App.tsx),
// se está aberto nesa materia, o reflicta ao instante.

// peticionResultadoMalta é o corpo desta ruta — espello de
// ResultadoTarefaMalta (tao/modulos/malta/tarefa.go).
type peticionResultadoMalta struct {
	Tipo                  string         `json:"tipo"`
	MateriaID             string         `json:"materiaId"`
	MateriaNome           string         `json:"materiaNome"`
	RangoFrom             string         `json:"rangoFrom,omitempty"`
	RangoTo               string         `json:"rangoTo,omitempty"`
	UnitIDObxectivo       int            `json:"unitIdObxectivo,omitempty"`
	ProvedorIA            string         `json:"provedorIA,omitempty"`
	ModeloIA              string         `json:"modeloIA,omitempty"`
	PeticionPersonalizada string         `json:"peticionPersonalizada,omitempty"`
	Unidades              []MaltaUnidade `json:"unidades,omitempty"`
	ComentarioGlobal      string         `json:"comentarioGlobal,omitempty"`
	Erro                  string         `json:"erro,omitempty"`
}

// maltaInformeEvent é o evento Wails "tao:malta-informe" — mesma forma cá
// petición, máis OK/ReportID (o ID definitivo que lle deu Tao ao gardalo).
type maltaInformeEvent struct {
	OK               bool           `json:"ok"`
	Erro             string         `json:"erro,omitempty"`
	Tipo             string         `json:"tipo"`
	MateriaID        string         `json:"materiaId"`
	MateriaNome      string         `json:"materiaNome"`
	ReportID         string         `json:"reportId,omitempty"`
	RangoFrom        string         `json:"rangoFrom,omitempty"`
	RangoTo          string         `json:"rangoTo,omitempty"`
	UnitIDObxectivo  int            `json:"unitIdObxectivo,omitempty"`
	Unidades         []MaltaUnidade `json:"unidades,omitempty"`
	ComentarioGlobal string         `json:"comentarioGlobal,omitempty"`
}

func (s *apiServidor) handleMaltaResultado(w http.ResponseWriter, r *http.Request) {
	var peticion peticionResultadoMalta
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&peticion); err != nil {
		escribirErroAPI(w, http.StatusBadRequest, "corpo_invalido", "corpo JSON non válido: "+err.Error())
		return
	}
	s.app.auditarAPI(r.Method, r.URL.Path)

	if peticion.Erro != "" {
		emitirEvento("tao:malta-informe", maltaInformeEvent{
			OK: false, Erro: peticion.Erro, Tipo: peticion.Tipo,
			MateriaID: peticion.MateriaID, MateriaNome: peticion.MateriaNome,
			RangoFrom: peticion.RangoFrom, RangoTo: peticion.RangoTo,
			UnitIDObxectivo: peticion.UnitIDObxectivo,
		})
		escribirJSONAPI(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	informe := MaltaInforme{
		Tipo: peticion.Tipo, MateriaID: peticion.MateriaID, MateriaNome: peticion.MateriaNome,
		ReportID:              time.Now().Format("20060102-150405"),
		RangoFrom:             peticion.RangoFrom,
		RangoTo:               peticion.RangoTo,
		UnitIDObxectivo:       peticion.UnitIDObxectivo,
		ProvedorIA:            peticion.ProvedorIA,
		ModeloIA:              peticion.ModeloIA,
		PeticionPersonalizada: peticion.PeticionPersonalizada,
		XeradoEn:              time.Now().UTC().Format(time.RFC3339),
		Unidades:              peticion.Unidades,
		ComentarioGlobal:      peticion.ComentarioGlobal,
	}
	// Best-effort a propósito: se falla o gardado en File Browser (servidor
	// caído, etc.), o profesorado non ten por que perder o contido que a IA
	// xa xerou — segue chegando por evento e amosándose na interface, só que
	// non quedará arquivado ata a próxima xeración que si grave ben.
	if err := s.app.gardarInformeMaltaArquivo(informe); err != nil {
		log.Printf("[malta] non se puido gardar o informe en File Browser: %v", err)
	}

	emitirEvento("tao:malta-informe", maltaInformeEvent{
		OK: true, Tipo: informe.Tipo, MateriaID: informe.MateriaID, MateriaNome: informe.MateriaNome,
		ReportID: informe.ReportID, RangoFrom: informe.RangoFrom, RangoTo: informe.RangoTo,
		UnitIDObxectivo: informe.UnitIDObxectivo, Unidades: informe.Unidades,
		ComentarioGlobal: informe.ComentarioGlobal,
	})
	escribirJSONAPI(w, http.StatusOK, map[string]string{"status": "ok"})
}
