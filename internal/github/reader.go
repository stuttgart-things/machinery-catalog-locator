package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v66/github"

	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
)

// Reader reads repository files via the GitHub Contents API. It
// implements catalog.FileReader.
type Reader struct {
	Client *gh.Client
}

func NewReader(c *gh.Client) *Reader {
	return &Reader{Client: c}
}

func (r *Reader) Read(ctx context.Context, ref catalog.SourceRef) ([]byte, error) {
	file, _, _, err := r.Client.Repositories.GetContents(
		ctx, ref.Owner, ref.Repo, ref.Path,
		&gh.RepositoryContentGetOptions{Ref: ref.Ref},
	)
	if err != nil {
		return nil, fmt.Errorf("contents api %s: %w", ref, err)
	}
	if file == nil {
		return nil, fmt.Errorf("%s is a directory, not a file", ref.Path)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decode content %s: %w", ref, err)
	}
	return []byte(content), nil
}
