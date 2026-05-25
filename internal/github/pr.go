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

// PRRequest describes a change that should land as a Pull Request.
type PRRequest struct {
	Owner         string
	Repo          string
	BaseBranch    string
	HeadBranch    string
	Title         string
	Body          string
	CommitMessage string
	Edits         map[string][]byte // path -> new content
	Deletes       []string          // paths to remove
}

// PRService opens Pull Requests: clone + commit + push via go-git, PR
// creation via go-github.
type PRService struct {
	client *gh.Client
	token  TokenSource
	author appcfg.GitIdentity
}

func NewPRService(client *gh.Client, token TokenSource, author appcfg.GitIdentity) *PRService {
	return &PRService{client: client, token: token, author: author}
}

// OpenPullRequest performs the full PR cycle and returns the PR URL.
func (s *PRService) OpenPullRequest(ctx context.Context, req PRRequest) (string, error) {
	if len(req.Edits) == 0 && len(req.Deletes) == 0 {
		return "", fmt.Errorf("PRRequest contains no changes")
	}

	token, err := s.token(ctx)
	if err != nil {
		return "", fmt.Errorf("auth token: %w", err)
	}
	auth := &githttp.BasicAuth{Username: "x-access-token", Password: token}

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

	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(req.HeadBranch),
		Create: true,
	}); err != nil {
		return "", fmt.Errorf("create branch %s: %w", req.HeadBranch, err)
	}

	fs := wt.Filesystem
	for path, content := range req.Edits {
		f, err := fs.Create(path)
		if err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
		if _, err := f.Write(content); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("write %s: %w", path, err)
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

	if _, err := wt.Commit(req.CommitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  s.author.Name,
			Email: s.author.Email,
			When:  time.Now(),
		},
	}); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	if err := repo.PushContext(ctx, &git.PushOptions{
		Auth: auth,
		RefSpecs: []config.RefSpec{config.RefSpec(fmt.Sprintf(
			"refs/heads/%s:refs/heads/%s", req.HeadBranch, req.HeadBranch))},
	}); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}

	pr, _, err := s.client.PullRequests.Create(ctx, req.Owner, req.Repo, &gh.NewPullRequest{
		Title: gh.String(req.Title),
		Head:  gh.String(req.HeadBranch),
		Base:  gh.String(req.BaseBranch),
		Body:  gh.String(req.Body),
	})
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}
	return pr.GetHTMLURL(), nil
}
