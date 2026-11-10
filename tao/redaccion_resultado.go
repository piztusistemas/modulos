package main

import (
	"encoding/json"
	"net/http"
)

// handleMaltaResultadoAsinado atende POST /api/v1/malta/resultado-asinado —
// o outro extremo do pipe Tao→Malta para o informe asinado
// (redaccion_launch.go): recibe o texto redactado (ou o erro) e emite un
// evento Wails para que ReportBuilder.tsx, se segue aberto, o reciba. A
// diferenza de handleMaltaResultado (malta_resultado.go), aquí NON se garda
// nada en File Browser: é un borrador de usar-e-tirar, non un informe
// arquivado — o que se garda é o PDF xa asinado, en ExportSignedReport.

// peticionResultadoRedaccion é o corpo desta ruta — espello de
// ResultadoTarefaRedaccion (tao/modulos/malta/redaccion.go).
type peticionResultadoRedaccion struct {
	RequestID  string `json:"requestId"`
	ProvedorIA string `json:"provedorIA,omitempty"`
	ModeloIA   string `json:"modeloIA,omitempty"`
	Texto      string `json:"texto,omitempty"`
	Erro       string `json:"erro,omitempty"`
}

// maltaRedaccionEvent é o evento Wails "tao:malta-redaccion".
type maltaRedaccionEvent struct {
	RequestID  string `json:"requestId"`
	OK         bool   `json:"ok"`
	Erro       string `json:"erro,omitempty"`
	Texto      string `json:"texto,omitempty"`
	ProvedorIA string `json:"provedorIA,omitempty"`
	ModeloIA   string `json:"modeloIA,omitempty"`
}

func (s *apiServidor) handleMaltaResultadoAsinado(w http.ResponseWriter, r *http.Request) {
	var peticion peticionResultadoRedaccion
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&peticion); err != nil {
		escribirErroAPI(w, http.StatusBadRequest, "corpo_invalido", "corpo JSON non válido: "+err.Error())
		return
	}
	s.app.auditarAPI(r.Method, r.URL.Path)

	emitirEvento("tao:malta-redaccion", maltaRedaccionEvent{
		RequestID:  peticion.RequestID,
		OK:         peticion.Erro == "",
		Erro:       peticion.Erro,
		Texto:      peticion.Texto,
		ProvedorIA: peticion.ProvedorIA,
		ModeloIA:   peticion.ModeloIA,
	})
	escribirJSONAPI(w, http.StatusOK, map[string]string{"status": "ok"})
}
