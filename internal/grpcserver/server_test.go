package grpcserver

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
)

// testdataRoot points at the fixture tree from this package directory.
const testdataRoot = "../../testdata/catalog"

// rootURL is a fake blob URL whose path matches the local fixture
// layout. ParseBlobURL extracts the path; LocalReader ignores the
// owner/repo/ref. Same trick the resolver tests use.
const rootURL = "https://github.com/o/r/blob/main/all-locations.yaml"

func newTestServer() *Server {
	reader := catalog.LocalReader{Root: testdataRoot}
	return &Server{
		Resolver: catalog.NewResolver(reader),
		Reader:   reader,
		// PR is unused by GetEntityManifest, leave nil.
	}
}

func TestGetEntityManifest_Success(t *testing.T) {
	srv := newTestServer()
	resp, err := srv.GetEntityManifest(context.Background(), &catalogservice.GetEntityManifestRequest{
		RootUrl:   rootURL,
		Kind:      "Component",
		Name:      "claim-router",
		Namespace: "insurance",
	})
	if err != nil {
		t.Fatalf("GetEntityManifest: %v", err)
	}
	if len(resp.Manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(resp.Manifests))
	}
	m := resp.Manifests[0]
	if m.LinkKind != "claim" {
		t.Errorf("expected link_kind=claim, got %q", m.LinkKind)
	}
	if m.Annotation != catalog.AnnotationCrossplaneClaim {
		t.Errorf("annotation key mismatch: %q", m.Annotation)
	}
	if m.Source.GetPath() != "crossplane-claims/insurance/claim-router.yaml" {
		t.Errorf("source path mismatch: %q", m.Source.GetPath())
	}
	if !strings.Contains(string(m.Body), "kind: ClaimRouter") {
		t.Errorf("manifest body should contain `kind: ClaimRouter`, got:\n%s", m.Body)
	}
}

// TestGetEntityManifest_MultipleRefs is the multi-doc case — payment-api
// has an xr annotation and payment-api-db has a claim annotation, but
// they're separate entities. This test covers the entity that has at
// least one ref and confirms the loop returns them all.
func TestGetEntityManifest_PaymentAPIHasXR(t *testing.T) {
	srv := newTestServer()
	resp, err := srv.GetEntityManifest(context.Background(), &catalogservice.GetEntityManifestRequest{
		RootUrl: rootURL,
		Kind:    "Component",
		Name:    "payment-api",
	})
	if err != nil {
		t.Fatalf("GetEntityManifest: %v", err)
	}
	if len(resp.Manifests) != 1 || resp.Manifests[0].LinkKind != "xr" {
		t.Fatalf("expected single xr manifest, got %+v", resp.Manifests)
	}
}

// TestGetEntityManifest_NoAnnotations: only-component (reached via the
// singular-target edge-case fixture) has no Crossplane annotations, so
// the RPC must surface that with FailedPrecondition rather than an
// empty success.
func TestGetEntityManifest_NoAnnotations(t *testing.T) {
	srv := newTestServer()
	_, err := srv.GetEntityManifest(context.Background(), &catalogservice.GetEntityManifestRequest{
		RootUrl: "https://github.com/o/r/blob/main/edge-cases/singular-target.yaml",
		Kind:    "Component",
		Name:    "only-component",
	})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %s (err=%v)", got, err)
	}
}

func TestGetEntityManifest_NotFound(t *testing.T) {
	srv := newTestServer()
	_, err := srv.GetEntityManifest(context.Background(), &catalogservice.GetEntityManifestRequest{
		RootUrl: rootURL,
		Kind:    "Component",
		Name:    "nope",
	})
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("expected NotFound, got %s (err=%v)", got, err)
	}
}

// TestListEntitiesByCrossplaneSource is the reverse direction:
// given a Crossplane manifest URL, find the catalog entity referencing
// it. Mirrors TestFindByCrossplaneSource in the catalog package, but
// goes through the gRPC handler end-to-end.
func TestListEntitiesByCrossplaneSource(t *testing.T) {
	srv := newTestServer()

	cases := []struct {
		manifestURL string
		wantName    string
	}{
		{"https://github.com/o/r/blob/main/crossplane-claims/insurance/claim-router.yaml", "claim-router"},
		{"https://github.com/o/r/blob/main/crossplane-claims/payments/payment-api-db.yaml", "payment-api-db"},
		{"https://github.com/o/r/blob/main/crossplane-claims/platform/api-gateway-xr.yaml", "api-gateway"},
	}
	for _, tc := range cases {
		resp, err := srv.ListEntitiesByCrossplaneSource(context.Background(), &catalogservice.ListEntitiesByCrossplaneSourceRequest{
			RootUrl:     rootURL,
			ManifestUrl: tc.manifestURL,
		})
		if err != nil {
			t.Errorf("%s: %v", tc.manifestURL, err)
			continue
		}
		if len(resp.Entities) != 1 {
			t.Errorf("%s: expected 1 entity, got %d", tc.manifestURL, len(resp.Entities))
			continue
		}
		if resp.Entities[0].Name != tc.wantName {
			t.Errorf("%s: expected entity %s, got %s",
				tc.manifestURL, tc.wantName, resp.Entities[0].Name)
		}
	}

	// Unmatched manifest: empty list, no error (list-style semantics).
	resp, err := srv.ListEntitiesByCrossplaneSource(context.Background(), &catalogservice.ListEntitiesByCrossplaneSourceRequest{
		RootUrl:     rootURL,
		ManifestUrl: "https://github.com/o/r/blob/main/crossplane-claims/nope/missing.yaml",
	})
	if err != nil {
		t.Fatalf("unmatched manifest should not error: %v", err)
	}
	if len(resp.Entities) != 0 {
		t.Errorf("unmatched manifest should yield empty list, got %d", len(resp.Entities))
	}
}

func TestListEntitiesByCrossplaneSource_InvalidArgument(t *testing.T) {
	srv := newTestServer()

	if _, err := srv.ListEntitiesByCrossplaneSource(context.Background(), &catalogservice.ListEntitiesByCrossplaneSourceRequest{
		RootUrl:     "not-a-url",
		ManifestUrl: "https://github.com/o/r/blob/main/crossplane-claims/insurance/claim-router.yaml",
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("bad root_url should be InvalidArgument, got %v", err)
	}

	if _, err := srv.ListEntitiesByCrossplaneSource(context.Background(), &catalogservice.ListEntitiesByCrossplaneSourceRequest{
		RootUrl:     rootURL,
		ManifestUrl: "not-a-url",
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("bad manifest_url should be InvalidArgument, got %v", err)
	}
}

func TestGetEntityManifest_InvalidArgument(t *testing.T) {
	srv := newTestServer()

	if _, err := srv.GetEntityManifest(context.Background(), &catalogservice.GetEntityManifestRequest{
		RootUrl: "not-a-url",
		Kind:    "Component",
		Name:    "claim-router",
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("bad root_url should be InvalidArgument, got %v", err)
	}

	if _, err := srv.GetEntityManifest(context.Background(), &catalogservice.GetEntityManifestRequest{
		RootUrl: rootURL,
		Name:    "claim-router",
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("missing kind should be InvalidArgument, got %v", err)
	}
}
