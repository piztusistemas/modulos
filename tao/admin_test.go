package main

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// TestParseUsersCSV checks the reference column layout is parsed and mapped, with
// both comma- and semicolon-separated variants.
func TestParseUsersCSV(t *testing.T) {
	header := "username,password,firstname,lastname,email,city,departament,profile_field_grupo"
	row := "iker.rodriguezmouzo,Dombate(28),Iker,Rodríguez Mouzo,nonresponder@edu.xunta.gal,Vimianzo,eso4a,eso4a"

	for _, tc := range []struct{ name, text string }{
		{"comma", header + "\n" + row + "\n"},
		{"semicolon", replaceComma(header) + "\n" + replaceComma(row) + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, err := parseUsersCSV(tc.text)
			if err != nil {
				t.Fatalf("parseUsersCSV: %v", err)
			}
			if len(users) != 1 {
				t.Fatalf("esperábase 1 usuario, obtívose %d", len(users))
			}
			u := users[0]
			if u.Username != "iker.rodriguezmouzo" || u.Password != "Dombate(28)" {
				t.Fatalf("credenciais mal mapeadas: %+v", u)
			}
			if u.Firstname != "Iker" || u.Lastname != "Rodríguez Mouzo" {
				t.Fatalf("nome/apelidos mal mapeados: %+v", u)
			}
			if u.Group != "eso4a" || u.City != "Vimianzo" || u.Email != "nonresponder@edu.xunta.gal" {
				t.Fatalf("campos extra mal mapeados: %+v", u)
			}
		})
	}
}

// replaceComma swaps ',' for ';' to build the semicolon-separated fixture. The sample
// data has no embedded commas, so a plain replacement is faithful.
func replaceComma(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r == ',' {
			out[i] = ';'
		}
	}
	return string(out)
}

func TestParseUsersCSVMissingUsername(t *testing.T) {
	if _, err := parseUsersCSV("password,firstname\nx,y\n"); err == nil {
		t.Fatal("esperábase erro cando falta a columna 'username'")
	}
}

// TestGenerateCAAndSign generates a CA in a temp dataDir, then issues a student
// certificate and verifies it chains up to that generated CA (not the embedded one).
func TestGenerateCAAndSign(t *testing.T) {
	dir := t.TempDir()
	if err := generateCA(dir, "IES de Proba", "Departamento de Proba"); err != nil {
		t.Fatalf("generateCA: %v", err)
	}

	// loadCAFrom must prefer the locally generated CA.
	caCert, _, err := loadCAFrom(dir)
	if err != nil {
		t.Fatalf("loadCAFrom: %v", err)
	}
	if !caCert.IsCA {
		t.Fatal("o certificado da CA xerada non ten IsCA=true")
	}
	if got := caCert.Subject.Organization; len(got) == 0 || got[0] != "IES de Proba" {
		t.Fatalf("O= da CA inesperado: %v", got)
	}
	if caCert.Subject.CommonName != "CA Departamento de Proba" {
		t.Fatalf("CN da CA inesperado: %q", caCert.Subject.CommonName)
	}

	pfx, err := generateStudentP12(dir, "Iker", "Rodriguez Mouzo", "segredo123")
	if err != nil {
		t.Fatalf("generateStudentP12: %v", err)
	}
	_, cert, _, err := pkcs12.DecodeChain(pfx, "segredo123")
	if err != nil {
		t.Fatalf("DecodeChain: %v", err)
	}
	// The student certificate inherits O/OU from the generated CA.
	if got := cert.Subject.Organization; len(got) == 0 || got[0] != "IES de Proba" {
		t.Fatalf("O= do certificado do alumno inesperado: %v", got)
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Fatalf("o certificado non chega á CA xerada: %v", err)
	}
}

// TestLoadCAFallsBackToEmbedded checks that with no local CA the embedded one is used.
func TestLoadCAFallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	// Ensure there is no local CA file.
	if _, err := os.Stat(filepath.Join(caDir(dir), "ca.crt")); err == nil {
		t.Fatal("non debería existir unha CA local no directorio temporal")
	}
	caCert, _, err := loadCAFrom(dir)
	if err != nil {
		t.Fatalf("loadCAFrom (embebida): %v", err)
	}
	if caCert.Subject.CommonName != "CA Departamento de Tecnoloxia" {
		t.Fatalf("esperábase a CA embebida, obtívose CN=%q", caCert.Subject.CommonName)
	}
}

// TestDirectorCicloCompleto cubre o fluxo enteiro do acceso de Director: sen
// configurar → configurar → login correcto → login con contrasinal errado →
// non se pode reconfigurar unha segunda vez.
func TestDirectorCicloCompleto(t *testing.T) {
	a := &App{dataDir: t.TempDir()}

	if a.DirectorConfigurado() {
		t.Fatal("non debería haber Director configurado nun dataDir novo")
	}

	if err := a.ConfigurarDirector("curto"); err == nil {
		t.Fatal("esperábase erro cun contrasinal de menos de 8 caracteres")
	}
	if a.DirectorConfigurado() {
		t.Fatal("un ConfigurarDirector fallido non debería deixar director.json escrito")
	}

	if err := a.ConfigurarDirector("segredo123"); err != nil {
		t.Fatalf("ConfigurarDirector: %v", err)
	}
	if !a.DirectorConfigurado() {
		t.Fatal("DirectorConfigurado debería ser true tras ConfigurarDirector")
	}

	if err := a.ConfigurarDirector("outro-contrasinal"); err == nil {
		t.Fatal("esperábase erro ao reconfigurar un Director xa existente")
	}

	if _, err := a.AdminLogin("director", "segredo123"); err != nil {
		t.Fatalf("AdminLogin co contrasinal correcto: %v", err)
	}
	if !a.isAdmin {
		t.Fatal("AdminLogin correcto debería marcar isAdmin=true")
	}

	b := &App{dataDir: a.dataDir}
	if _, err := b.AdminLogin("director", "contrasinal-errado"); err == nil {
		t.Fatal("esperábase erro cun contrasinal incorrecto")
	}
	if b.isAdmin {
		t.Fatal("un AdminLogin fallido non debería marcar isAdmin=true")
	}

	// Case-insensitive no nome de usuario, coma o resto do panel de administración.
	if _, err := b.AdminLogin("DIRECTOR", "segredo123"); err != nil {
		t.Fatalf("AdminLogin debería ser case-insensitive no usuario: %v", err)
	}
}
