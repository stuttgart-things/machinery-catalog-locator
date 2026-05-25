// Package grpcserver implements the CatalogService gRPC API. It binds
// the platform-agnostic resolver (internal/catalog) and the GitHub forge
// adapter (internal/github) behind a stable proto contract so external
// tools — including the umbrella that fronts machinery + this service —
// can speak to it without depending on internals.
package grpcserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
	ghforge "github.com/stuttgart-things/machinery-catalog-locator/internal/github"
)

// Server implements catalogservice.CatalogServiceServer.
type Server struct {
	catalogservice.UnimplementedCatalogServiceServer

	Resolver *catalog.Resolver
	Reader   catalog.FileReader
	PR       *ghforge.PRService
}

func (s *Server) ResolveTree(ctx context.Context, req *catalogservice.ResolveTreeRequest) (*catalogservice.ResolveTreeResponse, error) {
	root, err := catalog.ParseBlobURL(req.GetRootUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "root_url: %v", err)
	}
	nodes, err := s.Resolver.Resolve(ctx, root)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}
	out := &catalogservice.ResolveTreeResponse{}
	for _, n := range nodes {
		out.Roots = append(out.Roots, toProtoNode(n))
	}
	return out, nil
}

func (s *Server) ListResources(ctx context.Context, req *catalogservice.ListResourcesRequest) (*catalogservice.ListResourcesResponse, error) {
	root, err := catalog.ParseBlobURL(req.GetRootUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "root_url: %v", err)
	}
	nodes, err := s.Resolver.Resolve(ctx, root)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}
	kindFilter := strings.TrimSpace(req.GetKind())
	out := &catalogservice.ListResourcesResponse{}
	for _, n := range catalog.Resources(nodes) {
		if kindFilter != "" && !strings.EqualFold(kindFilter, n.Entity.Kind) {
			continue
		}
		out.Resources = append(out.Resources, &catalogservice.Resource{
			Kind:      n.Entity.Kind,
			Name:      n.Entity.Metadata.Name,
			Namespace: n.Entity.Metadata.Namespace,
			Source:    toProtoSource(n.Source),
			ViaTarget: n.ViaTarget,
		})
	}
	return out, nil
}

func (s *Server) RemoveTarget(ctx context.Context, req *catalogservice.RemoveTargetRequest) (*catalogservice.RemoveTargetResponse, error) {
	if s.PR == nil {
		return nil, status.Error(codes.Unimplemented, "PR service not configured (running in local-root mode?)")
	}
	locRef, err := catalog.ParseBlobURL(req.GetLocationUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "location_url: %v", err)
	}
	if req.GetTarget() == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}

	raw, err := s.Reader.Read(ctx, locRef)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "read location: %v", err)
	}
	patched, err := catalog.RemoveTargetFromLocation(raw, req.GetTarget())
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "remove target: %v", err)
	}

	url, err := s.PR.OpenPullRequest(ctx, ghforge.PRRequest{
		Owner:         locRef.Owner,
		Repo:          locRef.Repo,
		BaseBranch:    locRef.Ref,
		HeadBranch:    branchName("remove-target", req.GetTarget()),
		Title:         fmt.Sprintf("chore(catalog): remove target from %s", locRef.Path),
		Body:          fmt.Sprintf("Removes target `%s` from `%s`.", req.GetTarget(), locRef.Path),
		CommitMessage: fmt.Sprintf("chore(catalog): remove target %s", req.GetTarget()),
		Edits:         map[string][]byte{locRef.Path: patched},
	})
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "open PR: %v", err)
	}
	return &catalogservice.RemoveTargetResponse{PullRequestUrl: url}, nil
}

func (s *Server) DeleteResource(ctx context.Context, req *catalogservice.DeleteResourceRequest) (*catalogservice.DeleteResourceResponse, error) {
	if s.PR == nil {
		return nil, status.Error(codes.Unimplemented, "PR service not configured (running in local-root mode?)")
	}
	root, err := catalog.ParseBlobURL(req.GetRootUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "root_url: %v", err)
	}
	if req.GetKind() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "kind and name are required")
	}

	nodes, err := s.Resolver.Resolve(ctx, root)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}
	node, ok := catalog.Find(nodes, req.GetKind(), req.GetName(), req.GetNamespace())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s/%s not in tree", req.GetKind(), req.GetName())
	}
	if node.Parent == nil {
		return nil, status.Error(codes.FailedPrecondition, "resource is a root Location; cannot delete this way")
	}

	prReq := ghforge.PRRequest{
		Owner:         node.Source.Owner,
		Repo:          node.Source.Repo,
		BaseBranch:    node.Source.Ref,
		HeadBranch:    branchName("delete", node.Entity.Kind+"-"+node.Entity.Metadata.Name),
		Title:         fmt.Sprintf("chore(catalog): remove %s/%s", node.Entity.Kind, node.Entity.Metadata.Name),
		CommitMessage: fmt.Sprintf("chore(catalog): remove %s/%s", node.Entity.Kind, node.Entity.Metadata.Name),
		Edits:         map[string][]byte{},
	}

	docExtracted := false
	if node.FileDocCount > 1 {
		raw, err := s.Reader.Read(ctx, node.Source)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "read resource file: %v", err)
		}
		patched, err := catalog.RemoveDocumentFromFile(raw, node.DocIndex)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "extract document: %v", err)
		}
		prReq.Edits[node.Source.Path] = patched
		docExtracted = true
	} else {
		prReq.Deletes = []string{node.Source.Path}
	}

	parentTouched := node.Parent.Source.SameRepo(node.Source)
	if parentTouched {
		raw, err := s.Reader.Read(ctx, node.Parent.Source)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "read parent location: %v", err)
		}
		patched, err := catalog.RemoveTargetFromLocation(raw, node.ViaTarget)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "remove parent target: %v", err)
		}
		prReq.Edits[node.Parent.Source.Path] = patched
	}

	prReq.Body = fmt.Sprintf(
		"Removes `%s/%s` from the catalog.\n\n- File: `%s`\n- Parent location: `%s`",
		node.Entity.Kind, node.Entity.Metadata.Name,
		node.Source.Path, node.Parent.Source.Path,
	)

	url, err := s.PR.OpenPullRequest(ctx, prReq)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "open PR: %v", err)
	}
	return &catalogservice.DeleteResourceResponse{
		PullRequestUrl:    url,
		ParentTouched:     parentTouched,
		DocumentExtracted: docExtracted,
	}, nil
}

// GetEntityManifest fetches the Crossplane Claim/XR manifests linked
// from a catalog entity via the machinery.stuttgart-things.com/crossplane-*
// annotations. The catalog is resolved once to locate the entity, then
// each annotated manifest is read through s.Reader using the same
// credentials as the rest of the service.
func (s *Server) GetEntityManifest(ctx context.Context, req *catalogservice.GetEntityManifestRequest) (*catalogservice.GetEntityManifestResponse, error) {
	root, err := catalog.ParseBlobURL(req.GetRootUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "root_url: %v", err)
	}
	if req.GetKind() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "kind and name are required")
	}

	nodes, err := s.Resolver.Resolve(ctx, root)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}
	node, ok := catalog.Find(nodes, req.GetKind(), req.GetName(), req.GetNamespace())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s/%s not in tree", req.GetKind(), req.GetName())
	}

	refs := catalog.CrossplaneRefs(node.Entity)
	if len(refs) == 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"%s/%s has no machinery.stuttgart-things.com/crossplane-* annotations",
			req.GetKind(), req.GetName())
	}

	out := &catalogservice.GetEntityManifestResponse{}
	for _, ref := range refs {
		body, err := s.Reader.Read(ctx, ref.Source)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "read %s: %v", ref.Source.Path, err)
		}
		out.Manifests = append(out.Manifests, &catalogservice.CrossplaneManifest{
			LinkKind:   ref.Kind,
			Annotation: ref.Annotation,
			Url:        ref.URL,
			Source:     toProtoSource(ref.Source),
			Body:       body,
		})
	}
	return out, nil
}

// ListEntitiesByCrossplaneSource is the reverse of GetEntityManifest:
// given a Crossplane manifest URL, return the catalog entities whose
// machinery.stuttgart-things.com/crossplane-* annotations reference it.
// Matching is on the URL's path only — owner/repo/ref are ignored,
// consistent with how LocalReader and the rest of the package treat
// blob URLs.
func (s *Server) ListEntitiesByCrossplaneSource(ctx context.Context, req *catalogservice.ListEntitiesByCrossplaneSourceRequest) (*catalogservice.ListEntitiesByCrossplaneSourceResponse, error) {
	root, err := catalog.ParseBlobURL(req.GetRootUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "root_url: %v", err)
	}
	manifestRef, err := catalog.ParseBlobURL(req.GetManifestUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "manifest_url: %v", err)
	}

	nodes, err := s.Resolver.Resolve(ctx, root)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}

	out := &catalogservice.ListEntitiesByCrossplaneSourceResponse{}
	for _, n := range catalog.FindByCrossplaneSource(nodes, manifestRef.Path) {
		out.Entities = append(out.Entities, &catalogservice.Resource{
			Kind:      n.Entity.Kind,
			Name:      n.Entity.Metadata.Name,
			Namespace: n.Entity.Metadata.Namespace,
			Source:    toProtoSource(n.Source),
			ViaTarget: n.ViaTarget,
		})
	}
	return out, nil
}

// branchName builds a deterministic-ish branch name with a unix-second
// suffix so repeated PRs against the same artifact don't collide.
func branchName(prefix, hint string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, hint)
	clean = strings.Trim(strings.ToLower(clean), "-")
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return fmt.Sprintf("machinery-catalog-locator/%s-%s-%d", prefix, clean, time.Now().Unix())
}
