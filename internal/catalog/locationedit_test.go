package catalog

import (
	"context"
	"strings"
	"testing"
)

// testdataRoot is the path from this package directory to the shared
// catalog fixtures. See testdata/catalog/README.md for the layout.
const testdataRoot = "../../testdata/catalog"

func testResolver() *Resolver {
	return NewResolver(LocalReader{Root: testdataRoot})
}

func resolveFixture(t *testing.T, path string) []*Node {
	t.Helper()
	roots, err := testResolver().Resolve(context.Background(), SourceRef{Path: path})
	if err != nil {
		t.Fatalf("Resolve %s: %v", path, err)
	}
	return roots
}

// TestResolveTree exercises the main fixture tree: multi-target root,
// relative-path resolution, multi-doc files, and a nested Location.
func TestResolveTree(t *testing.T) {
	roots := resolveFixture(t, "all-locations.yaml")
	res := Resources(roots)

	if len(res) != 4 {
		t.Fatalf("expected 4 resources from all-locations.yaml, got %d", len(res))
	}

	wantNames := map[string]bool{
		"claim-router":   false,
		"payment-api":    false,
		"payment-api-db": false,
		"api-gateway":    false,
	}
	for _, n := range res {
		if _, ok := wantNames[n.Entity.Metadata.Name]; !ok {
			t.Errorf("unexpected resource: %s/%s", n.Entity.Kind, n.Entity.Metadata.Name)
			continue
		}
		wantNames[n.Entity.Metadata.Name] = true
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("missing expected resource: %s", name)
		}
	}
}

// TestMultiDocResource verifies that both entities from a `---`-separated
// file surface as distinct nodes with the right FileDocCount.
func TestMultiDocResource(t *testing.T) {
	res := Resources(resolveFixture(t, "all-locations.yaml"))

	var paymentAPI, paymentDB *Node
	for _, n := range res {
		switch n.Entity.Metadata.Name {
		case "payment-api":
			paymentAPI = n
		case "payment-api-db":
			paymentDB = n
		}
	}
	if paymentAPI == nil || paymentDB == nil {
		t.Fatal("expected both payment-api Component and payment-api-db Resource")
	}
	if paymentAPI.Source.Path != paymentDB.Source.Path {
		t.Errorf("multi-doc entities should share Source.Path, got %q vs %q",
			paymentAPI.Source.Path, paymentDB.Source.Path)
	}
	if paymentAPI.FileDocCount != 2 {
		t.Errorf("expected FileDocCount=2 on multi-doc entity, got %d", paymentAPI.FileDocCount)
	}
	if paymentAPI.DocIndex == paymentDB.DocIndex {
		t.Error("multi-doc entities should have distinct DocIndex")
	}
}

// TestNamespaceAndFind covers Find's empty-namespace ↔ "default" alias
// and the case-insensitive kind match.
func TestNamespaceAndFind(t *testing.T) {
	roots := resolveFixture(t, "all-locations.yaml")

	if _, ok := Find(roots, "Component", "claim-router", "insurance"); !ok {
		t.Error("Find should match claim-router in namespace insurance")
	}
	if _, ok := Find(roots, "component", "claim-router", "insurance"); !ok {
		t.Error("Find should be case-insensitive on kind")
	}
	if _, ok := Find(roots, "Component", "api-gateway", ""); !ok {
		t.Error("Find should treat empty namespace as default")
	}
	if _, ok := Find(roots, "Component", "api-gateway", "default"); !ok {
		t.Error("Find should match api-gateway with explicit default namespace")
	}
	if _, ok := Find(roots, "Component", "does-not-exist", ""); ok {
		t.Error("Find should not invent results")
	}
}

// TestSingularTarget verifies that spec.target (singular) is followed
// just like spec.targets (list).
func TestSingularTarget(t *testing.T) {
	roots := resolveFixture(t, "edge-cases/singular-target.yaml")
	res := Resources(roots)
	if len(res) != 1 {
		t.Fatalf("expected 1 resource via spec.target, got %d", len(res))
	}
	if res[0].Entity.Metadata.Name != "only-component" {
		t.Errorf("expected only-component, got %s", res[0].Entity.Metadata.Name)
	}
	if res[0].ViaTarget != "./only-component.yaml" {
		t.Errorf("ViaTarget not propagated: %q", res[0].ViaTarget)
	}
}

// TestCycleDetection asserts that the resolver terminates on a cycle
// instead of looping. cycle-a → cycle-b → cycle-a — should yield two
// Location nodes (one per file) and stop.
func TestCycleDetection(t *testing.T) {
	roots := resolveFixture(t, "edge-cases/cycle-a.yaml")
	all := Flatten(roots)
	if len(all) == 0 {
		t.Fatal("expected at least one node from cycle-a")
	}
	for _, n := range all {
		if !n.Entity.IsLocation() {
			t.Errorf("cycle fixtures should only contain Location entities, got %s", n.Entity.Kind)
		}
	}
	// Resources() filters out Locations, so the cycle should produce
	// zero non-Location entities — nothing to render, but importantly
	// the test reaches this assertion at all, proving termination.
	if len(Resources(roots)) != 0 {
		t.Errorf("cycle fixtures should expose no resources, got %d", len(Resources(roots)))
	}
}

// TestBrokenTarget asserts that one bad target does not halt the
// resolver and is surfaced on Node.Broken with a useful message.
func TestBrokenTarget(t *testing.T) {
	roots := resolveFixture(t, "edge-cases/broken-targets.yaml")
	if len(roots) != 1 {
		t.Fatalf("expected single root Location, got %d", len(roots))
	}
	root := roots[0]
	if len(root.Broken) != 1 {
		t.Fatalf("expected exactly 1 broken target, got %d", len(root.Broken))
	}
	if root.Broken[0].Target != "./does-not-exist.yaml" {
		t.Errorf("unexpected broken target: %q", root.Broken[0].Target)
	}
	if root.Broken[0].Err == "" {
		t.Error("broken target should carry a non-empty error message")
	}

	// The good target still resolved.
	res := Resources(roots)
	if len(res) != 1 || res[0].Entity.Metadata.Name != "only-component" {
		t.Errorf("expected only-component to resolve alongside the broken target, got %+v", res)
	}
}

// --- locationedit ---

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
		t.Errorf("a.yaml should have been removed:\n%s", s)
	}
	if !strings.Contains(s, "./b.yaml") {
		t.Errorf("b.yaml should have been kept:\n%s", s)
	}
}

func TestRemoveTargetNotFound(t *testing.T) {
	in := []byte("kind: Location\nspec:\n  targets:\n    - ./a.yaml\n")
	if _, err := RemoveTargetFromLocation(in, "./x.yaml"); err == nil {
		t.Fatal("expected error for unknown target")
	}
}

// TestRemoveTargetSingular round-trips a real fixture: load
// singular-target.yaml, drop its spec.target, and verify the key is
// gone from the result.
func TestRemoveTargetSingular(t *testing.T) {
	raw, err := LocalReader{Root: testdataRoot}.Read(
		context.Background(),
		SourceRef{Path: "edge-cases/singular-target.yaml"},
	)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out, err := RemoveTargetFromLocation(raw, "./only-component.yaml")
	if err != nil {
		t.Fatalf("RemoveTarget singular: %v", err)
	}
	if strings.Contains(string(out), "target:") {
		t.Errorf("spec.target key should be gone after removal:\n%s", out)
	}
}
