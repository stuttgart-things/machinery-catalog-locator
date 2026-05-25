package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// StripPathPrefix, when non-empty, is removed from the start of
	// ref.Path before joining with Root. Lets the same blob URL work
	// both against real GitHub (where the path lives inside the repo)
	// and against a LocalReader rooted somewhere deeper than the repo
	// root. Example: Root="testdata/catalog",
	// StripPathPrefix="testdata/catalog/" turns the URL path
	// "testdata/catalog/all-locations.yaml" back into
	// "all-locations.yaml" before reading.
	StripPathPrefix string
}

// Read implements FileReader against the filesystem.
func (l LocalReader) Read(_ context.Context, ref SourceRef) ([]byte, error) {
	rel := ref.Path
	if l.StripPathPrefix != "" {
		rel = strings.TrimPrefix(rel, l.StripPathPrefix)
	}
	p := filepath.Join(l.Root, filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("local file %s: %w", ref.Path, err)
	}
	return data, nil
}
