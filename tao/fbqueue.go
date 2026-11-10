package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Offline queue for File Browser writes.
//
// The server can be a VPS reached over the internet, where a slow link or a brief
// outage is expected, not exceptional. Reads (fbRawGet, fbList) stay synchronous —
// the caller needs the data now and already has its own short retry — but writes
// (fbUpload, fbEnsureDir, fbDelete) go through this queue: a single attempt is made
// right away, and only if it fails for a TRANSIENT reason (no connection, timeout,
// 5xx — exactly what a congested/overloaded VPS produces) is the operation persisted
// to disk and handed to a background worker that retries it with exponential backoff
// until the server answers. A definite failure (bad session, 4xx) is returned to the
// caller immediately, since retrying it would never succeed.
//
// This keeps every existing call site (mensaxes.go, traballos.go, profesor.go,
// backup.go, admin_fb.go, report.go, practicas.go — all of which already funnel
// through fbUpload/fbEnsureDir/fbDelete) resilient without having to touch each one:
// the queue lives entirely behind those three functions.

const fbQueueFile = "fb-cola.json"

// fbQueueDataDir é o cartafol irmán onde se gardan os contidos grandes da cola, un
// ficheiro por operación; fbQueueInlineMax é o tamaño a partir do cal se saca do JSON.
const fbQueueDataDir = "fb-cola-datos"
const fbQueueInlineMax = 256 << 10 // 256 KiB

// fbOp is one pending write, persisted verbatim so it survives an app restart.
type fbOp struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "upload" | "ensureDir" | "delete"
	Path string `json:"path"`
	Data []byte `json:"data,omitempty"` // only for "upload", and only when small
	// DataFile: nome do ficheiro que garda o contido cando é grande (ver
	// fbQueueInlineMax). Vai á parte por unha razón práctica: a cola enteira
	// serialízase a JSON e escríbese en disco en CADA cambio, e un traballo de
	// 50 MiB dentro dese JSON (en base64, ~67 MiB) convertía cada actualización
	// da cola nun parón de segundos co mutex collido — a app quedaba "colgada"
	// xusto despois de subir. Co contido fóra, o JSON da cola é sempre pequeno.
	DataFile  string    `json:"dataFile,omitempty"`
	Label     string    `json:"label"` // short human description, for the UI
	CreatedAt time.Time `json:"createdAt"`
	Attempts  int       `json:"attempts"`
	NextTry   time.Time `json:"nextTry"`
	LastError string    `json:"lastError,omitempty"`
}

// FBQueueStatus is what the frontend reads (GetFBQueueStatus) and receives on the
// "fbcola" event to show a sync indicator ("3 cambios pendentes de sincronizar").
type FBQueueStatus struct {
	Pendentes  int    `json:"pendentes"`
	UltimoErro string `json:"ultimoErro,omitempty"`
}

// fbTransientError marks a recoverable failure (no connection, timeout, 5xx) as
// opposed to a definite one (expired session, bad request). Only transient failures
// are queued for retry.
type fbTransientError struct{ err error }

func (e *fbTransientError) Error() string { return e.err.Error() }
func (e *fbTransientError) Unwrap() error { return e.err }

func fbTransient(err error) error {
	if err == nil {
		return nil
	}
	return &fbTransientError{err}
}

func isFBTransient(err error) bool {
	var te *fbTransientError
	return errors.As(err, &te)
}

func (a *App) fbQueuePath() string {
	return filepath.Join(a.dataDir, fbQueueFile)
}

// fbQueueDataPath é onde vive o contido dunha operación grande.
func (a *App) fbQueueDataPath(nome string) string {
	return filepath.Join(a.dataDir, fbQueueDataDir, nome)
}

// fbOpData devolve o contido dunha operación, veña inline ou do seu ficheiro. O
// segundo valor é false se o contido se perdeu (ficheiro borrado a man, disco
// limpado): nese caso non hai nada que reintentar e a operación debe descartarse,
// non quedar na cola para sempre.
func (a *App) fbOpData(op fbOp) ([]byte, bool) {
	if op.DataFile == "" {
		return op.Data, true
	}
	datos, err := os.ReadFile(a.fbQueueDataPath(op.DataFile))
	if err != nil {
		return nil, false
	}
	return datos, true
}

// fbQueueInit loads any operations left pending from a previous run and starts the
// background worker. Called once from startup.
func (a *App) fbQueueInit() {
	a.fbQueueWake = make(chan struct{}, 1)
	if raw, err := os.ReadFile(a.fbQueuePath()); err == nil {
		var ops []fbOp
		if err := json.Unmarshal(raw, &ops); err == nil {
			// Operacións cuxo contido xa non está en disco non se poden reintentar:
			// quedarían na cola para sempre amosando "pendente de sincronizar".
			vivas := ops[:0]
			for _, op := range ops {
				if _, ok := a.fbOpData(op); ok {
					vivas = append(vivas, op)
				} else {
					log.Printf("[fbcola] descártase %q ao arrancar: perdeuse o contido", op.Label)
				}
			}
			ops = vivas
			a.fbQueueMu.Lock()
			a.fbQueue = ops
			a.fbQueueMu.Unlock()
			if len(ops) > 0 {
				log.Printf("[fbcola] %d operación(s) pendentes dunha sesión anterior, retomando", len(ops))
			}
		}
	}
	go a.fbQueueWorker()
}

// fbEnqueue persists a write and wakes the worker. content is copied since callers
// may reuse or mutate their buffer afterwards.
func (a *App) fbEnqueue(kind, path string, content []byte, label string) {
	var data []byte
	if content != nil {
		data = append([]byte(nil), content...)
	}
	op := fbOp{
		ID:        fmt.Sprintf("%d-%s", time.Now().UnixNano(), kind),
		Kind:      kind,
		Path:      path,
		Data:      data,
		Label:     label,
		CreatedAt: time.Now(),
		NextTry:   time.Now(),
	}
	// Contido grande: ao seu propio ficheiro, fóra do JSON da cola (ver DataFile).
	// Se non se puidese escribir, queda inline: mellor unha cola pesada que perder
	// o traballo do alumno.
	if len(op.Data) > fbQueueInlineMax {
		nome := op.ID + ".bin"
		if err := os.MkdirAll(a.fbQueueDataPath(""), 0o700); err == nil {
			if err := os.WriteFile(a.fbQueueDataPath(nome), op.Data, 0o600); err == nil {
				op.DataFile, op.Data = nome, nil
			} else {
				log.Printf("[fbcola] non se puido gardar o contido de %q á parte: %v", label, err)
			}
		}
	}
	a.fbQueueMu.Lock()
	a.fbQueue = append(a.fbQueue, op)
	a.fbQueuePersistLocked()
	a.fbQueueMu.Unlock()

	select {
	case a.fbQueueWake <- struct{}{}:
	default:
	}
	a.fbQueueNotify()
}

// fbQueueWorker processes queued operations in FIFO order (so e.g. an "ensureDir"
// enqueued before the "upload" into it always runs first), sleeping until either the
// next retry is due or a new operation wakes it up.
func (a *App) fbQueueWorker() {
	for {
		a.fbQueueMu.Lock()
		var next *fbOp
		now := time.Now()
		wait := time.Hour
		for i := range a.fbQueue {
			op := &a.fbQueue[i]
			if op.NextTry.After(now) {
				if d := op.NextTry.Sub(now); d < wait {
					wait = d
				}
				continue
			}
			next = op
			break
		}
		var opCopy fbOp
		if next != nil {
			opCopy = *next
		}
		a.fbQueueMu.Unlock()

		if next == nil {
			select {
			case <-a.fbQueueWake:
			case <-time.After(wait):
			}
			continue
		}

		err := a.fbExecOp(opCopy)

		a.fbQueueMu.Lock()
		if err == nil {
			a.fbQueueRemoveLocked(opCopy.ID)
			log.Printf("[fbcola] sincronizado: %s", opCopy.Label)
		} else {
			for i := range a.fbQueue {
				if a.fbQueue[i].ID == opCopy.ID {
					a.fbQueue[i].Attempts++
					a.fbQueue[i].LastError = err.Error()
					a.fbQueue[i].NextTry = time.Now().Add(fbBackoff(a.fbQueue[i].Attempts))
					break
				}
			}
			log.Printf("[fbcola] fallo ao sincronizar %q (intento %d): %v", opCopy.Label, opCopy.Attempts+1, err)
		}
		a.fbQueuePersistLocked()
		a.fbQueueMu.Unlock()
		a.fbQueueNotify()
	}
}

// fbExecOp replays a queued operation using the app's *current* File Browser session
// (a.fbToken/a.fbURL), not the one captured when it was enqueued — so a re-login that
// refreshes an expired token is picked up on the next retry.
func (a *App) fbExecOp(op fbOp) error {
	if a.fbToken == "" || a.fbURL == "" {
		return fmt.Errorf("sen sesión activa no servidor de ficheiros")
	}
	switch op.Kind {
	case "upload":
		datos, ok := a.fbOpData(op)
		if !ok {
			log.Printf("[fbcola] descártase %q: perdeuse o contido gardado en disco", op.Label)
			return nil // nada que reintentar; sácase da cola
		}
		// A causa máis típica de que unha subida acabe aquí é un tempo esgotado
		// mentres se agardaba a resposta, co ficheiro XA enteiro no servidor.
		// Reenvialo sería mover os mesmos megas outra vez para nada, así que
		// primeiro se mira se xa está alí co mesmo tamaño.
		if a.fbXaSubido(op.Path, len(datos)) {
			log.Printf("[fbcola] %q xa estaba no servidor: non se reenvía", op.Label)
			return nil
		}
		return a.fbUploadDirect(op.Path, datos)
	case "ensureDir":
		return a.fbEnsureDirDirect(op.Path)
	case "delete":
		return a.fbDeleteDirect(op.Path)
	default:
		return fmt.Errorf("tipo de operación descoñecido na cola: %s", op.Kind)
	}
}

// fbBackoff grows 5s → 10s → 20s… capped at 5 minutes, so a real outage is retried
// often enough to recover quickly but without hammering the server.
func fbBackoff(attempts int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < attempts && d < 5*time.Minute; i++ {
		d *= 2
	}
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// fbQueueRemoveLocked deletes an operation by id. Caller must hold fbQueueMu.
func (a *App) fbQueueRemoveLocked(id string) {
	out := a.fbQueue[:0]
	for _, op := range a.fbQueue {
		if op.ID != id {
			out = append(out, op)
			continue
		}
		if op.DataFile != "" {
			os.Remove(a.fbQueueDataPath(op.DataFile))
		}
	}
	a.fbQueue = out
}

// fbXaSubido di se remotePath xa está no servidor co tamaño esperado. Úsase antes de
// reintentar unha subida encolada (ver fbExecOp). Ante a dúbida (o servidor non
// responde, non se pode listar) devolve false: mellor reenviar de máis que dar por
// boa unha subida que quizais non chegou.
func (a *App) fbXaSubido(remotePath string, tamano int) bool {
	corte := strings.LastIndexByte(remotePath, '/')
	if corte <= 0 {
		return false
	}
	dir, nome := remotePath[:corte], remotePath[corte+1:]
	items, err := a.fbList(dir)
	if err != nil {
		return false
	}
	for _, it := range items {
		if !it.IsDir && it.Name == nome {
			return it.Size == int64(tamano)
		}
	}
	return false
}

// fbQueuePersistLocked writes the queue to disk. Caller must hold fbQueueMu.
func (a *App) fbQueuePersistLocked() {
	raw, err := json.MarshalIndent(a.fbQueue, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(a.fbQueuePath(), raw, 0o600); err != nil {
		log.Printf("[fbcola] non se puido gardar a cola en disco: %v", err)
	}
}

// fbQueueNotify emits the current status to the frontend so it can show a sync
// indicator without polling.
func (a *App) fbQueueNotify() {
	emitirEvento("fbcola", a.FBQueueStatus())
}

// FBQueueStatus devolve o estado actual da cola de sincronización pendente co
// servidor de ficheiros (chamadas Wails desde o frontend son sempre asíncronas en JS,
// pero este método en si é instantáneo: só le estado en memoria).
func (a *App) FBQueueStatus() FBQueueStatus {
	a.fbQueueMu.Lock()
	defer a.fbQueueMu.Unlock()
	st := FBQueueStatus{Pendentes: len(a.fbQueue)}
	for _, op := range a.fbQueue {
		if op.LastError != "" {
			st.UltimoErro = op.LastError
		}
	}
	return st
}

// ---- Public write API: single attempt, then queue-and-forget on transient failure --

// fbUpload writes content to remotePath, overwriting any existing file. On a
// transient failure it is queued and retried in the background; the call still
// returns success, since the write WILL happen once the server is reachable again.
func (a *App) fbUpload(remotePath string, content []byte) error {
	err := a.fbUploadDirect(remotePath, content)
	if err == nil {
		return nil
	}
	if isFBTransient(err) {
		log.Printf("[fbcola] servidor non dispoñible, encolando gardado de %q: %v", remotePath, err)
		a.fbEnqueue("upload", remotePath, content, "gardar "+remotePath)
		return nil
	}
	return err
}

// fbEnsureDir creates dir on the server, queueing the creation on a transient
// failure. See fbUpload.
func (a *App) fbEnsureDir(dir string) error {
	err := a.fbEnsureDirDirect(dir)
	if err == nil {
		return nil
	}
	if isFBTransient(err) {
		log.Printf("[fbcola] servidor non dispoñible, encolando creación de %q: %v", dir, err)
		a.fbEnqueue("ensureDir", dir, nil, "crear cartafol "+dir)
		return nil
	}
	return err
}

// fbDelete removes a file on the server, queueing the deletion on a transient
// failure. See fbUpload.
func (a *App) fbDelete(remotePath string) error {
	err := a.fbDeleteDirect(remotePath)
	if err == nil {
		return nil
	}
	if isFBTransient(err) {
		log.Printf("[fbcola] servidor non dispoñible, encolando borrado de %q: %v", remotePath, err)
		a.fbEnqueue("delete", remotePath, nil, "borrar "+remotePath)
		return nil
	}
	return err
}
