// Package github ist der GitHub-spezifische Forge-Adapter:
// es implementiert catalog.FileReader und kapselt die PR-Erstellung.
package github

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v66/github"

	"github.com/stuttgart-things/machinery-catalog-locator/internal/config"
)

// TokenSource liefert ein gueltiges Git-Auth-Token (z. B. fuer git push).
type TokenSource func(ctx context.Context) (string, error)

// NewClient baut einen authentifizierten GitHub-Client.
// Bevorzugt wird die GitHub App; faellt sonst auf ein PAT zurueck.
// Zusaetzlich wird eine TokenSource fuer go-git-Operationen zurueckgegeben.
func NewClient(cfg config.GitHubConfig) (*gh.Client, TokenSource, error) {
	if cfg.UsesApp() {
		key, err := os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("private key lesen: %w", err)
		}
		itr, err := ghinstallation.New(http.DefaultTransport, cfg.AppID, cfg.InstallationID, key)
		if err != nil {
			return nil, nil, fmt.Errorf("ghinstallation: %w", err)
		}
		client := gh.NewClient(&http.Client{Transport: itr})
		return client, itr.Token, nil
	}

	// Dev-Fallback: Personal Access Token
	client := gh.NewClient(nil).WithAuthToken(cfg.Token)
	token := cfg.Token
	return client, func(context.Context) (string, error) { return token, nil }, nil
}
