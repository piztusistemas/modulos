package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// A parte CLÁSICA da xestión de contas: o que fai o File Browser clásico
// (github.com/filebrowser/filebrowser, v2.x) no lugar do que fai Quantum en
// admin_fb.go e privacidade.go. As funcións de alí ramifícanse cara aquí cando
// fbSaborDe(a.fbURL) == fbClasico (ver fbsabor.go).
//
// Diferenzas de fondo co protocolo de Quantum:
//   - Todo pasa por un obxecto usuario completo {"what":"user","which":[…],"data":{…}}
//     en PUT/POST /api/users[/<id>]; non hai chamadas separadas de permisos, contrasinal
//     e regras.
//   - Para confirmar unha operación sensible o servidor pide o contrasinal do ADMIN no
//     corpo ("current_password"), non na cabeceira X-Password.
//   - As regras de acceso (notas privadas) viven DENTRO do usuario, como regex (ver
//     privacidade_clasico.go).

// fbUpdatePasswordClasico cambia o contrasinal do propio usuario con PUT /api/users/<id>.
// Para imitar exactamente o que envía o cliente web de File Browser (e evitar problemas
// cun corpo parcial), primeiro le o obxecto usuario actual, troca o contrasinal e
// devólveo restrinxindo a actualización ao campo password — o único cambio que pode
// facer unha conta non admin.
func (a *App) fbUpdatePasswordClasico(token string, userID int, currentPassword, newPassword string) error {
	user, err := a.fbGetUser(token, userID)
	if err != nil {
		return err
	}
	user["password"] = newPassword

	// As versións recentes esixen o contrasinal actual para autorizar o cambio propio
	// (advisory GHSA-hxw8-4h9j-hq2r). O nome exacto do campo envíase baixo as súas
	// grafías máis probables; File Browser ignora as que non recoñece, así que colle a
	// boa sen romper nada.
	body, _ := json.Marshal(map[string]any{
		"what":             "user",
		"which":            []string{"password"},
		"data":             user,
		"currentPassword":  currentPassword,
		"current_password": currentPassword,
		"oldPassword":      currentPassword,
	})
	req, err := http.NewRequest(http.MethodPut, fbUserURL(a.fbURL, userID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	fbAuth(req, token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := fbClient().Do(req)
	if err != nil {
		return fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("o servidor de ficheiros rexeitou o cambio de contrasinal (%d): %s",
			resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

// fbAdminUserCallClasico envía unha petición de xestión de contas ao clásico como
// administrador. Devolve o status e o corpo para que quen chama decida que conta como
// éxito.
func (a *App) fbAdminUserCallClasico(method, endpoint string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, endpoint, rd)
	if err != nil {
		return 0, nil, err
	}
	fbAuth(req, a.fbToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := fbClient().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("non se puido contactar co servidor de ficheiros: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, nil
}

// fbDefaultsClasico devolve os axustes por defecto do servidor para contas novas (perm,
// scope, locale). Tolera que o endpoint falte ou estea prohibido devolvendo un mapa
// baleiro; daquela fbCreateUserClasico usa valores razoables propios.
func (a *App) fbDefaultsClasico() map[string]any {
	req, err := http.NewRequest(http.MethodGet, a.fbURL+"/api/settings", nil)
	if err != nil {
		return nil
	}
	fbAuth(req, a.fbToken)
	resp, err := fbClient().Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var settings struct {
		Defaults map[string]any `json:"defaults"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return nil
	}
	return settings.Defaults
}

// fbCreateUserClasico crea unha conta no File Browser clásico. Permisos, scope e locale
// veñen dos defectos do propio servidor (GET /api/settings) para que a conta se
// comporte coma unha creada desde a web de File Browser; se non están dispoñibles
// úsanse valores propios. A convención users/<username> que usa Tao consérvase deixando
// o scope por defecto do servidor.
func (a *App) fbCreateUserClasico(username, password string) error {
	defaults := a.fbDefaultsClasico()

	perm := map[string]any{
		"admin": false, "execute": true, "create": true, "rename": true,
		"modify": true, "delete": true, "share": true, "download": true,
	}
	if p, ok := defaults["perm"].(map[string]any); ok {
		perm = p
	}
	scope := "."
	if s, ok := defaults["scope"].(string); ok && s != "" {
		scope = s
	}
	locale := "gl"
	if l, ok := defaults["locale"].(string); ok && l != "" {
		locale = l
	}

	data := map[string]any{
		"username":     username,
		"password":     password,
		"scope":        scope,
		"locale":       locale,
		"perm":         perm,
		"lockPassword": false,
		"hideDotfiles": false,
		"commands":     []string{},
		// Toda conta nace sen poder ler notas-privadas/ (ver privacidade_clasico.go) ata
		// que SetProfessor llo concede a un profesor.
		"rules":   fbRulesDenyingNotasPrivadas(),
		"sorting": map[string]any{"by": "name", "asc": false},
	}
	// O endpoint de crear usuario esixe un "which" baleiro (só se usa ao actualizar);
	// un non baleiro dá 400. "current_password" é o contrasinal do propio administrador:
	// File Browser >= 2.63 esíxeo para confirmar a operación, se non responde 400 "the
	// current password is incorrect".
	status, respData, err := a.fbAdminUserCallClasico(http.MethodPost, a.fbURL+"/api/users", map[string]any{
		"what":             "user",
		"which":            []string{},
		"current_password": a.fbAdminPass,
		"data":             data,
	})
	if err != nil {
		return err
	}
	// File Browser responde 201 Created (algunhas versións 200) se vai ben.
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("o servidor rexeitou a creación do usuario (%d): %s",
			status, strings.TrimSpace(string(respData)))
	}
	return nil
}

// fbDeleteUserClasico elimina unha conta polo seu id. File Browser >= 2.63 agarda un
// corpo JSON co contrasinal do administrador en "current_password" (a mesma
// confirmación que esixe para crear/actualizar); sen corpo responde 400 "EOF".
func (a *App) fbDeleteUserClasico(id int) error {
	status, data, err := a.fbAdminUserCallClasico(http.MethodDelete, fbUserURL(a.fbURL, id),
		map[string]any{"current_password": a.fbAdminPass})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("o servidor rexeitou a eliminación do usuario (%d): %s",
			status, strings.TrimSpace(string(data)))
	}
	return nil
}

// fbAdminSetPasswordClasico cambia o contrasinal doutro usuario co token de admin. A
// diferenza do cambio propio (fbUpdatePasswordClasico) non fai falta o contrasinal
// actual dese usuario. Le antes o obxecto completo para conservar todos os outros campos.
func (a *App) fbAdminSetPasswordClasico(id int, newPassword string) error {
	user, err := a.fbGetUser(a.fbToken, id)
	if err != nil {
		return err
	}
	user["password"] = newPassword
	// current_password é o contrasinal do propio administrador, que File Browser >= 2.63
	// esixe para autorizar o cambio (ver fbCreateUserClasico).
	status, data, err := a.fbAdminUserCallClasico(http.MethodPut, fbUserURL(a.fbURL, id), map[string]any{
		"what":             "user",
		"which":            []string{"password"},
		"current_password": a.fbAdminPass,
		"data":             user,
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("o servidor rexeitou o cambio de contrasinal (%d): %s",
			status, strings.TrimSpace(string(data)))
	}
	return nil
}
