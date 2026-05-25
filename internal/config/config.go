// Package config laedt die Laufzeitkonfiguration aus Environment-Variablen.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config buendelt alle Laufzeitparameter.
type Config struct {
	ListenAddr string
	GitHub     GitHubConfig
	Git        GitIdentity
}

// GitHubConfig haelt die Authentifizierungsdaten fuer GitHub.
// Entweder werden App-Credentials genutzt oder - als Dev-Fallback - ein Token.
type GitHubConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPath string
	Token          string
}

// UsesApp meldet, ob eine GitHub App konfiguriert ist.
func (g GitHubConfig) UsesApp() bool {
	return g.AppID != 0 && g.InstallationID != 0 && g.PrivateKeyPath != ""
}

// GitIdentity ist die Commit-Identitaet des Bots.
type GitIdentity struct {
	Name  string
	Email string
}

// Load liest die Konfiguration aus dem Environment und validiert sie.
func Load() (Config, error) {
	c := Config{
		ListenAddr: envDefault("LISTEN_ADDR", ":8080"),
		GitHub: GitHubConfig{
			PrivateKeyPath: os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
			Token:          os.Getenv("GITHUB_TOKEN"),
		},
		Git: GitIdentity{
			Name:  envDefault("GIT_AUTHOR_NAME", "catalog-locator"),
			Email: envDefault("GIT_AUTHOR_EMAIL", "catalog-locator@local"),
		},
	}

	if v := os.Getenv("GITHUB_APP_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("GITHUB_APP_ID ungueltig: %w", err)
		}
		c.GitHub.AppID = id
	}
	if v := os.Getenv("GITHUB_INSTALLATION_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("GITHUB_INSTALLATION_ID ungueltig: %w", err)
		}
		c.GitHub.InstallationID = id
	}

	if !c.GitHub.UsesApp() && c.GitHub.Token == "" {
		return Config{}, fmt.Errorf("keine GitHub-Credentials: entweder GITHUB_APP_ID/INSTALLATION_ID/PRIVATE_KEY_PATH oder GITHUB_TOKEN setzen")
	}
	return c, nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
