package main

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitorus/pdfsign/sign"
	"github.com/digitorus/pdfsign/verify"
)

func TestFixDoubleEncodedUTF8(t *testing.T) {
	// mojibake reproduces a UTF-8 string mis-decoded as Latin-1 (each byte → rune).
	mojibake := func(s string) string {
		var sb strings.Builder
		for _, b := range []byte(s) {
			sb.WriteRune(rune(b))
		}
		return sb.String()
	}
	correct := "Martín Pardiñas Fondo"
	if got := fixDoubleEncodedUTF8(mojibake(correct)); got != correct {
		t.Errorf("reparación: got %q, quere %q", got, correct)
	}
	if got := fixDoubleEncodedUTF8(correct); got != correct {
		t.Errorf("preservación: got %q, quere %q", got, correct)
	}
	if got := fixDoubleEncodedUTF8("Ana Gomez"); got != "Ana Gomez" {
		t.Errorf("ascii: got %q", got)
	}
}

// TestSignPipeline exercises the risky end-to-end path: generate a certificate,
// render HTML to a PDF with headless Chromium, and sign it (PAdES). It validates
// that Chromium's PDF output is compatible with the pdfsign library.
func TestSignPipeline(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "test.p12")
	if err := generateSelfSignedP12(certPath, "Proba Alumno", "1234"); err != nil {
		t.Fatalf("xerar p12: %v", err)
	}

	html := `<!doctype html><html><head><meta charset="utf-8">
	<style>@page{size:A4;margin:16mm 16mm 16mm 16mm}
	.page-signature{position:fixed;top:0;bottom:0;left:-13mm;width:12mm;writing-mode:vertical-rl;transform:rotate(180deg);font-size:8px}
	</style></head><body>
	<div class="page-signature">Asinado por Proba Alumno · 11/07/2026</div>
	<h1>Informe de proba</h1><p>Contido da <b>práctica</b> con parágrafo longo.</p>
	<div style="page-break-before:always"></div><h2>Segunda páxina</h2><p>Máis contido.</p>
	</body></html>`

	pdfBytes, err := renderizarPDF(html)
	if err != nil {
		t.Fatalf("render HTML a PDF: %v", err)
	}
	if len(pdfBytes) < 1000 || !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("saída non parece un PDF (%d bytes)", len(pdfBytes))
	}

	unsigned := filepath.Join(dir, "unsigned.pdf")
	if err := os.WriteFile(unsigned, pdfBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	signer, cert, chains, err := loadSigner(certPath, "1234")
	if err != nil {
		t.Fatalf("cargar certificado: %v", err)
	}

	signed := filepath.Join(dir, "signed.pdf")
	signData := sign.SignData{
		Signature: sign.SignDataSignature{
			CertType:   sign.CertificationSignature,
			DocMDPPerm: sign.DoNotAllowAnyChangesPerms,
			Info:       sign.SignDataSignatureInfo{Name: "Proba Alumno", Reason: "Proba"},
		},
		Signer:            signer,
		DigestAlgorithm:   crypto.SHA256,
		Certificate:       cert,
		CertificateChains: chains,
	}
	if err := sign.SignFile(unsigned, signed, signData); err != nil {
		t.Fatalf("asinar PDF: %v", err)
	}

	out, err := os.ReadFile(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("/Type /Sig")) && !bytes.Contains(out, []byte("adbe.pkcs7")) {
		t.Fatalf("o PDF asinado non contén un dicionario de sinatura")
	}

	// Cryptographically verify the signature integrity. The demo cert is
	// self-signed, so we allow untrusted roots and its EmailProtection EKU.
	f, err := os.Open(signed)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	opts := verify.DefaultVerifyOptions()
	opts.AllowUntrustedRoots = true
	opts.AllowedEKUs = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}
	resp, err := verify.VerifyFileWithOptions(f, opts)
	if err != nil {
		t.Fatalf("verificar sinatura: %v", err)
	}
	if len(resp.Signers) == 0 {
		t.Fatalf("non se atopou ningún asinante no PDF")
	}
	if !resp.Signers[0].ValidSignature {
		t.Fatalf("a sinatura non é válida criptograficamente")
	}
	t.Logf("OK: PDF asinado de %d bytes, sinatura válida por %q", len(out), resp.Signers[0].Name)

	// Wrong password must fail cleanly.
	if _, _, _, err := loadSigner(certPath, "incorrecto"); err == nil {
		t.Fatalf("agardábase erro con contrasinal incorrecto")
	}
}

// ---- Asinar co contrasinal da sesión ---------------------------------------------
//
// The report no longer asks for the certificate's password: it is unlocked with the
// one of the session. These tests pin the two halves of that promise — the session
// password does open a certificate issued the way the app issues them, and a
// certificate left over from an older password fails with a message that says what to
// do about it, since the student typed no password to blame.

// certForPassword writes a .p12 protected with p12Password, as every path in the app
// issues them (admin_fb.go on create/change, RegenerateCertificate on a self change).
func certForPassword(t *testing.T, p12Password string) string {
	t.Helper()
	dir := t.TempDir()
	pfx, err := generateStudentP12(dir, "Ana", "Pérez Gómez", p12Password)
	if err != nil {
		t.Fatalf("generateStudentP12: %v", err)
	}
	path := filepath.Join(dir, "alumna.p12")
	if err := os.WriteFile(path, pfx, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSessionPasswordOpensTheCertificate(t *testing.T) {
	const password = "acceso-do-alumno"
	path := certForPassword(t, password)

	if _, _, _, err := loadSigner(path, password); err != nil {
		t.Fatalf("o contrasinal da sesión debe abrir o certificado: %v", err)
	}
	if _, _, _, err := loadSigner(path, "outro-contrasinal"); err == nil {
		t.Fatal("un contrasinal distinto non debe abrir o certificado")
	}
}

func TestExportSignedReportGuards(t *testing.T) {
	html := "<html><body>informe</body></html>"

	t.Run("sen sesión", func(t *testing.T) {
		if _, err := NewApp().ExportSignedReport(html, "informe.pdf", "", ""); err == nil {
			t.Fatal("esperábase erro sen sesión iniciada")
		}
	})

	t.Run("sen certificado", func(t *testing.T) {
		a := NewApp()
		a.profile = &Profile{Username: "alumna", Connected: true}
		a.fbPassword = "acceso"
		_, err := a.ExportSignedReport(html, "informe.pdf", "", "")
		if err == nil || !strings.Contains(err.Error(), "certificado") {
			t.Fatalf("erro = %v, esperábase que falase do certificado que falta", err)
		}
	})

	t.Run("sesión sen contrasinal en memoria", func(t *testing.T) {
		a := NewApp()
		a.profile = &Profile{Username: "alumna", Connected: true, HasCert: true, CertPath: certForPassword(t, "acceso")}
		_, err := a.ExportSignedReport(html, "informe.pdf", "", "")
		if err == nil || !strings.Contains(err.Error(), "iniciar sesión") {
			t.Fatalf("erro = %v, esperábase que pedise volver iniciar sesión", err)
		}
	})

	// O certificado quedou dun contrasinal anterior: como aquí xa non se escribe ningún
	// contrasinal, un "contrasinal incorrecto" a secas non lle diría nada ao alumnado.
	t.Run("certificado dun contrasinal vello", func(t *testing.T) {
		a := NewApp()
		a.profile = &Profile{Username: "alumna", Connected: true, HasCert: true, CertPath: certForPassword(t, "contrasinal-vello")}
		a.fbPassword = "contrasinal-novo"
		_, err := a.ExportSignedReport(html, "informe.pdf", "", "")
		if err == nil {
			t.Fatal("esperábase erro: o certificado non abre co contrasinal da sesión")
		}
		if !strings.Contains(err.Error(), "rexenérao") || !strings.Contains(err.Error(), "Configuración") {
			t.Errorf("erro = %q, esperábase que dixese como arranxalo (rexenerar en Configuración)", err)
		}
	})
}
