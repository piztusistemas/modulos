package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Messaging.
//
// There is no database and no real-time backend: like everything else, a conversation
// is a set of files on the File Browser server. Each message is its own JSON file inside
// a per-thread folder (append-only: a fresh file per message, never overwritten, so a
// send can never clobber another). A thread is read by listing its folder and ordering
// the files by the TS field. Attachments live next to the message, under
// <threadDir>/adxuntos/<msgID>/.
//
// Threads by channel:
//   - professor ↔ student (per materia):  users/<student>/mensaxes/<materiaID>/
//   - group wall (per group):             dixitalizacion/mensaxes/mural/<group>/
//   - student 1:1 (grupo ou materia):     dixitalizacion/mensaxes/1a1/<a>__<b>/
//
// The professor↔student folder lives in the student's own space (the professor can
// reach it with their broad account, the student with their own). The group channels
// live in the shared folder, which every account can read and write (the student's own
// session already reads dixitalizacion/ for the materias and roster, so its File Browser
// scope covers it).

const (
	// fbMessagesRoot is the subfolder of each student's folder holding the professor↔
	// student threads, one per materia (users/<login>/mensaxes/<materiaID>).
	fbMessagesRoot = "mensaxes"
	// maxMessageLen caps a single message body so a stray paste cannot fill the disk.
	maxMessageLen = 4000
)

// Message is one entry in a conversation. Each is stored as its own JSON file; a thread
// is its folder's files sorted by TS.
type Message struct {
	ID         string `json:"id"`
	MateriaID  string `json:"materiaId,omitempty"` // professor↔student channel
	Group      string `json:"group,omitempty"`     // wall / 1:1 channel
	From       string `json:"from"`                // "profesor" or "alumno": which side sent it
	Author     string `json:"author"`              // sender's File Browser login
	AuthorName string `json:"authorName"`          // sender's display name (may be empty)
	Text       string `json:"text"`
	TS         int64  `json:"ts"`                  // send time, unix milliseconds
	Broadcast  bool   `json:"broadcast,omitempty"` // true for a group-wide notice

	// Attachments are the files sent with the message; AttachBase is the thread folder,
	// so a reader can fetch them from AttachBase/adxuntos/<id>/<name>.
	Attachments []WorkFile `json:"attachments,omitempty"`
	AttachBase  string     `json:"attachBase,omitempty"`
}

// ---- Path building and validation -----------------------------------------------

// messagesDir returns the professor↔student thread folder for a materia.
func messagesDir(login, materiaID string) string {
	return fbUsersRoot + "/" + login + "/" + fbMessagesRoot + "/" + materiaID
}

// wallDir returns the shared group-wall folder.
func wallDir(group string) string {
	return sharedRoot + "/" + fbMessagesRoot + "/mural/" + group
}

// peerDir returns the shared 1:1 folder for two students (order-independent).
//
// A carpeta NON colga do grupo: dúas persoas teñen un único fío, compartan grupo ou
// só materia (un bac1a e un bac1b da mesma Filosofía). Os logins normalízanse a
// minúsculas para que ambos os lados calculen sempre a mesma ruta.
func peerDir(a, b string) string {
	pair := []string{strings.ToLower(a), strings.ToLower(b)}
	sort.Strings(pair)
	return sharedRoot + "/" + fbMessagesRoot + "/1a1/" + pair[0] + "__" + pair[1]
}

// peerDirLegado é a ruta que se usaba cando o 1:1 estaba limitado ao grupo
// (1a1/<grupo>/<a>__<b>). Só se le, nunca se escribe: consérvase para que ninguén
// perda o historial anterior ao 1:1 por materia. Ver readPeerThread.
func peerDirLegado(group, a, b string) string {
	pair := []string{a, b}
	sort.Strings(pair)
	return sharedRoot + "/" + fbMessagesRoot + "/1a1/" + group + "/" + pair[0] + "__" + pair[1]
}

// validMateriaID rejects an empty id or one that would escape the folder.
func validMateriaID(materiaID string) (string, error) {
	materiaID = strings.TrimSpace(materiaID)
	if materiaID == "" {
		return "", fmt.Errorf("indica a materia")
	}
	if strings.ContainsAny(materiaID, "/\\") || strings.Contains(materiaID, "..") {
		return "", fmt.Errorf("identificador de materia non válido")
	}
	return materiaID, nil
}

// validGroup validates a group name used as a folder segment.
func validGroup(group string) (string, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return "", fmt.Errorf("indica o grupo")
	}
	if strings.ContainsAny(group, "/\\") || strings.Contains(group, "..") {
		return "", fmt.Errorf("nome de grupo non válido")
	}
	return group, nil
}

// validLogin validates a username used as a folder segment.
func validLogin(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("indica o usuario")
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("usuario non válido")
	}
	return name, nil
}

// cleanMessageText trims the body and enforces the length limit. An empty body is only
// allowed when the message carries attachments.
func cleanMessageText(text string, hasAttachments bool) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" && !hasAttachments {
		return "", fmt.Errorf("a mensaxe está baleira")
	}
	if len(text) > maxMessageLen {
		return "", fmt.Errorf("a mensaxe é demasiado longa (máximo %d caracteres)", maxMessageLen)
	}
	return text, nil
}

// newMsgID builds a sortable, collision-resistant id: the send time in milliseconds
// followed by a few random bytes.
func newMsgID(ts int64) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%s", ts, hex.EncodeToString(b[:]))
}

// displayName joins the logged-in user's name and surname, falling back to the login.
func (a *App) displayName() string {
	if a.profile != nil {
		full := strings.TrimSpace(a.profile.Name + " " + a.profile.Surname)
		if full != "" {
			return full
		}
	}
	return a.fbUser
}

// mySide returns the "from" value for the logged-in user.
func (a *App) mySide() string {
	if a.profile != nil && a.profile.Role == "profesor" {
		return "profesor"
	}
	return "alumno"
}

// ---- Generic thread read/write --------------------------------------------------

// readThreadDir returns the messages in a thread folder, oldest first. A missing folder
// (nobody has written yet) is not an error: it yields an empty thread.
func (a *App) readThreadDir(dir string) ([]Message, error) {
	items, err := a.fbList(dir)
	if err != nil {
		return []Message{}, nil // folder not created yet: no messages
	}
	var nomes []string
	for _, it := range items {
		if it.IsDir || !strings.HasSuffix(strings.ToLower(it.Name), ".json") {
			continue
		}
		nomes = append(nomes, it.Name)
	}
	// Unha mensaxe por ficheiro: nun fío longo, baixalas en serie era unha ida e volta
	// por mensaxe (ver concorrencia.go). A orde final imponse ao ordenar por TS/ID.
	lidas := make([]*Message, len(nomes))
	enParalelo(len(nomes), func(i int) {
		content, err := a.fbRawGet(dir + "/" + nomes[i])
		if err != nil || len(content) == 0 {
			return // skip an unreadable file rather than failing the whole thread
		}
		var m Message
		if err := json.Unmarshal(content, &m); err != nil {
			return // skip a corrupt message
		}
		lidas[i] = &m
	})
	out := make([]Message, 0, len(nomes))
	for _, m := range lidas {
		if m != nil {
			out = append(out, *m)
		}
	}
	ordenarMensaxes(out)
	return out, nil
}

// ordenarMensaxes ordena por hora de envío; en caso de empate polo id (que empeza
// polo mesmo timestamp e segue con bytes aleatorios), para que a orde sexa
// determinista entre mensaxes do mesmo milisegundo.
func ordenarMensaxes(out []Message) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].TS != out[j].TS {
			return out[i].TS < out[j].TS
		}
		return out[i].ID < out[j].ID
	})
}

// postMessage uploads any attachments into dir/adxuntos/<id>/ and then writes the
// message file into dir, filling in Attachments/AttachBase from what reached the server.
func (a *App) postMessage(dir string, m Message, attachPaths []string) error {
	if err := a.fbEnsureWorkDir(dir); err != nil {
		return fmt.Errorf("non se puido preparar a conversa no servidor: %w", err)
	}
	if len(attachPaths) > 0 {
		adx := dir + "/adxuntos/" + m.ID
		if err := a.fbEnsureWorkDir(adx); err != nil {
			return fmt.Errorf("non se puido preparar os adxuntos: %w", err)
		}
		if _, err := a.uploadLocalFiles(adx, attachPaths); err != nil {
			return err
		}
		// Reflect exactly what is on the server (names and sizes).
		if items, err := a.fbList(adx); err == nil {
			for _, it := range items {
				if !it.IsDir {
					m.Attachments = append(m.Attachments, WorkFile{Name: it.Name, Size: it.Size})
				}
			}
		}
		m.AttachBase = dir
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return a.fbUpload(dir+"/"+m.ID+".json", raw)
}

// newMessage builds a Message authored by the logged-in user with a fresh id/timestamp.
func (a *App) newMessage(text string) Message {
	ts := time.Now().UnixMilli()
	return Message{
		ID:         newMsgID(ts),
		From:       a.mySide(),
		Author:     a.fbUser,
		AuthorName: a.displayName(),
		Text:       text,
		TS:         ts,
	}
}

// ---- Messaging policy (root/admin controlled) -----------------------------------

// assertStudentMessagingAllowed rejects a send when the logged-in student, or their
// group, has messaging disabled by the administrator. Professors and the admin are
// exempt (they call this via nothing; only student sends invoke it).
func (a *App) assertStudentMessagingAllowed() error {
	if a.profile != nil && a.profile.Role == "profesor" {
		return nil
	}
	cfg, _ := a.GetMessagingConfig() // on error, defaults leave everything enabled
	uname := strings.ToLower(strings.TrimSpace(a.fbUser))
	for _, u := range cfg.DisabledUsers {
		if strings.ToLower(strings.TrimSpace(u)) == uname && uname != "" {
			return fmt.Errorf("a túa mensaxería está desactivada pola administración")
		}
	}
	group := ""
	if a.profile != nil {
		group = strings.ToLower(strings.TrimSpace(a.profile.Group))
	}
	if group != "" {
		for _, g := range cfg.DisabledGroups {
			// Se o alumno está en varios grupos e a administración desactivou algún
			// deles, mándase o criterio máis restritivo.
			if pertenceAGrupo(group, g) {
				return fmt.Errorf("a mensaxería do teu grupo está desactivada pola administración")
			}
		}
	}
	return nil
}

// ---- Professor ↔ student (per materia) ------------------------------------------

// GetMyMessages returns the logged-in student's conversation with the professorado for
// a materia, oldest first.
func (a *App) GetMyMessages(materiaID string) ([]Message, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	materiaID, err := validMateriaID(materiaID)
	if err != nil {
		return nil, err
	}
	return a.readThreadDir(messagesDir(a.fbUser, materiaID))
}

// SendMessageToProfessor posts a message (with optional attachments) from the logged-in
// student into their own thread for a materia. The professor reads it from there.
func (a *App) SendMessageToProfessor(materiaID, text string, attachPaths []string) error {
	if a.profile == nil || !a.profile.Connected {
		return fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	if err := a.assertStudentMessagingAllowed(); err != nil {
		return err
	}
	materiaID, err := validMateriaID(materiaID)
	if err != nil {
		return err
	}
	text, err = cleanMessageText(text, len(attachPaths) > 0)
	if err != nil {
		return err
	}
	m := a.newMessage(text)
	m.MateriaID = materiaID
	return a.postMessage(messagesDir(a.fbUser, materiaID), m, attachPaths)
}

// GetStudentMessages returns a student's conversation for a materia. Professor only.
func (a *App) GetStudentMessages(username, materiaID string) ([]Message, error) {
	if err := a.requireProfessor(); err != nil {
		return nil, err
	}
	username, err := validLogin(username)
	if err != nil {
		return nil, err
	}
	materiaID, err = validMateriaID(materiaID)
	if err != nil {
		return nil, err
	}
	return a.readThreadDir(messagesDir(username, materiaID))
}

// SendMessageToStudent posts a professor's message (with optional attachments) into a
// student's thread for a materia. Professor only.
func (a *App) SendMessageToStudent(username, materiaID, text string, attachPaths []string) error {
	if err := a.requireProfessor(); err != nil {
		return err
	}
	username, err := validLogin(username)
	if err != nil {
		return err
	}
	materiaID, err = validMateriaID(materiaID)
	if err != nil {
		return err
	}
	text, err = cleanMessageText(text, len(attachPaths) > 0)
	if err != nil {
		return err
	}
	m := a.newMessage(text)
	m.MateriaID = materiaID
	return a.postMessage(messagesDir(username, materiaID), m, attachPaths)
}

// SendMessageToStudents posts the same message (with optional attachments) to each of
// the given students' threads for a materia — one, several or all. Flagged as a group
// notice when there is more than one recipient. Returns how many received it. Professor only.
func (a *App) SendMessageToStudents(usernames []string, materiaID, text string, attachPaths []string) (int, error) {
	if err := a.requireProfessor(); err != nil {
		return 0, err
	}
	materiaID, err := validMateriaID(materiaID)
	if err != nil {
		return 0, err
	}
	text, err = cleanMessageText(text, len(attachPaths) > 0)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	var recipients []string
	for _, u := range usernames {
		if u = strings.TrimSpace(u); u != "" && !seen[u] {
			seen[u] = true
			recipients = append(recipients, u)
		}
	}
	if len(recipients) == 0 {
		return 0, fmt.Errorf("indica polo menos un alumno/a")
	}
	broadcast := len(recipients) > 1
	sent := 0
	var firstErr error
	for _, username := range recipients {
		m := a.newMessage(text)
		m.MateriaID = materiaID
		m.Broadcast = broadcast
		if err := a.postMessage(messagesDir(username, materiaID), m, attachPaths); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // best-effort: keep delivering to the rest
		}
		sent++
	}
	if sent == 0 && firstErr != nil {
		return 0, firstErr
	}
	return sent, nil
}

// BroadcastToMateria sends the same message to every student enrolled in a materia
// (matched by the materia's groups against the roster). Returns how many received it.
// Professor only.
func (a *App) BroadcastToMateria(materiaID, text string) (int, error) {
	if err := a.requireProfessor(); err != nil {
		return 0, err
	}
	if _, err := validMateriaID(materiaID); err != nil {
		return 0, err
	}
	recipients, err := a.studentsInMateria(materiaID)
	if err != nil {
		return 0, err
	}
	if len(recipients) == 0 {
		return 0, fmt.Errorf("non hai alumnado matriculado nesta materia")
	}
	return a.SendMessageToStudents(recipients, materiaID, text, nil)
}

// studentsInMateria returns the logins of the students whose roster group is one of the
// materia's groups.
func (a *App) studentsInMateria(materiaID string) ([]string, error) {
	var def MateriaDef
	if ok, err := a.fbReadSharedJSON(sharedMateriasDir+"/"+materiaID+".json", &def); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("non se atopou a materia %q", materiaID)
	}
	groups := map[string]bool{}
	for _, g := range def.Groups {
		if g = strings.TrimSpace(strings.ToLower(g)); g != "" {
			groups[g] = true
		}
	}
	if len(groups) == 0 {
		return nil, nil
	}
	roster := map[string]RosterEntry{}
	if _, err := a.fbReadSharedJSON(sharedAlumnos, &roster); err != nil {
		return nil, err
	}
	var out []string
	for username, e := range roster {
		for g := range groups {
			if pertenceAGrupo(e.Group, g) {
				out = append(out, username)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// ---- Group wall (shared, per group) ---------------------------------------------

// GetWallMessages returns the group wall thread, oldest first. Any connected user may
// read it (students of the group and the professorado).
func (a *App) GetWallMessages(group string) ([]Message, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	group, err := validGroup(group)
	if err != nil {
		return nil, err
	}
	return a.readThreadDir(wallDir(group))
}

// PostToWall publishes a message (with optional attachments) on a group's wall. Students
// may post to their own group's wall (when messaging is enabled); professors to any.
func (a *App) PostToWall(group, text string, attachPaths []string) error {
	if a.profile == nil || !a.profile.Connected {
		return fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	group, err := validGroup(group)
	if err != nil {
		return err
	}
	isProf := a.profile.Role == "profesor"
	if !isProf {
		if err := a.assertStudentMessagingAllowed(); err != nil {
			return err
		}
		if cfg, _ := a.GetMessagingConfig(); !cfg.WallEnabled {
			return fmt.Errorf("o muro de grupo está desactivado pola administración")
		}
		if !pertenceAGrupo(a.profile.Group, group) {
			return fmt.Errorf("só podes escribir no muro do teu propio grupo")
		}
	}
	text, err = cleanMessageText(text, len(attachPaths) > 0)
	if err != nil {
		return err
	}
	m := a.newMessage(text)
	m.Group = group
	return a.postMessage(wallDir(group), m, attachPaths)
}

// ---- Student 1:1 (grupo ou materia, shared) --------------------------------------
//
// O 1:1 non se limita ao grupo: hai materias que xuntan alumnado de varios grupos
// (bac1a e bac1b na mesma Filosofía) e ese alumnado é compañeiro para todos os
// efectos. O círculo de contactos calcúlase en gruposDoMeuContorno.

// gruposDoMeuContorno devolve, en minúsculas, os grupos cos que o usuario comparte
// clase: os seus propios máis os de toda materia na que estea matriculado.
//
// Se as materias non se poden ler devólvense só os propios (o comportamento de
// antes): degrádase a menos contactos, nunca a máis.
func (a *App) gruposDoMeuContorno() map[string]bool {
	out := map[string]bool{}
	meus := ""
	if a.profile != nil {
		meus = a.profile.Group
	}
	for _, g := range gruposDe(meus) {
		out[strings.ToLower(g)] = true
	}
	if len(out) == 0 {
		return out // sen grupo non hai contorno ningún
	}
	materias, err := a.listMateriasRaw()
	if err != nil {
		return out
	}
	for _, m := range materias {
		miña := false
		for _, g := range m.Groups {
			if pertenceAGrupo(meus, g) {
				miña = true
				break
			}
		}
		if !miña {
			continue
		}
		for _, g := range m.Groups {
			if g = strings.TrimSpace(strings.ToLower(g)); g != "" {
				out[g] = true
			}
		}
	}
	return out
}

// noMeuContorno indica se o campo de grupo dun perfil cae dentro do contorno dado.
func noMeuContorno(contorno map[string]bool, campoGrupo string) bool {
	for _, g := range gruposDe(campoGrupo) {
		if contorno[strings.ToLower(g)] {
			return true
		}
	}
	return false
}

// podeFalarCon indica se o usuario pode abrir unha conversa 1:1 con other: abonda con
// compartir grupo ou materia. O profesorado fala con quen queira.
func (a *App) podeFalarCon(other string) bool {
	if a.profile != nil && a.profile.Role == "profesor" {
		return true
	}
	e, ok := a.fbRosterEntry(other)
	if !ok {
		return false
	}
	return noMeuContorno(a.gruposDoMeuContorno(), e.Group)
}

// gruposComunsCon devolve os grupos que o usuario comparte literalmente con other.
// Úsase só para localizar os fíos antigos (ver readPeerThread).
func (a *App) gruposComunsCon(other string) []string {
	e, ok := a.fbRosterEntry(other)
	if !ok {
		return nil
	}
	meus := ""
	if a.profile != nil {
		meus = a.profile.Group
	}
	var out []string
	for _, g := range gruposDe(meus) {
		if pertenceAGrupo(e.Group, g) {
			out = append(out, g)
		}
	}
	return out
}

// readPeerThread le o fío 1:1 con other: o actual máis, se as dúas persoas comparten
// grupo, o que quedou na ruta antiga por grupo. Todo se mestura e ordénase por hora,
// así que ninguén perde as conversas anteriores ao 1:1 por materia.
func (a *App) readPeerThread(other string) ([]Message, error) {
	msgs, err := a.readThreadDir(peerDir(a.fbUser, other))
	if err != nil {
		return nil, err
	}
	for _, g := range a.gruposComunsCon(other) {
		vellas, err := a.readThreadDir(peerDirLegado(g, a.fbUser, other))
		if err != nil {
			continue // unha carpeta vella ilexible non debe tumbar o fío actual
		}
		msgs = append(msgs, vellas...)
	}
	ordenarMensaxes(msgs)
	return msgs, nil
}

// GetPeerMessages returns the logged-in student's 1:1 conversation with a classmate
// (same group or same materia), oldest first.
func (a *App) GetPeerMessages(other string) ([]Message, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	other, err := validLogin(other)
	if err != nil {
		return nil, err
	}
	return a.readPeerThread(other)
}

// SendPeerMessage posts a 1:1 message (with optional attachments) from the logged-in
// student to a classmate of the same group or the same materia.
func (a *App) SendPeerMessage(other, text string, attachPaths []string) error {
	if a.profile == nil || !a.profile.Connected {
		return fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	if err := a.assertStudentMessagingAllowed(); err != nil {
		return err
	}
	if cfg, _ := a.GetMessagingConfig(); !cfg.PeerEnabled {
		return fmt.Errorf("as mensaxes privadas entre alumnado están desactivadas pola administración")
	}
	other, err := validLogin(other)
	if err != nil {
		return err
	}
	if strings.EqualFold(other, a.fbUser) {
		return fmt.Errorf("non podes enviarte unha mensaxe a ti mesmo")
	}
	if !a.podeFalarCon(other) {
		return fmt.Errorf("só podes falar con compañeiros do teu grupo ou das túas materias")
	}
	text, err = cleanMessageText(text, len(attachPaths) > 0)
	if err != nil {
		return err
	}
	m := a.newMessage(text)
	if a.profile != nil {
		m.Group = strings.TrimSpace(a.profile.Group) // informativo: de onde escribe quen envía
	}
	return a.postMessage(peerDir(a.fbUser, other), m, attachPaths)
}

// ListMyPeers returns the classmates the logged-in student may chat with 1:1: everyone
// in their own group plus everyone in any group that shares a materia with them. Cada
// fila leva o seu grupo para que a UI poida distinguir un bac1a dun bac1b.
func (a *App) ListMyPeers() ([]RosterRow, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	contorno := a.gruposDoMeuContorno()
	if len(contorno) == 0 {
		return []RosterRow{}, nil // sen grupo: nin materias nin compañeiros
	}
	roster := map[string]RosterEntry{}
	if _, err := a.fbReadSharedJSON(sharedAlumnos, &roster); err != nil {
		return nil, err
	}
	out := make([]RosterRow, 0, len(roster))
	for username, e := range roster {
		if strings.EqualFold(username, a.fbUser) {
			continue // not oneself
		}
		if noMeuContorno(contorno, e.Group) {
			out = append(out, RosterRow{Username: username, Name: e.Name, Surname: e.Surname, Group: e.Group})
		}
	}
	// Primeiro por grupo e despois por login: na lista única, o alumnado doutro grupo
	// queda agrupado ao final en vez de intercalado.
	sort.Slice(out, func(i, j int) bool {
		if !strings.EqualFold(out[i].Group, out[j].Group) {
			return strings.ToLower(out[i].Group) < strings.ToLower(out[j].Group)
		}
		return out[i].Username < out[j].Username
	})
	return out, nil
}

// ---- Attachments download -------------------------------------------------------

// validAttachBase confirms that base is one of the allowed thread folders, so a client
// cannot use the download endpoint to read arbitrary server paths.
func validAttachBase(base string) (string, error) {
	base = strings.Trim(strings.TrimSpace(base), "/")
	if base == "" || strings.Contains(base, "..") || strings.Contains(base, `\`) {
		return "", fmt.Errorf("ruta de adxunto non válida")
	}
	okUser := strings.HasPrefix(base, fbUsersRoot+"/") && strings.Contains(base, "/"+fbMessagesRoot+"/")
	okShared := strings.HasPrefix(base, sharedRoot+"/"+fbMessagesRoot+"/")
	if !okUser && !okShared {
		return "", fmt.Errorf("ruta de adxunto non permitida")
	}
	return base, nil
}

// DownloadChatAttachment saves one of a message's attachments to disk, asking where.
// Returns the destination path, or "" when the user cancels the dialog.
func (a *App) DownloadChatAttachment(attachBase, msgID, name string) (string, error) {
	if a.profile == nil || !a.profile.Connected {
		return "", fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	base, err := validAttachBase(attachBase)
	if err != nil {
		return "", err
	}
	id, err := workSegment(msgID, "a mensaxe")
	if err != nil {
		return "", err
	}
	file, err := workFileName(name)
	if err != nil {
		return "", err
	}
	content, err := a.fbRawGet(base + "/adxuntos/" + id + "/" + file)
	if err != nil {
		return "", fmt.Errorf("non se puido descargar %q do servidor: %w", file, err)
	}
	dest, err := gardarFicheiro("Gardar o ficheiro", file)
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // user cancelled
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return "", fmt.Errorf("non se puido gardar o ficheiro: %w", err)
	}
	return dest, nil
}

// PickFiles opens a native multiple-file picker and returns the chosen absolute paths,
// so the composer can show the attachments before they are sent. Empty when cancelled.
func (a *App) PickFiles(title string) ([]string, error) {
	if a.profile == nil || !a.profile.Connected {
		return nil, fmt.Errorf("non hai conexión co servidor de ficheiros")
	}
	if strings.TrimSpace(title) == "" {
		title = "Escolle ficheiros para adxuntar"
	}
	paths, err := escollerFicheiros(title)
	if err != nil {
		return nil, err
	}
	if paths == nil {
		return []string{}, nil
	}
	return paths, nil
}
