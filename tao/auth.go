package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// Profile are the public student data returned to the frontend after login.
// It intentionally omits the password.
type Profile struct {
	Username   string `json:"username"`
	Name       string `json:"name"`
	Surname    string `json:"surname"`
	Group      string `json:"group"`
	CertPath   string `json:"certPath"` // absolute path to the .p12/.pfx certificate
	HasCert    bool   `json:"hasCert"`
	CertSource string `json:"certSource"` // "servidor", "local", "demo" or "" (none)
	Connected  bool   `json:"connected"`  // true when authenticated against a File Browser server
	Role       string `json:"role"`       // "profesor" or "alumno"; the fixed admin is resolved apart
}

// storedProfile is how a student record is persisted locally (Fase 1). In Fase 2
// this data will come from the server instead of profiles.json.
type storedProfile struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Surname  string `json:"surname"`
	Group    string `json:"group"`
	Email    string `json:"email,omitempty"`
	CertFile string `json:"certFile"`       // file name inside <dataDir>/certs
	UUID     string `json:"uuid,omitempty"` // internal identifier for anonymised work folders
}

func (a *App) profilesPath() string { return filepath.Join(a.dataDir, "profiles.json") }
func (a *App) certsDir() string     { return filepath.Join(a.dataDir, "certs") }

// loadProfiles reads the local reference profiles file.
func (a *App) loadProfiles() ([]storedProfile, error) {
	raw, err := os.ReadFile(a.profilesPath())
	if err != nil {
		return nil, err
	}
	var profiles []storedProfile
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, fmt.Errorf("profiles.json non válido: %w", err)
	}
	return profiles, nil
}

// saveIdentity stores the student's real name and surname in the local reference
// profiles file, keyed by username. File Browser only exposes the login name, so this
// is what lets the app remember (and reuse across sessions) the full name used for the
// certificate and the visible signature. Any other stored fields are preserved.
func (a *App) saveIdentity(username, name, surname string) error {
	profiles, _ := a.loadProfiles() // missing/invalid file -> start from empty
	found := false
	for i := range profiles {
		if strings.EqualFold(profiles[i].Username, username) {
			profiles[i].Name = name
			profiles[i].Surname = surname
			found = true
			break
		}
	}
	if !found {
		profiles = append(profiles, storedProfile{Username: username, Name: name, Surname: surname})
	}
	return a.writeProfiles(profiles)
}

// saveFullIdentity stores/updates the full reference record (name, surname, group,
// email, internal uuid) for username in profiles.json. Used by the admin panel when
// creating users so the enrolled account keeps the same display name/group the student
// sees at login. Existing fields not provided here are preserved.
func (a *App) saveFullIdentity(username, name, surname, group, email, uuid string) error {
	profiles, _ := a.loadProfiles()
	found := false
	for i := range profiles {
		if strings.EqualFold(profiles[i].Username, username) {
			profiles[i].Name, profiles[i].Surname = name, surname
			profiles[i].Group, profiles[i].Email = group, email
			profiles[i].UUID = uuid
			found = true
			break
		}
	}
	if !found {
		profiles = append(profiles, storedProfile{
			Username: username, Name: name, Surname: surname, Group: group, Email: email, UUID: uuid,
		})
	}
	return a.writeProfiles(profiles)
}

// removeIdentity drops username's record from profiles.json (best-effort). Called
// when the admin deletes a user so no stale reference remains.
func (a *App) removeIdentity(username string) error {
	profiles, err := a.loadProfiles()
	if err != nil {
		return nil // missing/invalid file: nothing to remove
	}
	kept := profiles[:0]
	for _, p := range profiles {
		if !strings.EqualFold(p.Username, username) {
			kept = append(kept, p)
		}
	}
	return a.writeProfiles(kept)
}

// writeProfiles persists the reference profiles list to profiles.json.
func (a *App) writeProfiles(profiles []storedProfile) error {
	raw, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.profilesPath(), raw, 0o600)
}

// Login authenticates the student against the File Browser server at serverURL. The
// credentials must match a File Browser account; there is no local/offline mode.
func (a *App) Login(username, password, serverURL string) (*Profile, error) {
	username = strings.TrimSpace(username)
	serverURL = strings.TrimSpace(serverURL)
	if username == "" || password == "" {
		return nil, fmt.Errorf("introduce usuario e contrasinal")
	}
	if serverURL == "" {
		return nil, fmt.Errorf("introduce o enderezo do servidor de ficheiros")
	}
	return a.loginServer(username, password, serverURL)
}

// Logout clears the current session.
func (a *App) Logout() {
	a.publicarSesion(false) // must run before clearing fbToken, which it needs
	a.apiEstadoMu.Lock()
	a.profile = nil
	a.apiEstadoMu.Unlock()
	a.fbURL, a.fbToken, a.fbUser, a.fbPassword = "", "", "", ""
}

// generateSelfSignedP12 creates an RSA self-signed certificate for commonName and
// writes it as a password-protected PKCS#12 (.p12) file. Real students receive their
// certificate from the server; this helper only builds throwaway certificates for
// the tests that exercise the signing flow.
func generateSelfSignedP12(path, commonName, password string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
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
			CommonName:   commonName,
			Organization: []string{"Tao (certificado de demostración)"},
			Country:      []string{"ES"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	pfx, err := pkcs12.Modern.Encode(key, cert, nil, password)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pfx, 0o600)
}
