package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const nomeFicheiroAuditoriaAPI = "auditoria_api.jsonl"

// auditarAPI appends one JSONL line per write (POST/DELETE) through the
// local API. Best-effort: a full disk must never block a legitimate
// materia write. No caller/module identity is recorded — v1 has no such
// concept, see api.go.
func (a *App) auditarAPI(metodo, ruta string) {
	a.auditarAPITarefa(metodo, ruta, "")
}

// auditarAPITarefa is auditarAPI plus an optional username — used only by
// handleSaveTarefa (docs/api-tao.md §11.7) to record WHICH student a
// POST /api/v1/tarefa was for, without a separate audit store: username
// stays "" (and the line looks exactly like before) for every other write.
func (a *App) auditarAPITarefa(metodo, ruta, username string) {
	f, err := os.OpenFile(filepath.Join(a.dataDir, nomeFicheiroAuditoriaAPI),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if username == "" {
		fmt.Fprintf(f, "{\"ts\":%q,\"metodo\":%q,\"ruta\":%q}\n", time.Now().Format(time.RFC3339), metodo, ruta)
		return
	}
	fmt.Fprintf(f, "{\"ts\":%q,\"metodo\":%q,\"ruta\":%q,\"username\":%q}\n",
		time.Now().Format(time.RFC3339), metodo, ruta, username)
}
