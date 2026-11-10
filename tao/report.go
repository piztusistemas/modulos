package main

import (
	"crypto"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/digitorus/pdfsign/sign"
)

// signerNamePlaceholder is the token the frontend leaves in the report wherever the
// signer's name should appear; ExportSignedReport replaces it with the name read
// from the certificate. It must match the constant used in ReportBuilder.tsx.
const signerNamePlaceholder = "@@ASINANTE@@"

// htmlEscape escapes the characters that matter inside the report's HTML text nodes
// (mirroring the frontend's esc), so a name with & < > is inserted safely.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// fixDoubleEncodedUTF8 repairs a string whose UTF-8 bytes were mis-decoded as
// Latin-1, a common problem with certificate subjects (e.g. "MartÃ­n" → "Martín").
// It reinterprets each rune as a single byte and, only when the result is valid
// UTF-8, decodes it; a correctly-encoded name (where the reinterpretation would be
// invalid UTF-8) is returned unchanged.
func fixDoubleEncodedUTF8(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return s // contains a genuinely non-Latin-1 rune; not mojibake
		}
		b = append(b, byte(r))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return s
}

// ExportSignedReport renders the given report HTML to an A4 PDF (via the system's
// headless Chromium, see browser.go), signs it cryptographically (PAdES) with the
// logged-in student's certificate, and uploads it to the File Browser server — it is
// never saved to local disk or offered through a save dialog: the student's copy is
// whichever they later download from the server, same as any other file (see
// traballos.go). That keeps every signed report in one place regardless of which
// machine generated it.
//
// The certificate is unlocked with the password of the session itself (a.fbPassword):
// every certificate is issued protected with its owner's login password — by the admin
// when creating the account or changing the password, and by RegenerateCertificate
// after a self-service change — so the student has already typed it to get in and is
// not asked for it again. It never leaves Go.
//
// The visible vertical signature in the left margin is drawn by the report HTML
// itself (a position:fixed band repeated on every printed page); this function
// adds the cryptographic signature that makes the document verifiable and tamper
// evident.
//
// materiaID/sessionDate are only set when the report covers a single session (the
// ReportBuilder "session" scope, including the shortcut opened from that session's
// DayModal): when they are, the signed PDF is also attached to that session's own
// work files (fbWorkRoot), so it shows up alongside the student's other uploads — and
// is downloadable the same way — without them having to upload it by hand. Any other
// scope (week/range/unit/evaluation) leaves them empty, since there is no single
// session to attach the combined report to; it still lands in users/<login>/informes/.
//
// It also returns a LOCAL path to the same signed bytes, in the system temp dir, so the
// frontend can offer an "Abrir" button right after generating it (OpenFile, app.go) —
// without a second round-trip to the server or a save dialog just to look at what was
// just created. That temp copy is not cleaned up here: it needs to outlive this call
// for OpenFile to still find it, same tradeoff as the browser's own download cache.
func (a *App) ExportSignedReport(html, suggestedName, materiaID, sessionDate string) (string, error) {
	if a.profile == nil {
		return "", fmt.Errorf("inicia sesión antes de xerar o informe")
	}
	if !a.profile.HasCert || a.profile.CertPath == "" {
		return "", fmt.Errorf("non hai certificado dixital dispoñible para este alumno/a")
	}
	if a.fbPassword == "" {
		return "", fmt.Errorf("a sesión non ten o contrasinal necesario para asinar; volve iniciar sesión")
	}

	// Load and decrypt the certificate first, so we fail fast on a wrong password.
	signer, cert, chains, err := loadSigner(a.profile.CertPath, a.fbPassword)
	if err != nil {
		// The two passwords only drift apart when a certificate was left behind by a
		// password change (the reissue is automatic, but it can fail and only warn).
		// Say what to do rather than report a bare "wrong password" the student cannot
		// act on, having typed no password here.
		return "", fmt.Errorf("non se puido abrir o teu certificado co contrasinal da sesión. "+
			"Probablemente quedou dun contrasinal anterior: entra en Configuración e rexenérao. (%w)", err)
	}

	// The signer's display name comes from the certificate subject (the authoritative
	// signing identity), since File Browser only exposes the login name. Fall back to
	// the profile and finally the username. The frontend leaves a placeholder in the
	// report for this name, which we fill in before rendering.
	signerName := fixDoubleEncodedUTF8(strings.TrimSpace(cert.Subject.CommonName))
	if signerName == "" {
		signerName = strings.TrimSpace(a.profile.Name + " " + a.profile.Surname)
	}
	if signerName == "" {
		signerName = a.profile.Username
	}
	html = strings.ReplaceAll(html, signerNamePlaceholder, htmlEscape(signerName))

	// 1) Render the HTML to a PDF with the system's headless Chromium.
	pdfBytes, err := renderizarPDF(html)
	if err != nil {
		return "", err
	}

	// 2) Write the unsigned PDF to a temporary file (pdfsign works on files).
	tmpIn, err := os.CreateTemp("", "dixit-*-unsigned.pdf")
	if err != nil {
		return "", fmt.Errorf("non se puido crear o ficheiro temporal: %w", err)
	}
	tmpPath := tmpIn.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpIn.Write(pdfBytes); err != nil {
		tmpIn.Close()
		return "", err
	}
	tmpIn.Close()

	if suggestedName == "" {
		suggestedName = "informe.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(suggestedName), ".pdf") {
		suggestedName += ".pdf"
	}

	// 3) Sign the PDF into a second temporary file — nothing is written to a
	// user-chosen location; the signed bytes only ever leave this function towards
	// the server (and the local-open copy below).
	tmpOut, err := os.CreateTemp("", "dixit-*-signed.pdf")
	if err != nil {
		return "", fmt.Errorf("non se puido crear o ficheiro temporal: %w", err)
	}
	tmpSignedPath := tmpOut.Name()
	tmpOut.Close()
	defer os.Remove(tmpSignedPath)

	signData := sign.SignData{
		Signature: sign.SignDataSignature{
			CertType:   sign.CertificationSignature,
			DocMDPPerm: sign.DoNotAllowAnyChangesPerms,
			Info: sign.SignDataSignatureInfo{
				Name:   signerName,
				Reason: "Sinatura das prácticas do alumnado",
				Date:   time.Now(),
			},
		},
		Signer:            signer,
		DigestAlgorithm:   crypto.SHA256,
		Certificate:       cert,
		CertificateChains: chains,
	}

	if err := sign.SignFile(tmpPath, tmpSignedPath, signData); err != nil {
		return "", fmt.Errorf("non se puido asinar o PDF: %w", err)
	}

	signed, err := os.ReadFile(tmpSignedPath)
	if err != nil {
		return "", fmt.Errorf("non se puido ler o PDF asinado: %w", err)
	}

	// 4) Upload the signed PDF to the student's own folder (users/<login>/informes/) —
	// this IS the save, not a backup of a local file, so a failure here must be
	// reported: there is no local copy left to fall back to.
	remoteName := time.Now().Format("20060102-150405") + "_" + suggestedName
	informesDir := a.fbUserDir() + "/" + fbInformesRoot
	if err := a.fbEnsureWorkDir(informesDir); err != nil {
		return "", fmt.Errorf("non se puido preparar o cartafol de informes no servidor: %w", err)
	}
	if err := a.fbUpload(informesDir+"/"+remoteName, signed); err != nil {
		return "", fmt.Errorf("non se puido gardar o informe asinado no servidor: %w", err)
	}

	// Report of a single session: also attach it to that session's own work files
	// (fbWorkRoot), so it appears alongside whatever the student uploaded and can be
	// downloaded the same way. Best-effort: the informes/ copy above already
	// succeeded, so a failure here only costs the convenience of finding it there too.
	if materiaID != "" && sessionDate != "" {
		if dir, err := workDir(a.fbUser, materiaID, sessionDate); err != nil {
			log.Printf("[informes] non se puido compor o cartafol de traballos para adxuntar o informe de %q: %v", a.fbUser, err)
		} else if err := a.fbEnsureWorkDir(dir); err != nil {
			log.Printf("[informes] non se puido preparar o cartafol de traballos de %q para adxuntar o informe: %v", a.fbUser, err)
		} else if err := a.fbUpload(dir+"/"+suggestedName, signed); err != nil {
			log.Printf("[informes] non se puido adxuntar o informe asinado aos ficheiros da sesión de %q: %v", a.fbUser, err)
		}
	}

	// 5) Keep a local copy of the very same signed bytes for the "Abrir" button (see
	// doc comment above) — best-effort: the server copy already succeeded, so a
	// failure here only costs the convenience of viewing it right away.
	localPath, err := writeLocalReportCopy(suggestedName, signed)
	if err != nil {
		log.Printf("[informes] non se puido gardar a copia local do informe asinado: %v", err)
		return "", nil
	}
	return localPath, nil
}

// writeLocalReportCopy writes content to a fresh temp file named after suggestedName
// (kept as a hint only, so OpenFile's system dialog shows something readable) and
// returns its path. Unlike the mid-process temp files above, this one is NOT removed:
// it has to outlive ExportSignedReport for the frontend's later OpenFile call to find
// it.
func writeLocalReportCopy(suggestedName string, content []byte) (string, error) {
	pattern := "informe-asinado-*-" + suggestedName
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := f.Name()
	_, werr := f.Write(content)
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}
	return path, nil
}

// loadSigner decrypts a PKCS#12 certificate and returns the crypto.Signer, the
// signing certificate and its chain, ready for pdfsign.
func loadSigner(p12Path, password string) (crypto.Signer, *x509.Certificate, [][]*x509.Certificate, error) {
	raw, err := os.ReadFile(p12Path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("non se puido ler o certificado (%s): %w", filepath.Base(p12Path), err)
	}
	key, cert, caCerts, err := pkcs12.DecodeChain(raw, password)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("non se puido abrir o certificado (contrasinal incorrecto?): %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, nil, nil, fmt.Errorf("a chave do certificado non serve para asinar")
	}
	chain := append([]*x509.Certificate{cert}, caCerts...)
	return signer, cert, [][]*x509.Certificate{chain}, nil
}
