package main

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
)

// Administrator-side File Browser operations: enrolling and removing student
// accounts. These require a File Browser administrator token, obtained from the
// credentials stored in admin.json (see admin.go). They reuse the resource helpers
// in filebrowser.go (fbEnsureDir/fbUpload/fbDelete/fbList) by temporarily pointing
// the app's File Browser session at the administrator token.

// NewUser is the data needed to enrol one student. Field names match the reference
// CSV columns; the certificate uses Firstname/Lastname, and Group is the student's
// class group (profile_field_grupo in the CSV).
type NewUser struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Firstname  string `json:"firstname"`
	Lastname   string `json:"lastname"`
	Email      string `json:"email"`
	City       string `json:"city"`
	Department string `json:"department"`
	Group      string `json:"group"`
}

// AdminUser is a row of the users table shown in the admin panel.
type AdminUser struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	Surname     string `json:"surname"`
	Group       string `json:"group"`
	IsAdmin     bool   `json:"isAdmin"`
	IsProfessor bool   `json:"isProfessor"` // listed in dixitalizacion/profesores.json
}

// CSVResult summarises a bulk enrolment.
type CSVResult struct {
	Created int      `json:"created"`
	Total   int      `json:"total"`
	Errors  []string `json:"errors"`
}

// fbAdminSession logs in to File Browser with the configured administrator account.
// It also returns the administrator's own password: recent File Browser versions
// require it as "current_password" to confirm user-management operations.
func (a *App) fbAdminSession() (base, token, adminPass string, err error) {
	cfg, err := a.GetAdminConfig()
	if err != nil {
		return "", "", "", err
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return "", "", "", fmt.Errorf("configura o enderezo do servidor de ficheiros")
	}
	if strings.TrimSpace(cfg.FBAdminUser) == "" || cfg.FBAdminPass == "" {
		return "", "", "", fmt.Errorf("configura o usuario e contrasinal de administrador de File Browser")
	}
	base = normalizeServerURL(cfg.ServerURL)
	token, err = fbLogin(base, cfg.FBAdminUser, cfg.FBAdminPass)
	if err != nil {
		// Unha conta bloqueada non é un problema de URL: buscar outra só gastaría máis
		// intentos contra un usuario que xa está en castigo.
		if errors.Is(err, errFBContaBloqueada) {
			return "", "", "", fmt.Errorf("non se puido iniciar sesión como administrador de File Browser: %w", err)
		}
		// A URL gardada pode ter envellecido: o servidor mudou de porto, ou gañou (ou
		// perdeu) a subruta /arquivos do proxy de Apache. Iso non dá un erro de
		// credenciais senón un 404 desconcertante, e deixaba o Director bloqueado sen
		// saber por que. A pantalla de acceso xa se corrixe soa (ver servidor.go); aquí
		// faise o mesmo: búscase a boa e, se se atopa, gárdase para as vindeiras veces.
		if outra := a.DetectarServidor(base); outra != "" && outra != base {
			if tk, err2 := fbLogin(outra, cfg.FBAdminUser, cfg.FBAdminPass); err2 == nil {
				log.Printf("[admin] o servidor de ficheiros xa non responde en %s; pásase a %s", base, outra)
				cfg.ServerURL = outra
				if e := a.gardarAdminConfigDirecto(*cfg); e != nil {
					log.Printf("[admin] non se puido gardar a URL corrixida: %v", e)
				}
				return outra, tk, cfg.FBAdminPass, nil
			}
		}
		return "", "", "", fmt.Errorf("non se puido iniciar sesión como administrador de File Browser: %w", err)
	}
	return base, token, cfg.FBAdminPass, nil
}

// gardarAdminConfigDirecto escribe admin.json sen pasar por SaveAdminConfig. Úsase só
// para corrixir a URL do servidor desde dentro (ver fbAdminSession): SaveAdminConfig
// verifica as credenciais contra o servidor antes de gardar, e aquí xa se verificaron
// —é o único motivo polo que sabemos que a URL nova é a boa.
func (a *App) gardarAdminConfigDirecto(cfg AdminConfig) error {
	datos, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.adminConfigPath(), datos, 0o600)
}

// withAdminFB runs fn with the app's File Browser session pointed at an administrator
// token, restoring the previous session afterwards so nothing else is disturbed.
func (a *App) withAdminFB(fn func() error) error {
	if err := a.requireAdmin(); err != nil {
		log.Printf("[admin] operación rexeitada: %v", err)
		return err
	}
	base, token, adminPass, err := a.fbAdminSession()
	if err != nil {
		// Log every failure to open the administrator session so it is visible even
		// when the frontend banner is missed: this is the usual reason enrolments and
		// the user listing "do nothing" (server URL/credenciais sen gardar, conta sen
		// permisos de administración, ou servidor inaccesible).
		log.Printf("[admin] non se puido abrir a sesión de administrador de File Browser: %v", err)
		return err
	}
	log.Printf("[admin] sesión de administrador aberta en %s", base)
	prevURL, prevToken, prevAdminPass := a.fbURL, a.fbToken, a.fbAdminPass
	a.fbURL, a.fbToken, a.fbAdminPass = base, token, adminPass
	defer func() { a.fbURL, a.fbToken, a.fbAdminPass = prevURL, prevToken, prevAdminPass }()
	if err := fn(); err != nil {
		log.Printf("[admin] a operación fallou: %v", err)
		return err
	}
	return nil
}

// ListUsers returns the File Browser accounts, enriched with the local reference
// name/surname/group so the panel can show them.
func (a *App) ListUsers() ([]AdminUser, error) {
	var out []AdminUser
	err := a.withAdminFB(func() error {
		users, err := a.fbListUsers()
		if err != nil {
			return err
		}
		profiles, _ := a.loadProfiles()
		byName := map[string]storedProfile{}
		for _, p := range profiles {
			byName[strings.ToLower(p.Username)] = p
		}
		// professor role set, from the shared manifest.
		var profs []string
		a.fbReadSharedJSON(sharedProfesores, &profs)
		isProf := map[string]bool{}
		for _, p := range profs {
			isProf[strings.ToLower(strings.TrimSpace(p))] = true
		}
		// The server roster is the source of truth for the group (it is what students
		// resolve at login, and what ChangeUserGroup edits); the local profile only
		// fills in accounts the roster does not know.
		roster := map[string]RosterEntry{}
		a.fbReadSharedJSON(sharedAlumnos, &roster)
		byRoster := map[string]RosterEntry{}
		for name, e := range roster {
			byRoster[strings.ToLower(name)] = e
		}
		for _, u := range users {
			username, _ := u["username"].(string)
			row := AdminUser{Username: username, IsAdmin: fbUserIsAdmin(u), IsProfessor: isProf[strings.ToLower(username)]}
			if p, ok := byName[strings.ToLower(username)]; ok {
				row.Name, row.Surname, row.Group = p.Name, p.Surname, p.Group
			}
			if e, ok := byRoster[strings.ToLower(username)]; ok {
				row.Name = cmp.Or(row.Name, e.Name)
				row.Surname = cmp.Or(row.Surname, e.Surname)
				row.Group = cmp.Or(e.Group, row.Group)
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}

// CreateUser enrols a single student: creates the File Browser account, prepares its
// server folder, issues a personal certificate signed by the department CA (protected
// with the student's password) and uploads it, and records the identity locally.
func (a *App) CreateUser(u NewUser) error {
	return a.withAdminFB(func() error { return a.createUserLocked(u) })
}

// createUserLocked performs the enrolment assuming an administrator File Browser
// session is already active (a.fbURL/a.fbToken). Shared by CreateUser and the CSV
// import so the admin login happens once for the whole batch.
func (a *App) createUserLocked(u NewUser) error {
	u.Username = strings.TrimSpace(u.Username)
	u.Firstname = strings.TrimSpace(u.Firstname)
	u.Lastname = strings.TrimSpace(u.Lastname)
	if u.Username == "" || u.Password == "" {
		return fmt.Errorf("usuario e contrasinal son obrigatorios")
	}
	if u.Firstname == "" || u.Lastname == "" {
		return fmt.Errorf("nome e apelidos son obrigatorios para o certificado")
	}

	if err := a.fbCreateUser(u.Username, u.Password); err != nil {
		return err
	}

	dir := fbUsersRoot + "/" + u.Username
	if err := a.fbEnsureDir(dir); err != nil {
		return fmt.Errorf("conta creada pero non se puido preparar o cartafol %q: %w", dir, err)
	}

	pfx, err := generateStudentP12(a.dataDir, u.Firstname, u.Lastname, u.Password)
	if err != nil {
		return fmt.Errorf("conta creada pero non se puido xerar o certificado: %w", err)
	}
	remote := dir + "/" + certFileBase(u.Firstname, u.Lastname) + ".p12"
	if err := a.fbUpload(remote, pfx); err != nil {
		return fmt.Errorf("conta creada pero non se puido subir o certificado: %w", err)
	}

	// Internal UUID: identifies this student's work in de-identified folders
	// (fbPracticasRoot) without ever using their name or certificate. Kept redundantly
	// in three places (see fbIdentidadeFile) so the uuid<->login link is never lost.
	studentUUID := uuid.NewString()
	identidade, _ := json.Marshal(map[string]string{"uuid": studentUUID})
	if err := a.fbUpload(dir+"/"+fbIdentidadeFile, identidade); err != nil {
		log.Printf("[admin] non se puido gardar a identidade interna de %q no seu cartafol: %v", u.Username, err)
	}
	if err := a.fbSetUUIDEntry(u.Username, studentUUID); err != nil {
		log.Printf("[admin] non se puido actualizar o mapeo de uuid do servidor para %q: %v", u.Username, err)
	}

	if err := a.saveFullIdentity(u.Username, u.Firstname, u.Lastname, u.Group, u.Email, studentUUID); err != nil {
		log.Printf("[admin] non se puido gardar a identidade de %q localmente: %v", u.Username, err)
	}
	// Mirror the identity to the server roster so any student device can resolve its
	// group (and thus its enrolled materias) at login.
	if err := a.fbSetRosterEntry(u.Username, RosterEntry{Name: u.Firstname, Surname: u.Lastname, Group: u.Group}); err != nil {
		log.Printf("[admin] non se puido actualizar o roster do servidor para %q: %v", u.Username, err)
	}
	log.Printf("[admin] usuario %q creado, certificado subido a %s, uuid interno %s", u.Username, remote, studentUUID)
	return nil
}

// CreateUsersFromCSV enrols every student in a CSV export (see parseUsersCSV for the
// expected columns). It is best-effort: a failing row is recorded in Errors and the
// rest continue.
func (a *App) CreateUsersFromCSV(csvText string) (*CSVResult, error) {
	users, err := parseUsersCSV(csvText)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("o CSV non contén ningunha fila de usuario")
	}

	res := &CSVResult{Total: len(users)}
	err = a.withAdminFB(func() error {
		for _, u := range users {
			if e := a.createUserLocked(u); e != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", u.Username, e))
				continue
			}
			res.Created++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DeleteUser removes a student's File Browser account and their server folder
// (users/<username>), and drops the local reference record.
func (a *App) DeleteUser(username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("indica o usuario a eliminar")
	}
	if strings.EqualFold(username, directorUser) {
		return fmt.Errorf("non se pode eliminar a conta de administración")
	}
	return a.withAdminFB(func() error { return a.deleteUserLocked(username) })
}

// deleteUserLocked removes one account, its server folder and its local record,
// assuming an administrator File Browser session is already active (a.fbURL/a.fbToken).
// Shared by DeleteUser and the CSV bulk deletion so the admin login happens once.
func (a *App) deleteUserLocked(username string) error {
	id, err := a.fbUserIDByName(username)
	if err != nil {
		return err
	}
	if err := a.fbDeleteUser(id); err != nil {
		return err
	}
	// Quantum keeps the account's access rule after deleting it, and a new account with
	// the same name would inherit it (see privacidade.go). The classic File Browser keeps
	// the rule inside the account, so it goes away with it.
	if fbSaborDe(a.fbURL) != fbClasico {
		if err := a.fbSetUserPrivacyRule(username, false); err != nil {
			log.Printf("[admin] usuario %q eliminado; non se puido quitar a súa regra de notas privadas: %v", username, err)
		}
	}
	// Best-effort folder removal: the account is already gone.
	if err := a.fbDelete(fbUsersRoot + "/" + username); err != nil {
		log.Printf("[admin] usuario %q eliminado; non se puido borrar o cartafol: %v", username, err)
	}
	if err := a.removeIdentity(username); err != nil {
		log.Printf("[admin] non se puido limpar a identidade local de %q: %v", username, err)
	}
	if err := a.fbRemoveRosterEntry(username); err != nil {
		log.Printf("[admin] non se puido limpar o roster do servidor de %q: %v", username, err)
	}
	if err := a.fbRemoveUUIDEntry(username); err != nil {
		log.Printf("[admin] non se puido limpar o mapeo de uuid do servidor de %q: %v", username, err)
	}
	log.Printf("[admin] usuario %q eliminado (conta e cartafol)", username)
	return nil
}

// DeleteUsersFromCSV removes every student listed in a CSV (same format as
// CreateUsersFromCSV; only the username column is used), deleting each account and
// its server folder. It is best-effort: a failing row is recorded in Errors and the
// rest continue. The administrator account (root) and the configured File Browser
// administrator are never deleted, even if present in the file.
func (a *App) DeleteUsersFromCSV(csvText string) (*CSVResult, error) {
	users, err := parseUsersCSV(csvText)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("o CSV non contén ningunha fila de usuario")
	}

	cfg, _ := a.GetAdminConfig()
	protected := map[string]bool{strings.ToLower(directorUser): true}
	if cfg != nil {
		protected[strings.ToLower(strings.TrimSpace(cfg.FBAdminUser))] = true
	}

	res := &CSVResult{Total: len(users)}
	err = a.withAdminFB(func() error {
		for _, u := range users {
			name := strings.TrimSpace(u.Username)
			if name == "" {
				continue
			}
			if protected[strings.ToLower(name)] {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: conta protexida, non se elimina", name))
				continue
			}
			if e := a.deleteUserLocked(name); e != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, e))
				continue
			}
			res.Created++ // reused as "processed OK" count
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ChangeUserPassword sets a new password for username via the administrator API and
// re-issues the student's certificate (protected with the new password), so signing
// keeps working. The name/surname come from the local reference profiles.
func (a *App) ChangeUserPassword(username, newPassword string) error {
	username = strings.TrimSpace(username)
	newPassword = strings.TrimSpace(newPassword)
	if username == "" || newPassword == "" {
		return fmt.Errorf("indica o usuario e o novo contrasinal")
	}
	if len(newPassword) < 4 {
		return fmt.Errorf("o novo contrasinal debe ter polo menos 4 caracteres")
	}
	return a.withAdminFB(func() error {
		id, err := a.fbUserIDByName(username)
		if err != nil {
			return err
		}
		if err := a.fbAdminSetPassword(id, newPassword); err != nil {
			return err
		}

		// Re-issue the certificate with the new password when we know the identity.
		name, surname := "", ""
		if profiles, err := a.loadProfiles(); err == nil {
			for _, p := range profiles {
				if strings.EqualFold(p.Username, username) {
					name, surname = p.Name, p.Surname
					break
				}
			}
		}
		if name == "" || surname == "" {
			log.Printf("[admin] contrasinal de %q cambiado; sen nome/apelidos locais, non se rexenera o certificado", username)
			return nil
		}
		pfx, err := generateStudentP12(a.dataDir, name, surname, newPassword)
		if err != nil {
			return fmt.Errorf("contrasinal cambiado pero non se puido rexenerar o certificado: %w", err)
		}
		dir := fbUsersRoot + "/" + username
		if err := a.fbEnsureDir(dir); err != nil {
			return fmt.Errorf("contrasinal cambiado pero non se puido acceder ao cartafol: %w", err)
		}
		a.fbDeleteExistingCerts(dir)
		remote := dir + "/" + certFileBase(name, surname) + ".p12"
		if err := a.fbUpload(remote, pfx); err != nil {
			return fmt.Errorf("contrasinal cambiado pero non se puido subir o certificado: %w", err)
		}
		log.Printf("[admin] contrasinal e certificado de %q actualizados", username)
		return nil
	})
}

// SetProfessor grants or revokes the "profesor" role for an existing server account
// by editing the shared manifest dixitalizacion/profesores.json. The username must be
// an account already present on the server (the admin panel lists them).
func (a *App) SetProfessor(username string, isProf bool) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("indica o usuario")
	}
	if strings.EqualFold(username, directorUser) {
		return fmt.Errorf("a conta de administración non pode ser profesor")
	}
	return a.withAdminFB(func() error {
		var profs []string
		a.fbReadSharedJSON(sharedProfesores, &profs)
		kept := profs[:0]
		for _, p := range profs {
			if !strings.EqualFold(strings.TrimSpace(p), username) {
				kept = append(kept, strings.TrimSpace(p))
			}
		}
		if isProf {
			kept = append(kept, username)
		}
		if err := a.fbWriteSharedJSON(sharedProfesores, kept); err != nil {
			return err
		}
		// Keep the notas-privadas/ File Browser rule in sync with the role: a new
		// professor needs to read it (their own materias' notes), a demoted one goes
		// back to being denied, same as any other student. Best-effort: the role
		// change itself already succeeded above, so a failure here is logged, not
		// fatal — the admin panel's "aplicar regra" action can always retry it.
		if err := a.fbSetUserPrivacyRule(username, !isProf); err != nil {
			log.Printf("[admin] rol de profesor de %q = %v, pero non se puido axustar a regra de notas privadas: %v", username, isProf, err)
		}
		log.Printf("[admin] rol de profesor de %q = %v", username, isProf)
		return nil
	})
}

// ---- File Browser admin HTTP helpers -------------------------------------------

// fbListUsers fetches every account (admin only) as generic maps so both the
// username and the admin flag are available.
func (a *App) fbListUsers() ([]map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, a.fbURL+"/api/users", nil)
	if err != nil {
		return nil, err
	}
	fbAuth(req, a.fbToken)
	resp, err := fbClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("non se puido obter a lista de usuarios: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("o servidor devolveu un erro ao listar usuarios (%d): %s. Comproba a URL e que a conta admin de File Browser teña permisos de administración",
			resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var users []map[string]any
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("resposta non válida ao listar usuarios")
	}
	return users, nil
}

// fbUserIsAdmin reports whether a File Browser user object has admin permissions
// (Quantum keeps them under "permissions"; the classic File Browser under "perm").
func fbUserIsAdmin(u map[string]any) bool {
	for _, key := range []string{"permissions", "perm"} {
		if perm, ok := u[key].(map[string]any); ok {
			if admin, _ := perm["admin"].(bool); admin {
				return true
			}
		}
	}
	return false
}

// fbUserIDByName resolves a username to its numeric File Browser id.
func (a *App) fbUserIDByName(username string) (int, error) {
	users, err := a.fbListUsers()
	if err != nil {
		return 0, err
	}
	for _, u := range users {
		if name, _ := u["username"].(string); strings.EqualFold(name, username) {
			if idf, ok := u["id"].(float64); ok {
				return int(idf), nil
			}
		}
	}
	return 0, fmt.Errorf("non se atopou o usuario %q no servidor", username)
}

// fbAdminCall sends one request to Quantum as the administrator and returns the status
// and body, so callers can treat specific answers as success. path is what follows the
// server URL ("/api/users?id=3"). Every change (users and access rules alike) has to be
// confirmed with the ADMIN's own current password in X-Password (advisory
// GHSA-hxw8-4h9j-hq2r), URL-encoded like on login; a missing one answers 401.
func (a *App) fbAdminCall(method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, a.fbURL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	fbAuth(req, a.fbToken)
	req.Header.Set("X-Password", fbPasswordHeader(a.fbAdminPass))
	req.Header.Set("Content-Type", "application/json")
	resp, err := fbClient().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, nil
}

// fbUsersDo is fbAdminCall against /api/users where 200/201/204 is success and anything
// else an error. what names the operation in the message.
func (a *App) fbUsersDo(method, query string, body any, what string) error {
	status, data, err := a.fbAdminCall(method, "/api/users"+query, body)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	}
	return fmt.Errorf("o servidor rexeitou %s (%d): %s", what, status, strings.TrimSpace(string(data)))
}

// fbStudentPermissions is what a student account is allowed to do. Quantum creates
// accounts with EVERY permission off (there are no server-side defaults for the API), so
// the ones a student needs to work in their folders are granted explicitly; share, api
// and realtime stay off, and admin obviously too.
var fbStudentPermissions = map[string]bool{
	"admin": false, "api": false, "realtime": false, "share": false,
	"modify": true, "create": true, "delete": true, "download": true,
}

// fbCreateUser creates a student account (the classic File Browser's version, one call
// with a whole user object, is fbCreateUserClasico). Quantum needs three calls: POST /api/users
// creates it (its password is NOT stored by this call — a login would answer 401), then
// one PUT sets the password and another the permissions and locale, and the
// notas-privadas/ access rule is added (privacidade.go). The account is
// scoped to the whole "arquivos" source ("/"), since paths such as users/<name> are
// absolute from the source root; what a student may NOT read is enforced with access
// rules (see privacidade.go), not with the scope. If a later step fails the half-made
// account is removed again, so a retry does not stumble on "already exists".
func (a *App) fbCreateUser(username, password string) error {
	if fbSaborDe(a.fbURL) == fbClasico {
		return a.fbCreateUserClasico(username, password)
	}
	create := map[string]any{"data": map[string]any{
		"username": username,
		"password": password,
		"scopes":   []map[string]string{{"name": fbSource, "scope": "/"}},
	}}
	if err := a.fbUsersDo(http.MethodPost, "", create, "a creación do usuario"); err != nil {
		return err
	}
	// POST answers with an empty body and a useless Location (/settings/users/0), and
	// ?username= does not filter, so the id has to be looked up in the list.
	id, err := a.fbUserIDByName(username)
	if err != nil {
		return fmt.Errorf("conta creada pero non se atopou despois: %w", err)
	}
	fail := func(err error) error {
		if e := a.fbDeleteUser(id); e != nil {
			log.Printf("[admin] non se puido desfacer a conta %q a medio crear: %v", username, e)
		}
		return err
	}
	if err := a.fbAdminSetPassword(id, password); err != nil {
		return fail(err)
	}
	q := fmt.Sprintf("?id=%d", id)
	if err := a.fbUsersDo(http.MethodPut, q, map[string]any{
		"which": []string{"permissions"},
		"data":  map[string]any{"permissions": fbStudentPermissions},
	}, "a asignación de permisos"); err != nil {
		return fail(err)
	}
	// Fail closed: an account that cannot be denied notas-privadas/ would leak them.
	if err := a.fbSetUserPrivacyRule(username, true); err != nil {
		return fail(fmt.Errorf("non se puido aplicar a regra de notas privadas: %w", err))
	}
	if err := a.fbUsersDo(http.MethodPut, q, map[string]any{
		"which": []string{"locale"},
		"data":  map[string]any{"locale": "gl"},
	}, "o cambio de idioma"); err != nil {
		log.Printf("[admin] %q creado, pero sen idioma galego: %v", username, err) // cosmetic
	}
	return nil
}

// fbDeleteUser removes an account by numeric id. Deleting an id that does not exist
// answers 500 "not found", so callers should resolve the id from the list first.
func (a *App) fbDeleteUser(id int) error {
	if fbSaborDe(a.fbURL) == fbClasico {
		return a.fbDeleteUserClasico(id)
	}
	return a.fbUsersDo(http.MethodDelete, fmt.Sprintf("?id=%d", id), nil, "a eliminación do usuario")
}

// fbAdminSetPassword changes another user's password with the administrator token. Only
// the password field travels ("which"), so nothing else of the account is touched.
func (a *App) fbAdminSetPassword(id int, newPassword string) error {
	if fbSaborDe(a.fbURL) == fbClasico {
		return a.fbAdminSetPasswordClasico(id, newPassword)
	}
	return a.fbUsersDo(http.MethodPut, fmt.Sprintf("?id=%d", id), map[string]any{
		"which": []string{"password"},
		"data":  map[string]string{"password": newPassword},
	}, "o cambio de contrasinal")
}

// ---- CSV parsing ----------------------------------------------------------------

// parseUsersCSV reads a student export with a header row and the columns
// username, password, firstname, lastname, email, city, departament,
// profile_field_grupo (extra/missing columns are tolerated; only username, password,
// firstname and lastname are needed downstream). Both comma- and semicolon-separated
// files are accepted.
func parseUsersCSV(text string) ([]NewUser, error) {
	text = strings.TrimPrefix(text, "\ufeff") // strip UTF-8 BOM
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("o CSV está baleiro")
	}

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = detectDelimiter(text)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("non se puido ler o CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("o CSV non ten filas de datos (só a cabeceira?)")
	}

	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(row []string, name string) string {
		if idx, ok := col[name]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}
	if _, ok := col["username"]; !ok {
		return nil, fmt.Errorf("o CSV non ten a columna obrigatoria 'username'")
	}

	var users []NewUser
	for _, row := range records[1:] {
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue // skip blank lines
		}
		users = append(users, NewUser{
			Username:   get(row, "username"),
			Password:   get(row, "password"),
			Firstname:  get(row, "firstname"),
			Lastname:   get(row, "lastname"),
			Email:      get(row, "email"),
			City:       get(row, "city"),
			Department: get(row, "departament"),
			// "profile_field_grupo" é o nome que traen os CSV exportados de Moodle;
			// "group" acéptase como sinónimo para quen escriba o ficheiro a man ou o
			// saque doutra fonte. Sen grupo, a conta créase pero non ve ningunha
			// materia, así que paga a pena ser xeneroso aquí.
			Group: cmp.Or(get(row, "profile_field_grupo"), get(row, "group")),
		})
	}
	return users, nil
}

// detectDelimiter picks ';' when the header line has more semicolons than commas,
// otherwise ','. Covers the two common spreadsheet exports.
func detectDelimiter(text string) rune {
	firstLine := text
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		firstLine = text[:i]
	}
	if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
		return ';'
	}
	return ','
}
