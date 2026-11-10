package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// Real (server-side) privacy enforcement for notas-privadas/.
//
// Every File Browser account — alumno or profesor, there is no server-side distinction,
// see profesor.go's "Professor mode" comment — is scoped to the whole "arquivos" source
// (fbCreateUser, admin_fb.go), so in principle any authenticated account could read any
// path on the server directly through the File Browser API, bypassing Tao's own
// requireOwnsMateria/requireProfessor checks entirely. That is an accepted, pre-existing
// trust boundary for everything else shared between professors (e.g. one professor's
// materia JSON is technically readable by another's account this way too), but it is NOT
// acceptable for private notes on a student (notas_privadas.go): those must be unreadable
// by the student no matter which door they use.
//
// The fix is an ACCESS RULE on the notas-privadas/ path that denies every account that is
// NOT a professor, at the server itself. New accounts get it from fbCreateUser (which
// fails, and rolls the account back, if it cannot be set: an account without it would
// leak); SetProfessor keeps it in sync when a role changes (granted professors lose it,
// demoted ones regain it); deleteUserLocked drops it with the account;
// ApplyPrivacyRuleToAllStudents backfills every account that existed before.
//
// (No File Browser clásico a mesma idea implémentase con regras regex dentro de cada
// usuario: ver privacidade_clasico.go. fbSetUserPrivacyRule elixe unha ou outra.)
//
// COMO É EN QUANTUM — comprobado contra v1.5.6. As regras non van dentro do usuario (como
// no File Browser clásico, con regex) senón por CAMIÑO: POST /api/access?source=&path=
// con {"allow":false,"ruleCategory":"user","value":"<usuario>"} nega ese usuario en todo
// o subárbore (403 nos ficheiros de dentro; o cartafol pai e o resto seguen a 200). Non
// hai regex nin o bug do "regexp.raw" que había no clásico. Detalles que importan:
//   - Unha regra por usuario aplícase ao instante, ao poñela e ao quitala. Probouse
//     tamén con UN grupo "alumnado" e unha única regra, pero Quantum cachea o acceso
//     derivado de grupos: un alumno que se engade ao grupo despois de xa ter accedido
//     seguía a ler notas-privadas durante minutos. Por iso, regras por usuario.
//   - O camiño da regra pode non existir aínda (nunha aula nova notas-privadas/ créase co
//     primeiro apunte): a regra vale igual cando o cartafol aparece.
//   - Poñer unha regra que xa existe dá 500 "the resource already exists", e quitar unha
//     que non existe dá 404: ambos os dous se tratan como éxito (o estado xa é o pedido).
//   - Ao borrar un usuario a súa regra NON se borra soa, e un usuario novo co mesmo nome
//     herdaríaa; por iso deleteUserLocked a limpa.

// fbAccessURL builds base + /api/access?source=…&path=/dixitalizacion/notas-privadas plus
// any extra query parameters (the ones DELETE needs to name the rule).
func fbAccessURL(path string, extra url.Values) string {
	q := url.Values{}
	q.Set("source", fbSource)
	q.Set("path", path)
	for k, v := range extra {
		q[k] = v
	}
	return "/api/access?" + q.Encode()
}

// fbSetUserPrivacyRule denies (deny=true, the alumno default) or allows (deny=false, what
// a professor needs) username access to notas-privadas/. It is idempotent: asking for the
// state the account is already in succeeds.
func (a *App) fbSetUserPrivacyRule(username string, deny bool) error {
	if fbSaborDe(a.fbURL) == fbClasico {
		return a.fbSetUserPrivacyRuleClasico(username, deny)
	}
	if deny {
		status, body, err := a.fbAdminCall(http.MethodPost, fbAccessURL(notasPrivadasRulePath, nil), map[string]any{
			"allow": false, "ruleCategory": "user", "value": username,
		})
		if err != nil {
			return err
		}
		switch {
		case status == http.StatusOK, status == http.StatusCreated, status == http.StatusNoContent:
			return nil
		case strings.Contains(string(body), "already exists"):
			return nil
		}
		return fmt.Errorf("o servidor rexeitou a regra de notas privadas (%d): %s", status, strings.TrimSpace(string(body)))
	}

	status, body, err := a.fbAdminCall(http.MethodDelete, fbAccessURL(notasPrivadasRulePath, url.Values{
		"ruleType": {"deny"}, "ruleCategory": {"user"}, "value": {username},
	}), nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("o servidor rexeitou quitar a regra de notas privadas (%d): %s", status, strings.TrimSpace(string(body)))
}

// ApplyPrivacyRuleToAllStudents backfills the notas-privadas/ deny rule onto every
// account that existed before this feature shipped (fbCreateUser only applies it to
// NEW accounts). Skips professors (dixitalizacion/profesores.json) and the
// administration accounts, which must keep access. Returns how many accounts were
// updated. Admin only; meant to be run once from the admin panel after upgrading.
func (a *App) ApplyPrivacyRuleToAllStudents() (int, error) {
	var updated int
	err := a.withAdminFB(func() error {
		var profs []string
		a.fbReadSharedJSON(sharedProfesores, &profs)
		isProf := map[string]bool{}
		for _, p := range profs {
			isProf[strings.ToLower(strings.TrimSpace(p))] = true
		}
		cfg, _ := a.GetAdminConfig()
		protected := map[string]bool{strings.ToLower(directorUser): true}
		if cfg != nil {
			protected[strings.ToLower(strings.TrimSpace(cfg.FBAdminUser))] = true
		}

		users, err := a.fbListUsers()
		if err != nil {
			return err
		}
		for _, u := range users {
			name, _ := u["username"].(string)
			name = strings.TrimSpace(name)
			if name == "" || isProf[strings.ToLower(name)] || protected[strings.ToLower(name)] || fbUserIsAdmin(u) {
				continue
			}
			if err := a.fbSetUserPrivacyRule(name, true); err != nil {
				log.Printf("[privacidade] non se puido aplicar a regra de notas privadas a %q: %v", name, err)
				continue
			}
			updated++
		}
		return nil
	})
	return updated, err
}
