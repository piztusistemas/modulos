package main

import "strings"

// Messaging policy, controlled by the administrator (root) and shared with every client
// through dixitalizacion/mensaxes-config.json. It decides which student-to-student
// channels are available and which groups or individuals have messaging switched off
// (for misuse). A missing file means "everything on" — the service is active by default.

// sharedMensaxesConfig is the shared file holding the messaging policy.
const sharedMensaxesConfig = "mensaxes-config.json"

// MessagingConfig is the administrator's messaging policy.
type MessagingConfig struct {
	// WallEnabled turns the per-group wall (avisos/exames/tarefas) on or off for students.
	WallEnabled bool `json:"wallEnabled"`
	// PeerEnabled turns 1:1 messaging between students of a group on or off.
	PeerEnabled bool `json:"peerEnabled"`
	// DisabledGroups/DisabledUsers have ALL messaging blocked (they can still receive;
	// the professorado can always write to them).
	DisabledGroups []string `json:"disabledGroups"`
	DisabledUsers  []string `json:"disabledUsers"`
}

// defaultMessagingConfig is the policy used when no file has been saved yet: everything
// on, nothing blocked.
func defaultMessagingConfig() MessagingConfig {
	return MessagingConfig{
		WallEnabled:    true,
		PeerEnabled:    true,
		DisabledGroups: []string{},
		DisabledUsers:  []string{},
	}
}

// GetMessagingConfig returns the current messaging policy. Any connected account may
// read it (students need to know what is enabled/disabled for them). A missing or
// unreadable file yields the default (all enabled).
func (a *App) GetMessagingConfig() (MessagingConfig, error) {
	if a.profile == nil && !a.isAdmin {
		return defaultMessagingConfig(), nil
	}
	cfg := defaultMessagingConfig()
	if ok, err := a.fbReadSharedJSON(sharedMensaxesConfig, &cfg); err != nil {
		return defaultMessagingConfig(), nil // treat an invalid file as "defaults"
	} else if !ok {
		return defaultMessagingConfig(), nil
	}
	// Normalise the lists (trim, drop blanks) so comparisons elsewhere are simple.
	cfg.DisabledGroups = cleanList(cfg.DisabledGroups)
	cfg.DisabledUsers = cleanList(cfg.DisabledUsers)
	return cfg, nil
}

// SaveMessagingConfig persists the messaging policy to the shared folder. Admin only:
// it runs with an administrator File Browser session (like SetProfessor).
func (a *App) SaveMessagingConfig(cfg MessagingConfig) error {
	cfg.DisabledGroups = cleanList(cfg.DisabledGroups)
	cfg.DisabledUsers = cleanList(cfg.DisabledUsers)
	return a.withAdminFB(func() error {
		return a.fbWriteSharedJSON(sharedMensaxesConfig, cfg)
	})
}

// cleanList trims each entry and drops the empty ones.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
