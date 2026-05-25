package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileReader abstrahiert den lesenden Zugriff auf Repository-Dateien.
// Implementierungen: GitHub, GitLab, lokales Dateisystem (siehe LocalReader).
type FileReader interface {
	Read(ctx context.Context, ref SourceRef) ([]byte, error)
}

// LocalReader liest aus dem lokalen Dateisystem. Owner/Repo werden ignoriert;
// Ref wird ebenfalls ignoriert. Nuetzlich fuer Tests und Offline-Validierung.
type LocalReader struct {
	Root string // Basisverzeichnis
}

// Read implementiert FileReader gegen das Dateisystem.
func (l LocalReader) Read(_ context.Context, ref SourceRef) ([]byte, error) {
	p := filepath.Join(l.Root, filepath.FromSlash(ref.Path))
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("lokale Datei %s: %w", ref.Path, err)
	}
	return data, nil
}
