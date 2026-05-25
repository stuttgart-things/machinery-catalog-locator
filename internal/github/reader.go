package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v66/github"

	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
)

// Reader liest Repository-Dateien ueber die GitHub Contents-API.
// Es implementiert catalog.FileReader.
type Reader struct {
	Client *gh.Client
}

// NewReader erstellt einen GitHub-FileReader.
func NewReader(c *gh.Client) *Reader {
	return &Reader{Client: c}
}

// Read implementiert catalog.FileReader.
func (r *Reader) Read(ctx context.Context, ref catalog.SourceRef) ([]byte, error) {
	file, _, _, err := r.Client.Repositories.GetContents(
		ctx, ref.Owner, ref.Repo, ref.Path,
		&gh.RepositoryContentGetOptions{Ref: ref.Ref},
	)
	if err != nil {
		return nil, fmt.Errorf("contents-api %s: %w", ref, err)
	}
	if file == nil {
		return nil, fmt.Errorf("%s ist ein Verzeichnis, keine Datei", ref.Path)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, fmt.Errorf("inhalt dekodieren %s: %w", ref, err)
	}
	return []byte(content), nil
}
