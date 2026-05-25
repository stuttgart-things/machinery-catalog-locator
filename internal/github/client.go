// Package github is the GitHub-specific forge adapter: it implements
// catalog.FileReader and encapsulates Pull Request creation.
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

// TokenSource yields a valid Git auth token (e.g. for git push).
type TokenSource func(ctx context.Context) (string, error)

// NewClient builds an authenticated GitHub client. The GitHub App is
// preferred; otherwise it falls back to a Personal Access Token. A
// TokenSource is returned alongside for go-git push operations.
func NewClient(cfg config.GitHubConfig) (*gh.Client, TokenSource, error) {
	if cfg.UsesApp() {
		key, err := os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read private key: %w", err)
		}
		itr, err := ghinstallation.New(http.DefaultTransport, cfg.AppID, cfg.InstallationID, key)
		if err != nil {
			return nil, nil, fmt.Errorf("ghinstallation: %w", err)
		}
		client := gh.NewClient(&http.Client{Transport: itr})
		return client, itr.Token, nil
	}

	client := gh.NewClient(nil).WithAuthToken(cfg.Token)
	token := cfg.Token
	return client, func(context.Context) (string, error) { return token, nil }, nil
}
