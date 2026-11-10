package main

import (
	"crypto/x509"
	"testing"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

func TestGenerateStudentP12(t *testing.T) {
	pfx, err := generateStudentP12("", "Brais", "Castro Rodriguez", "alumno2026")
	if err != nil {
		t.Fatalf("generateStudentP12: %v", err)
	}

	// The wrong password must not open the archive.
	if _, _, _, err := pkcs12.DecodeChain(pfx, "malo"); err == nil {
		t.Fatal("esperábase erro co contrasinal incorrecto")
	}

	_, cert, caCerts, err := pkcs12.DecodeChain(pfx, "alumno2026")
	if err != nil {
		t.Fatalf("DecodeChain: %v", err)
	}
	if cert.Subject.CommonName != "Brais Castro Rodriguez" {
		t.Fatalf("CN inesperado: %q", cert.Subject.CommonName)
	}
	if len(caCerts) == 0 {
		t.Fatal("o .p12 debería incluír o certificado da CA")
	}

	// The issued certificate must chain up to the embedded department CA.
	caCert, _, err := loadCA()
	if err != nil {
		t.Fatalf("loadCA: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Fatalf("o certificado non chega á CA do departamento: %v", err)
	}
}

func TestCertFileBase(t *testing.T) {
	if got := certFileBase("Brais", "Castro Rodriguez"); got != "brais_castrorodriguez" {
		t.Fatalf("certFileBase = %q, quere %q", got, "brais_castrorodriguez")
	}
}
