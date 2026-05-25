package catalog

import (
	"context"
	"strings"
	"testing"
)

// TestCrossplaneRefs_None covers an entity with no annotations at all
// and one with annotations that don't include any Crossplane keys.
func TestCrossplaneRefs_None(t *testing.T) {
	if got := CrossplaneRefs(Entity{}); len(got) != 0 {
		t.Errorf("empty entity should yield no refs, got %+v", got)
	}
	e := Entity{Metadata: EntityMetadata{Annotations: map[string]string{
		"unrelated.example.com/foo": "bar",
	}}}
	if got := CrossplaneRefs(e); len(got) != 0 {
		t.Errorf("unrelated annotations should yield no refs, got %+v", got)
	}
}

// TestCrossplaneRefs_ParsesURLs asserts that both annotation keys
// resolve and that the parsed SourceRef.Path matches the blob URL's
// path component — that's what makes the same annotation work against
// both GitHub and LocalReader.
func TestCrossplaneRefs_ParsesURLs(t *testing.T) {
	e := Entity{Metadata: EntityMetadata{Annotations: map[string]string{
		AnnotationCrossplaneClaim: "https://github.com/o/r/blob/main/crossplane-claims/x/claim.yaml",
		AnnotationCrossplaneXR:    "https://github.com/o/r/blob/main/crossplane-claims/x/xr.yaml",
	}}}
	refs := CrossplaneRefs(e)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs (claim + xr), got %d", len(refs))
	}

	byKind := map[string]CrossplaneRef{}
	for _, r := range refs {
		byKind[r.Kind] = r
	}
	if _, ok := byKind["claim"]; !ok {
		t.Error("missing claim ref")
	}
	if _, ok := byKind["xr"]; !ok {
		t.Error("missing xr ref")
	}
	if got := byKind["claim"].Source.Path; got != "crossplane-claims/x/claim.yaml" {
		t.Errorf("claim Source.Path mismatch: %q", got)
	}
	if got := byKind["xr"].Source.Owner; got != "o" {
		t.Errorf("expected owner=o, got %q", got)
	}
}

// TestCrossplaneRefs_SkipsMalformed silently drops invalid URLs rather
// than failing the resolve — we don't want one bad annotation to take
// down a whole tree render.
func TestCrossplaneRefs_SkipsMalformed(t *testing.T) {
	e := Entity{Metadata: EntityMetadata{Annotations: map[string]string{
		AnnotationCrossplaneClaim: "not-a-url",
		AnnotationCrossplaneXR:    "https://example.com/no/blob/path",
	}}}
	if got := CrossplaneRefs(e); len(got) != 0 {
		t.Errorf("malformed URLs should be skipped, got %+v", got)
	}
}

// TestFindByCrossplaneSource is the reverse-direction check: starting
// from a Crossplane manifest path, find the catalog entity that
// references it. Pairs with TestCrossplaneRefs_FetchesFixture which
// goes the other way.
func TestFindByCrossplaneSource(t *testing.T) {
	roots := resolveFixture(t, "all-locations.yaml")

	cases := []struct {
		manifestPath string
		wantName     string
	}{
		{"crossplane-claims/insurance/claim-router.yaml", "claim-router"},
		{"crossplane-claims/payments/payment-api-xr.yaml", "payment-api"},
		{"crossplane-claims/payments/payment-api-db.yaml", "payment-api-db"},
		{"crossplane-claims/platform/api-gateway-xr.yaml", "api-gateway"},
	}
	for _, tc := range cases {
		got := FindByCrossplaneSource(roots, tc.manifestPath)
		if len(got) != 1 {
			t.Errorf("%s: expected exactly one catalog owner, got %d", tc.manifestPath, len(got))
			continue
		}
		if got[0].Entity.Metadata.Name != tc.wantName {
			t.Errorf("%s: expected owner %s, got %s",
				tc.manifestPath, tc.wantName, got[0].Entity.Metadata.Name)
		}
	}

	if got := FindByCrossplaneSource(roots, "does/not/exist.yaml"); len(got) != 0 {
		t.Errorf("unmatched path should return no owners, got %d", len(got))
	}
	if got := FindByCrossplaneSource(roots, ""); len(got) != 0 {
		t.Errorf("empty path should return no owners, got %d", len(got))
	}
}

// TestResolveClaimForEntity demonstrates the workflow callers reach
// for when they have a catalog entity name and want its claim YAML:
// resolve → Find → CrossplaneRefs → reader.Read. The bytes must equal
// the fixture verbatim so callers can pipe them straight into kubectl.
func TestResolveClaimForEntity(t *testing.T) {
	reader := LocalReader{Root: testdataRoot}
	roots := resolveFixture(t, "all-locations.yaml")

	node, ok := Find(roots, "Component", "claim-router", "insurance")
	if !ok {
		t.Fatal("Find should locate claim-router@insurance")
	}
	refs := CrossplaneRefs(node.Entity)
	if len(refs) != 1 || refs[0].Kind != "claim" {
		t.Fatalf("claim-router should have exactly one claim ref, got %+v", refs)
	}

	got, err := reader.Read(context.Background(), refs[0].Source)
	if err != nil {
		t.Fatalf("fetch linked claim: %v", err)
	}
	want, err := reader.Read(context.Background(), SourceRef{
		Path: "crossplane-claims/insurance/claim-router.yaml",
	})
	if err != nil {
		t.Fatalf("read fixture directly: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("claim bytes via annotation differ from the fixture:\n--- via annotation ---\n%s\n--- fixture ---\n%s", got, want)
	}
}

// TestCrossplaneRefs_FetchesFixture is the end-to-end check: resolve
// the catalog, find each entity's Crossplane annotation, fetch the
// referenced fixture via LocalReader, and verify it parses to the
// expected kind+name. If the URL path and the fixture layout drift
// apart, this test catches it.
func TestCrossplaneRefs_FetchesFixture(t *testing.T) {
	reader := LocalReader{Root: testdataRoot}
	roots := resolveFixture(t, "all-locations.yaml")

	want := map[string]string{ // entity name -> expected crossplane kind
		"claim-router":   "ClaimRouter",
		"payment-api":    "XPaymentApi",
		"payment-api-db": "PostgresClaim",
		"api-gateway":    "XApiGateway",
	}

	got := map[string]string{}
	for _, n := range Resources(roots) {
		refs := CrossplaneRefs(n.Entity)
		if len(refs) == 0 {
			t.Errorf("%s: expected at least one Crossplane ref", n.Entity.Metadata.Name)
			continue
		}
		body, err := reader.Read(context.Background(), refs[0].Source)
		if err != nil {
			t.Errorf("%s: fetch via LocalReader: %v", n.Entity.Metadata.Name, err)
			continue
		}
		// Cheap inspection — the manifest is just YAML, so a
		// substring check on `kind:` is enough to prove we read the
		// right file without pulling in a manifest schema.
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "kind:") {
				got[n.Entity.Metadata.Name] = strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
				break
			}
		}
	}

	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: expected linked manifest kind=%s, got %q", name, kind, got[name])
		}
	}
}
