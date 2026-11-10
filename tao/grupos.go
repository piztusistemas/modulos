package main

import "strings"

// Grupos dun perfil.
//
// O campo "grupo" dun alumno (columna profile_field_grupo do CSV de matrícula, campo
// Grupo do panel de administración) pode conter VARIOS grupos separados por comas
// —"eso3a, eso3b"—, que é como se dan de alta as contas que asisten a máis dun grupo.
// No roster (alumnos.json) gárdase tal cal se escribiu.
//
// Por iso ningún sitio debe comparar ese campo como unha cadea única: facelo deixa a
// ese alumnado sen materia ("Aínda non tes materias asignadas"), sen muro e sen
// compañeiros, porque "eso3a, eso3b" non é igual a "eso3a" nin a "eso3b". Todas as
// comparacións de grupo pasan por estas tres funcións.

// gruposDe divide o campo de grupo dun perfil nos grupos individuais, sen baleiros.
func gruposDe(campo string) []string {
	var out []string
	for _, g := range strings.FieldsFunc(campo, func(r rune) bool { return r == ',' || r == ';' }) {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// pertenceAGrupo indica se o campo de grupo dun perfil inclúe grupo, sen distinguir
// maiúsculas nin espazos sobrantes.
func pertenceAGrupo(campo, grupo string) bool {
	grupo = strings.TrimSpace(grupo)
	if grupo == "" {
		return false
	}
	for _, g := range gruposDe(campo) {
		if strings.EqualFold(g, grupo) {
			return true
		}
	}
	return false
}

// compartenGrupo indica se dous campos de grupo teñen algún grupo en común.
func compartenGrupo(campoA, campoB string) bool {
	for _, g := range gruposDe(campoA) {
		if pertenceAGrupo(campoB, g) {
			return true
		}
	}
	return false
}
