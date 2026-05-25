package catalog

import (
	"context"
	"strings"
	"testing"
)

// mapReader ist ein In-Memory-FileReader fuer Tests (Pfad -> Inhalt).
type mapReader map[string][]byte

func (m mapReader) Read(_ context.Context, ref SourceRef) ([]byte, error) {
	data, ok := m[ref.Path]
	if !ok {
		return nil, errNotFound{ref.Path}
	}
	return data, nil
}

type errNotFound struct{ path string }

func (e errNotFound) Error() string { return "nicht gefunden: " + e.path }

func TestResolveTree(t *testing.T) {
	files := mapReader{
		"all-locations.yaml": []byte(`
apiVersion: backstage.io/v1alpha1
kind: Location
metadata:
  name: root
spec:
  type: url
  targets:
    - ./components/claim-router/catalog-info.yaml
`),
		"components/claim-router/catalog-info.yaml": []byte(`
apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: claim-router
spec:
  type: service
`),
	}
	r := NewResolver(files)
	roots, err := r.Resolve(context.Background(), SourceRef{Path: "all-locations.yaml"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	res := Resources(roots)
	if len(res) != 1 {
		t.Fatalf("erwartet 1 Ressource, bekommen %d", len(res))
	}
	if res[0].Entity.Metadata.Name != "claim-router" {
		t.Errorf("falsche Ressource: %s", res[0].Entity.Metadata.Name)
	}
	if res[0].ViaTarget != "./components/claim-router/catalog-info.yaml" {
		t.Errorf("ViaTarget nicht gesetzt: %q", res[0].ViaTarget)
	}
}

func TestRemoveTargetFromLocation(t *testing.T) {
	in := []byte(`apiVersion: backstage.io/v1alpha1
kind: Location
metadata:
  name: root
spec:
  type: url
  targets:
    - ./a.yaml
    - ./b.yaml
`)
	out, err := RemoveTargetFromLocation(in, "./a.yaml")
	if err != nil {
		t.Fatalf("RemoveTarget: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "./a.yaml") {
		t.Errorf("a.yaml haette entfernt werden muessen:\n%s", s)
	}
	if !strings.Contains(s, "./b.yaml") {
		t.Errorf("b.yaml haette bleiben muessen:\n%s", s)
	}
}

func TestRemoveTargetNotFound(t *testing.T) {
	in := []byte("kind: Location\nspec:\n  targets:\n    - ./a.yaml\n")
	if _, err := RemoveTargetFromLocation(in, "./x.yaml"); err == nil {
		t.Fatal("Fehler erwartet bei unbekanntem Target")
	}
}
