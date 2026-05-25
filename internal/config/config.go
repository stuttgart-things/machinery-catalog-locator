// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config bundles all runtime parameters.
type Config struct {
	GRPCPort int
	HTTPPort int
	GitHub   GitHubConfig
	Git      GitIdentity
}

// GitHubConfig carries authentication for GitHub. Either App credentials
// are used or a Personal Access Token as a dev fallback.
type GitHubConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPath string
	Token          string
}

// UsesApp reports whether GitHub App credentials are configured.
func (g GitHubConfig) UsesApp() bool {
	return g.AppID != 0 && g.InstallationID != 0 && g.PrivateKeyPath != ""
}

// GitIdentity is the commit identity used by the bot.
type GitIdentity struct {
	Name  string
	Email string
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	c := Config{
		GRPCPort: envInt("GRPC_PORT", 50051),
		HTTPPort: envInt("HTTP_PORT", 8080),
		GitHub: GitHubConfig{
			PrivateKeyPath: os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
			Token:          os.Getenv("GITHUB_TOKEN"),
		},
		Git: GitIdentity{
			Name:  envDefault("GIT_AUTHOR_NAME", "machinery-catalog-locator"),
			Email: envDefault("GIT_AUTHOR_EMAIL", "machinery-catalog-locator@local"),
		},
	}

	if v := os.Getenv("GITHUB_APP_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("GITHUB_APP_ID invalid: %w", err)
		}
		c.GitHub.AppID = id
	}
	if v := os.Getenv("GITHUB_INSTALLATION_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("GITHUB_INSTALLATION_ID invalid: %w", err)
		}
		c.GitHub.InstallationID = id
	}

	if !c.GitHub.UsesApp() && c.GitHub.Token == "" {
		return Config{}, fmt.Errorf("no GitHub credentials: set GITHUB_APP_ID/INSTALLATION_ID/PRIVATE_KEY_PATH or GITHUB_TOKEN")
	}
	return c, nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
