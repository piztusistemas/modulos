package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// Client-side certificate issuance.
//
// The department Certificate Authority lives inside the program (ca/ca.crt +
// ca/ca.key, embedded below). When a student changes their password the app issues
// a fresh personal certificate signed by this CA, packages it as a password-protected
// PKCS#12 (.p12) and uploads it to the student's own folder on the File Browser server.
// This mirrors the server-side xeradorCertificados.sh but runs entirely on the client.
//
// SECURITY NOTE: shipping the CA private key in a client binary means anyone with the
// binary can extract it and forge certificates. This is acceptable only for a trusted,
// internal (department) deployment.

//go:embed ca/ca.crt
var caCertPEM []byte

//go:embed ca/ca.key
var caKeyPEM []byte

// caDir returns the folder that holds a locally generated Certificate Authority
// (dataDir/ca). The admin panel writes ca.crt + ca.key there via generateCA.
func caDir(dataDir string) string { return filepath.Join(dataDir, "ca") }

// loadCA parses the embedded department Certificate Authority. It is kept for the
// tests and callers that only need the built-in CA; the running app uses loadCAFrom.
func loadCA() (*x509.Certificate, *rsa.PrivateKey, error) { return loadCAFrom("") }

// loadCAFrom parses the Certificate Authority used to sign student certificates.
// A CA generated from the admin panel (dataDir/ca/ca.{crt,key}) takes precedence;
// when it is absent the CA embedded in the binary is used, so existing deployments
// keep working without any configuration.
func loadCAFrom(dataDir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, keyPEM := caCertPEM, caKeyPEM
	if dataDir != "" {
		cp := filepath.Join(caDir(dataDir), "ca.crt")
		kp := filepath.Join(caDir(dataDir), "ca.key")
		if c, cerr := os.ReadFile(cp); cerr == nil {
			if k, kerr := os.ReadFile(kp); kerr == nil {
				certPEM, keyPEM = c, k
			}
		}
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("o certificado da CA non é válido")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("non se puido ler o certificado da CA: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("a chave da CA non é válida")
	}
	caKey, err := parseRSAPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("non se puido ler a chave da CA: %w", err)
	}
	return caCert, caKey, nil
}

// generateCA creates a department Certificate Authority (RSA 4096, self-signed,
// valid for 5 years) and writes it to dataDir/ca as ca.key + ca.crt. It reproduces
// the reference openssl script: subject C=ES/ST=Galicia/O=<center>/OU=<department>
// and CN "CA <department>". Overwrites any previous CA in that folder.
func generateCA(dataDir, center, department string) error {
	center = strings.TrimSpace(center)
	department = strings.TrimSpace(department)
	if center == "" || department == "" {
		return fmt.Errorf("indica o nome do centro e do departamento antes de xerar a CA")
	}

	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Country:            []string{"ES"},
			Province:           []string{"Galicia"},
			Organization:       []string{center},
			OrganizationalUnit: []string{department},
			CommonName:         "CA " + department,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0), // 1825 días, como o script
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("non se puido crear o certificado da CA: %w", err)
	}

	dir := caDir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), keyPEM, 0o600); err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), certPEM, 0o644); err != nil {
		return err
	}
	return nil
}

// parseRSAPrivateKey accepts both PKCS#1 ("RSA PRIVATE KEY", as `openssl genrsa`
// emits) and PKCS#8 ("PRIVATE KEY") encodings.
func parseRSAPrivateKey(der []byte) (*rsa.PrivateKey, error) {
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, fmt.Errorf("a chave da CA non é RSA")
	}
	return nil, fmt.Errorf("formato de chave descoñecido")
}

// certFileBase reproduces xeradorCertificados.sh's file naming: "nome_apelidos" in
// lowercase with every space removed (e.g. "Brais", "Castro Rodriguez" ->
// "brais_castrorodriguez"). Using the same convention means the uploaded .p12
// overwrites any previously generated one for the same student.
func certFileBase(name, surname string) string {
	joined := strings.ToLower(name + "_" + surname)
	return strings.ReplaceAll(joined, " ", "")
}

// generateStudentP12 issues a personal certificate for the given student, signed by
// the department CA, and returns it as a PKCS#12 archive protected by p12Password.
// The CA certificate is bundled so signature validators can build the chain. The CA
// is loaded from dataDir (a CA generated in the admin panel) when present, otherwise
// from the embedded one; the certificate's Organization/OU are inherited from the
// signing CA so the student certificate always matches the configured centre/department.
func generateStudentP12(dataDir, name, surname, p12Password string) ([]byte, error) {
	caCert, caKey, err := loadCAFrom(dataDir)
	if err != nil {
		return nil, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Country:            []string{"ES"},
			Province:           []string{"Galicia"},
			Organization:       caCert.Subject.Organization,
			OrganizationalUnit: caCert.Subject.OrganizationalUnit,
			CommonName:         strings.TrimSpace(name + " " + surname),
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0), // 365 días, como o script
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("non se puido asinar o certificado: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	pfx, err := pkcs12.Modern.Encode(key, cert, []*x509.Certificate{caCert}, p12Password)
	if err != nil {
		return nil, fmt.Errorf("non se puido empaquetar o certificado (.p12): %w", err)
	}
	return pfx, nil
}
