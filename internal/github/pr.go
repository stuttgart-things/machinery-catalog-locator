package github

import (
	"context"
	"fmt"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	gh "github.com/google/go-github/v66/github"

	appcfg "github.com/stuttgart-things/machinery-catalog-locator/internal/config"
)

// PRRequest beschreibt eine Aenderung, die als Pull Request landen soll.
type PRRequest struct {
	Owner         string
	Repo          string
	BaseBranch    string
	HeadBranch    string
	Title         string
	Body          string
	CommitMessage string
	Edits         map[string][]byte // Pfad -> neuer Inhalt
	Deletes       []string          // zu loeschende Pfade
}

// PRService erzeugt Pull Requests: Clone + Commit + Push via go-git,
// PR-Erstellung via go-github.
type PRService struct {
	client *gh.Client
	token  TokenSource
	author appcfg.GitIdentity
}

// NewPRService erstellt den PR-Service.
func NewPRService(client *gh.Client, token TokenSource, author appcfg.GitIdentity) *PRService {
	return &PRService{client: client, token: token, author: author}
}

// OpenPullRequest fuehrt die komplette PR-Erstellung aus und liefert die PR-URL.
func (s *PRService) OpenPullRequest(ctx context.Context, req PRRequest) (string, error) {
	if len(req.Edits) == 0 && len(req.Deletes) == 0 {
		return "", fmt.Errorf("PRRequest enthaelt keine Aenderungen")
	}

	token, err := s.token(ctx)
	if err != nil {
		return "", fmt.Errorf("auth-token: %w", err)
	}
	auth := &githttp.BasicAuth{Username: "x-access-token", Password: token}

	// 1. In-Memory Shallow-Clone des Base-Branch.
	repo, err := git.CloneContext(ctx, memory.NewStorage(), memfs.New(), &git.CloneOptions{
		URL:           fmt.Sprintf("https://github.com/%s/%s.git", req.Owner, req.Repo),
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(req.BaseBranch),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return "", fmt.Errorf("clone: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}

	// 2. Neuen Head-Branch anlegen.
	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(req.HeadBranch),
		Create: true,
	}); err != nil {
		return "", fmt.Errorf("branch %s anlegen: %w", req.HeadBranch, err)
	}

	// 3. Edits und Deletes anwenden.
	fs := wt.Filesystem
	for path, content := range req.Edits {
		f, err := fs.Create(path)
		if err != nil {
			return "", fmt.Errorf("datei %s schreiben: %w", path, err)
		}
		if _, err := f.Write(content); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("datei %s schreiben: %w", path, err)
		}
		_ = f.Close()
		if _, err := wt.Add(path); err != nil {
			return "", fmt.Errorf("git add %s: %w", path, err)
		}
	}
	for _, path := range req.Deletes {
		if _, err := wt.Remove(path); err != nil {
			return "", fmt.Errorf("git rm %s: %w", path, err)
		}
	}

	// 4. Commit.
	if _, err := wt.Commit(req.CommitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  s.author.Name,
			Email: s.author.Email,
			When:  time.Now(),
		},
	}); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// 5. Push.
	if err := repo.PushContext(ctx, &git.PushOptions{
		Auth: auth,
		RefSpecs: []config.RefSpec{config.RefSpec(fmt.Sprintf(
			"refs/heads/%s:refs/heads/%s", req.HeadBranch, req.HeadBranch))},
	}); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}

	// 6. Pull Request oeffnen.
	pr, _, err := s.client.PullRequests.Create(ctx, req.Owner, req.Repo, &gh.NewPullRequest{
		Title: gh.String(req.Title),
		Head:  gh.String(req.HeadBranch),
		Base:  gh.String(req.BaseBranch),
		Body:  gh.String(req.Body),
	})
	if err != nil {
		return "", fmt.Errorf("PR erstellen: %w", err)
	}
	return pr.GetHTMLURL(), nil
}
