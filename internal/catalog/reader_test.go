package catalog

import (
	"context"
	"strings"
	"testing"
)

// TestLocalReader_StripPathPrefix covers the local-server case: a blob
// URL whose path includes the same prefix as LocalReader.Root must
// resolve to a single root-relative file, not double up.
func TestLocalReader_StripPathPrefix(t *testing.T) {
	reader := LocalReader{
		Root:            testdataRoot,
		StripPathPrefix: "testdata/catalog/",
	}

	// Path with the prefix — should be stripped and resolved.
	body, err := reader.Read(context.Background(), SourceRef{
		Path: "testdata/catalog/all-locations.yaml",
	})
	if err != nil {
		t.Fatalf("read with prefix: %v", err)
	}
	if !strings.Contains(string(body), "kind: Location") {
		t.Errorf("expected Location body, got:\n%s", body)
	}

	// Path without the prefix — should also work (no-op strip).
	if _, err := reader.Read(context.Background(), SourceRef{
		Path: "all-locations.yaml",
	}); err != nil {
		t.Errorf("read without prefix: %v", err)
	}

	// With no StripPathPrefix configured, the prefixed path produces
	// the doubled path and fails — proving the strip is what fixes it.
	bare := LocalReader{Root: testdataRoot}
	if _, err := bare.Read(context.Background(), SourceRef{
		Path: "testdata/catalog/all-locations.yaml",
	}); err == nil {
		t.Error("bare reader should fail on prefixed path (sanity check)")
	}
}
