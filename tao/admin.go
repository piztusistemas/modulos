package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Director mode.
//
// The app has a local management gate called "Director" (username directorUser,
// "director"), deliberately named apart from "Admin" — the SEPARATE File Browser
// administrator account (FBAdminUser/FBAdminPass below), so the two roles are never
// confused: Admin manages File Browser, Director manages Tao itself (server, centre/
// department, enrolling/removing students). Unlike Admin, Director's password is NOT
// fixed: each professorado sets their own the first time they open Tao (see
// DirectorConfigurado/ConfigurarDirector), hashed with bcrypt in <dataDir>/director.json
// — kept apart from admin.json (which GetAdminConfig returns to the frontend as-is; a
// password hash has no business being in that struct).
//
// Older Tao installs that still had the previous fixed credentials simply have no
// director.json after upgrading: DirectorConfigurado() reports false, and the next
// person to type "director" is walked through ConfigurarDirector instead of a login —
// the old fixed password stops working, which is the point (it used to be identical,
// and publicly readable, in every Tao binary).

const directorUser = "director"

// directorAuth is the locally stored, bcrypt-hashed Director credential.
type directorAuth struct {
	PassHash string `json:"passHash"`
}

func (a *App) directorAuthPath() string { return filepath.Join(a.dataDir, "director.json") }

// DirectorConfigurado reports whether a Director password has already been set on
// this machine — the frontend calls this before attempting a "director" login to
// decide whether to show the normal login form or the "create your password" one.
func (a *App) DirectorConfigurado() bool {
	_, err := os.Stat(a.directorAuthPath())
	return err == nil
}

// ConfigurarDirector sets the Director password for the first time. Fails if one is
// already configured (no "change password" flow yet — not asked for).
func (a *App) ConfigurarDirector(password string) error {
	if a.DirectorConfigurado() {
		return fmt.Errorf("xa hai un contrasinal de Director configurado")
	}
	if len(password) < 8 {
		return fmt.Errorf("o contrasinal debe ter polo menos 8 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(directorAuth{PassHash: string(hash)})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.directorAuthPath(), raw, 0o600)
}

// AdminConfig is the locally stored administration configuration.
type AdminConfig struct {
	ServerURL   string `json:"serverURL"`   // File Browser server
	Center      string `json:"center"`      // O= for the CA and student certificates
	Department  string `json:"department"`  // OU= for the CA; CN "CA <department>"
	FBAdminUser string `json:"fbAdminUser"` // File Browser administrator account
	FBAdminPass string `json:"fbAdminPass"` // its password (plaintext, local only)
}

// AdminSession is returned to the frontend after a successful admin login.
type AdminSession struct {
	IsAdmin bool `json:"isAdmin"`
}

// CAInfo describes the currently active signing CA for the admin panel.
type CAInfo struct {
	Exists    bool   `json:"exists"`    // a CA is available (local or embedded)
	Local     bool   `json:"local"`     // true when it was generated in the panel (dataDir/ca)
	Subject   string `json:"subject"`   // human-readable subject
	ExpiresAt string `json:"expiresAt"` // NotAfter, RFC3339
}

func (a *App) adminConfigPath() string { return filepath.Join(a.dataDir, "admin.json") }

// AdminLogin validates the Director credentials against the locally stored bcrypt
// hash. It does not touch the network: the File Browser administrator (Admin)
// account is configured separately and used only for the enrolment operations.
func (a *App) AdminLogin(username, password string) (*AdminSession, error) {
	if !strings.EqualFold(strings.TrimSpace(username), directorUser) {
		return nil, fmt.Errorf("usuario ou contrasinal de administración incorrectos")
	}
	raw, err := os.ReadFile(a.directorAuthPath())
	if err != nil {
		return nil, fmt.Errorf("usuario ou contrasinal de administración incorrectos")
	}
	var da directorAuth
	if err := json.Unmarshal(raw, &da); err != nil {
		return nil, fmt.Errorf("usuario ou contrasinal de administración incorrectos")
	}
	if bcrypt.CompareHashAndPassword([]byte(da.PassHash), []byte(password)) != nil {
		return nil, fmt.Errorf("usuario ou contrasinal de administración incorrectos")
	}
	a.apiEstadoMu.Lock()
	a.isAdmin = true
	a.apiEstadoMu.Unlock()
	return &AdminSession{IsAdmin: true}, nil
}

// requireAdmin guards the administration methods.
func (a *App) requireAdmin() error {
	if !a.isAdmin {
		return fmt.Errorf("non hai unha sesión de administración activa")
	}
	return nil
}

// GetAdminConfig returns the stored administration configuration, falling back to the
// built-in default server URL so the panel starts pre-filled on first run. Centre and
// department are deliberately left blank until the Director sets them — no sample
// school name should ship as if it were real data.
func (a *App) GetAdminConfig() (*AdminConfig, error) {
	cfg := &AdminConfig{
		ServerURL: servidorPorDefecto(),
	}
	raw, err := os.ReadFile(a.adminConfigPath())
	if err != nil {
		return cfg, nil // no file yet: return defaults
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("admin.json non válido: %w", err)
	}
	return cfg, nil
}

// SaveAdminConfig persists the administration configuration to admin.json (0600, as
// it holds the File Browser administrator password in plain text — the same trust
// assumption already made by shipping the CA key with the app).
func (a *App) SaveAdminConfig(cfg AdminConfig) error {
	if err := a.requireAdmin(); err != nil {
		return err
	}
	cfg.ServerURL = normalizeServerURL(cfg.ServerURL)
	// A bare server name is stored as the real URL (see resolverServidor).
	if real := resolverServidor(cfg.ServerURL); real != "" {
		cfg.ServerURL = real
	}
	cfg.Center = strings.TrimSpace(cfg.Center)
	cfg.Department = strings.TrimSpace(cfg.Department)
	cfg.FBAdminUser = strings.TrimSpace(cfg.FBAdminUser)

	// Safeguard against the common setup slip of swapping the administrator user and
	// password (or pointing at the wrong account): before persisting, verify the
	// credentials actually authenticate against the server. Only a definite rejection
	// blocks the save — a server that is merely unreachable does not, so the panel can
	// still be configured while the File Browser server is down.
	if cfg.ServerURL != "" && cfg.FBAdminUser != "" && cfg.FBAdminPass != "" {
		if _, err := fbLogin(cfg.ServerURL, cfg.FBAdminUser, cfg.FBAdminPass); err != nil {
			if errors.Is(err, errFBBadCredentials) {
				return fmt.Errorf("as credenciais de administrador non funcionan no servidor %s: comproba que o «Usuario admin» e o «Contrasinal» son os da conta de administración de File Browser (non os de Director) e que non están intercambiados. Non se gardou a configuración. Ollo: File Browser bloquea a conta 15 min tras uns 8 intentos fallidos, así que non insistas ás cegas", cfg.ServerURL)
			}
			if errors.Is(err, errFBContaBloqueada) {
				// Sen poder verificar, non se persiste un contrasinal que quizais sexa o malo.
				return fmt.Errorf("non se puido verificar o contrasinal: %w. Non se gardou a configuración", err)
			}
			log.Printf("[admin] aviso: non se puideron verificar as credenciais de File Browser ao gardar (%v); gárdase igualmente", err)
		}
	}

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.adminConfigPath(), raw, 0o600)
}

// GenerateCA creates a new department Certificate Authority from the stored centre and
// department, replacing any previous locally generated CA. Newly enrolled students get
// certificates signed by this CA.
func (a *App) GenerateCA() (*CAInfo, error) {
	if err := a.requireAdmin(); err != nil {
		return nil, err
	}
	cfg, err := a.GetAdminConfig()
	if err != nil {
		return nil, err
	}
	if err := generateCA(a.dataDir, cfg.Center, cfg.Department); err != nil {
		return nil, err
	}
	return a.GetCAInfo()
}

// GetCAInfo reports which signing CA is currently in effect (a CA generated in the
// panel takes precedence over the embedded one) and its subject/expiry.
func (a *App) GetCAInfo() (*CAInfo, error) {
	info := &CAInfo{}
	localCert := filepath.Join(caDir(a.dataDir), "ca.crt")
	if _, err := os.Stat(localCert); err == nil {
		info.Local = true
	}

	caCert, _, err := loadCAFrom(a.dataDir)
	if err != nil {
		return info, nil // no usable CA
	}
	info.Exists = true
	info.Subject = caCert.Subject.String()
	info.ExpiresAt = caCert.NotAfter.Format(time.RFC3339)
	return info, nil
}
