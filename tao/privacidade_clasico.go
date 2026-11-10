package main

import (
	"fmt"
	"net/http"
	"strings"
)

// A versión CLÁSICA da privacidade de notas-privadas/ (a de Quantum está en
// privacidade.go; fbSetUserPrivacyRule elixe unha ou outra segundo o servidor).
//
// No File Browser clásico a regra é unha "rule" con regex (allow=false) gardada DENTRO
// do obxecto usuario, no seu campo "rules". Toda conta que non é profesora leva esa
// regra; as contas novas nacen con ela (fbCreateUserClasico), SetProfessor mantena en
// sincronía co rol e ApplyPrivacyRuleToAllStudents ponlla ás anteriores.
//
// FORMATO DA REGRA — comprobado contra File Browser v2.63.18 cunha conta de proba:
// cando "regex" é true, o servidor NON compila a expresión a partir de "path". Espera
// tamén o obxecto "regexp": {"raw": "<a expresión>"}, e é ese o que usa ao avaliala
// (rules.Rule.Matches chama r.Regexp.MatchString). Se falta, "regexp" queda a null e a
// primeira comprobación de acceso rebenta cun nil pointer dentro do servidor: File
// Browser pecha a conexión sen resposta e o cliente ve un "EOF" seco.
//
// Iso deixaba TODA conta con esta regra (é dicir, todo o alumnado) sen poder ler nada
// do servidor: Tao amosáballes "Aínda non tes materias asignadas", porque un fallo de
// lectura non se distinguía de "non hai materias". O profesorado non o notaba: as súas
// contas non levan a regra. Comprobado que coa regra completa o listado funciona e
// notas-privadas segue devolvendo 403 para o alumnado.

// fbRulesDenyingNotasPrivadas é o valor de "rules" coa que se crean as contas
// (fbCreateUserClasico): todo o mundo comeza sen poder ler notas-privadas/ ata que
// SetProfessor llo concede.
func fbRulesDenyingNotasPrivadas() []map[string]any {
	return []map[string]any{regraNotasPrivadas()}
}

// regraNotasPrivadas devolve a regra tal e como a espera File Browser. O "regexp.raw"
// non é opcional: ver a nota de formato de arriba.
func regraNotasPrivadas() map[string]any {
	return map[string]any{
		"allow":  false,
		"regex":  true,
		"path":   notasPrivadasRuleRegex,
		"regexp": map[string]any{"raw": notasPrivadasRuleRegex},
	}
}

// withoutNotasPrivadasRule quita de rules calquera regra que sexa a de notas privadas,
// para poder volver engadila (ou deixala fóra) sen duplicala. Calquera outra regra que
// un admin puxese á man na web de File Browser consérvase.
func withoutNotasPrivadasRule(rules []any) []any {
	kept := rules[:0]
	for _, r := range rules {
		if m, ok := r.(map[string]any); ok {
			if p, _ := m["path"].(string); p == notasPrivadasRuleRegex {
				continue
			}
		}
		kept = append(kept, r)
	}
	return kept
}

// fbSetUserPrivacyRuleClasico le o rexistro actual de username e reescribe o seu campo
// "rules": deny=true nega o acceso a notas-privadas/ (o defecto do alumnado), deny=false
// quita esa negación (o que precisa un profesor). Todo o demais da conta (scope, perm,
// hash do contrasinal…) queda intacto: mesmo patrón ler-modificar-escribir ca en
// fbAdminSetPasswordClasico.
func (a *App) fbSetUserPrivacyRuleClasico(username string, deny bool) error {
	id, err := a.fbUserIDByName(username)
	if err != nil {
		return err
	}
	user, err := a.fbGetUser(a.fbToken, id)
	if err != nil {
		return err
	}
	existing, _ := user["rules"].([]any)
	rules := withoutNotasPrivadasRule(existing)
	if deny {
		rules = append(rules, regraNotasPrivadas())
	}
	user["rules"] = rules

	status, data, err := a.fbAdminUserCallClasico(http.MethodPut, fbUserURL(a.fbURL, id), map[string]any{
		"what":             "user",
		"which":            []string{"rules"},
		"current_password": a.fbAdminPass,
		"data":             user,
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("o servidor rexeitou a actualización de regras (%d): %s",
			status, strings.TrimSpace(string(data)))
	}
	return nil
}
