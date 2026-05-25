package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileReader abstracts read access to repository files.
// Implementations: GitHub, GitLab, local filesystem (see LocalReader).
type FileReader interface {
	Read(ctx context.Context, ref SourceRef) ([]byte, error)
}

// LocalReader reads from the local filesystem. Owner/Repo and Ref are
// ignored. Handy for tests and offline validation.
type LocalReader struct {
	Root string // base directory
}

// Read implements FileReader against the filesystem.
func (l LocalReader) Read(_ context.Context, ref SourceRef) ([]byte, error) {
	p := filepath.Join(l.Root, filepath.FromSlash(ref.Path))
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("local file %s: %w", ref.Path, err)
	}
	return data, nil
}
