package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeQuantumUsers is a stand-in for Quantum's users API with the quirks verified against
// a real v1.5.6 server: POST creates the account with EVERY permission off and does NOT
// store its password, changes need the actor's URL-encoded password in X-Password,
// duplicates and unknown ids answer 500, and DELETE answers 200.
type fakeQuantumUsers struct {
	mu        sync.Mutex
	adminPass string
	users     map[int]map[string]any // id -> {"username", "password", "permissions", "locale"}
	next      int
	failPerms bool // make the permissions PUT fail, to exercise the rollback
	// rules is the access-rule table: path (as sent, no trailing slash) -> denied users.
	rules      map[string][]string
	failAccess bool // make POST /api/access fail, to exercise the fail-closed rollback
}

func newFakeQuantumUsers(adminPass string) *fakeQuantumUsers {
	f := &fakeQuantumUsers{adminPass: adminPass, next: 2, users: map[int]map[string]any{}, rules: map[string][]string{}}
	f.users[1] = map[string]any{"username": "admin", "password": adminPass, "locale": "en",
		"permissions": map[string]any{"admin": true, "modify": true}}
	return f
}

func (f *fakeQuantumUsers) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/api/users" && r.URL.Path != "/api/access" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok-admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/access" {
			f.access(w, r)
			return
		}
		if r.Method == http.MethodGet {
			out := []map[string]any{}
			for id, u := range f.users {
				out = append(out, map[string]any{"id": id, "username": u["username"], "permissions": u["permissions"]})
			}
			json.NewEncoder(w).Encode(out)
			return
		}
		if cur, err := url.QueryUnescape(r.Header.Get("X-Password")); err != nil || cur != f.adminPass {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"status":401,"message":"X-Password header is required to confirm your password"}`)
			return
		}
		var body struct {
			Which []string       `json:"which"`
			Data  map[string]any `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		fail := func(msg string) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"status":500,"message":%q}`, msg)
		}
		var id int
		if r.Method != http.MethodPost {
			fmt.Sscan(r.URL.Query().Get("id"), &id)
			if f.users[id] == nil {
				fail("not found")
				return
			}
		}
		switch r.Method {
		case http.MethodPost:
			name, _ := body.Data["username"].(string)
			for _, u := range f.users {
				if u["username"] == name {
					fail("user with provided username already exists")
					return
				}
			}
			f.users[f.next] = map[string]any{"username": name, "password": "", "locale": "en",
				"permissions": map[string]any{"admin": false, "modify": false, "create": false}}
			f.next++
			w.Header().Set("Location", "/settings/users/0")
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			if len(body.Which) != 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch body.Which[0] {
			case "password":
				f.users[id]["password"] = body.Data["password"]
			case "permissions":
				if f.failPerms {
					fail("boom")
					return
				}
				f.users[id]["permissions"] = body.Data["permissions"]
			case "locale":
				f.users[id]["locale"] = body.Data["locale"]
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			delete(f.users, id)
		}
	})
}

// access serves /api/access like Quantum: a deny rule per (path, user); adding one that
// exists answers 500 "already exists", removing one that does not answers 404.
func (f *fakeQuantumUsers) access(w http.ResponseWriter, r *http.Request) {
	if cur, err := url.QueryUnescape(r.Header.Get("X-Password")); err != nil || cur != f.adminPass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	path := q.Get("path")
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Allow        bool   `json:"allow"`
			RuleCategory string `json:"ruleCategory"`
			Value        string `json:"value"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if q.Get("source") != fbSource || body.Allow || body.RuleCategory != "user" || body.Value == "" || f.failAccess {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"status":500,"message":"failed to add or update rule"}`)
			return
		}
		for _, u := range f.rules[path] {
			if u == body.Value {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"status":500,"message":"failed to add or update rule: the resource already exists"}`)
				return
			}
		}
		f.rules[path] = append(f.rules[path], body.Value)
		fmt.Fprint(w, `{"message":"rule added or updated"}`)
	case http.MethodDelete:
		if q.Get("ruleType") != "deny" || q.Get("ruleCategory") != "user" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		kept := []string{}
		for _, u := range f.rules[path] {
			if u != q.Get("value") {
				kept = append(kept, u)
			}
		}
		if len(kept) == len(f.rules[path]) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"status":404,"message":"entry not found in rule"}`)
			return
		}
		f.rules[path] = kept
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeQuantumUsers) denied(path, user string) bool {
	for _, u := range f.rules[path] {
		if u == user {
			return true
		}
	}
	return false
}

func (f *fakeQuantumUsers) byName(name string) (int, map[string]any) {
	for id, u := range f.users {
		if u["username"] == name {
			return id, u
		}
	}
	return 0, nil
}

func adminAppFor(srv *httptest.Server, adminPass string) *App {
	a := NewApp()
	a.fbURL, a.fbToken, a.fbAdminPass = srv.URL, "tok-admin", adminPass
	return a
}

func TestFbCreateUserSetsPasswordPermissionsAndLocale(t *testing.T) {
	// The admin password has characters that need URL-encoding in X-Password.
	const adminPass = "Ñandú p+a%s$ 1"
	fake := newFakeQuantumUsers(adminPass)
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	a := adminAppFor(srv, adminPass)

	if err := a.fbCreateUser("alu1", "Clave 123"); err != nil {
		t.Fatalf("fbCreateUser: %v", err)
	}
	_, u := fake.byName("alu1")
	if u == nil {
		t.Fatal("a conta non se creou")
	}
	if u["password"] != "Clave 123" {
		t.Errorf("contrasinal = %v; o POST non o garda, ten que fixalo o PUT", u["password"])
	}
	perms, _ := u["permissions"].(map[string]any)
	if perms["modify"] != true || perms["create"] != true || perms["delete"] != true || perms["admin"] != false {
		t.Errorf("permisos = %v; un alumno traballa nos seus cartafoles, non é admin", perms)
	}
	if u["locale"] != "gl" {
		t.Errorf("locale = %v; quere gl", u["locale"])
	}
	if !fake.denied("/dixitalizacion/notas-privadas", "alu1") {
		t.Errorf("un alumno novo debe nacer con notas-privadas denegado; regras: %v", fake.rules)
	}

	// A duplicate is refused and the existing account is left alone.
	if err := a.fbCreateUser("alu1", "outro"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("crear un duplicado = %v; quere o erro do servidor", err)
	}
	if _, u := fake.byName("alu1"); u["password"] != "Clave 123" {
		t.Error("o duplicado pisou o contrasinal da conta existente")
	}
}

func TestFbCreateUserRollsBackHalfMadeAccount(t *testing.T) {
	fake := newFakeQuantumUsers("AdminPass1")
	fake.failPerms = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	a := adminAppFor(srv, "AdminPass1")

	if err := a.fbCreateUser("alu1", "Clave123"); err == nil {
		t.Fatal("fbCreateUser debería fallar se os permisos non se aplican")
	}
	if id, _ := fake.byName("alu1"); id != 0 {
		t.Error("quedou unha conta a medio crear: un reintento daría 'already exists'")
	}
	// ...and a retry once the server recovers works.
	fake.failPerms = false
	if err := a.fbCreateUser("alu1", "Clave123"); err != nil {
		t.Errorf("reintento: %v", err)
	}
}

func TestFbAdminUserLifecycle(t *testing.T) {
	fake := newFakeQuantumUsers("AdminPass1")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	a := adminAppFor(srv, "AdminPass1")
	if err := a.fbCreateUser("alu1", "Clave123"); err != nil {
		t.Fatal(err)
	}

	users, err := a.fbListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("fbListUsers = %v, %v", users, err)
	}
	for _, u := range users {
		if want := u["username"] == "admin"; fbUserIsAdmin(u) != want {
			t.Errorf("fbUserIsAdmin(%v) = %v; quere %v", u["username"], fbUserIsAdmin(u), want)
		}
	}

	id, err := a.fbUserIDByName("ALU1") // case-insensitive, like the classic lookup
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.fbUserIDByName("ninguén"); err == nil {
		t.Error("un usuario que non existe debería dar erro")
	}

	if err := a.fbAdminSetPassword(id, "Nova pw+1"); err != nil {
		t.Fatalf("fbAdminSetPassword: %v", err)
	}
	if _, u := fake.byName("alu1"); u["password"] != "Nova pw+1" {
		t.Errorf("contrasinal = %v", u["password"])
	}

	// Without the admin's own password every change is refused (401).
	a.fbAdminPass = "mala"
	if err := a.fbDeleteUser(id); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("borrar co contrasinal de admin mal = %v; quere 401", err)
	}
	a.fbAdminPass = "AdminPass1"
	if err := a.fbDeleteUser(id); err != nil {
		t.Fatalf("fbDeleteUser: %v", err)
	}
	if fid, _ := fake.byName("alu1"); fid != 0 {
		t.Error("a conta segue aí despois de borrala")
	}
	if err := a.fbDeleteUser(id); err == nil {
		t.Error("borrar un id que xa non existe debería dar erro (o servidor dá 500)")
	}
}
