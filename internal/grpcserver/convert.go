package grpcserver

import (
	"github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
)

func toProtoSource(s catalog.SourceRef) *catalogservice.SourceRef {
	return &catalogservice.SourceRef{
		Owner: s.Owner,
		Repo:  s.Repo,
		Ref:   s.Ref,
		Path:  s.Path,
	}
}

func toProtoEntity(e catalog.Entity) *catalogservice.Entity {
	out := &catalogservice.Entity{
		ApiVersion: e.APIVersion,
		Kind:       e.Kind,
		Metadata: &catalogservice.EntityMetadata{
			Name:        e.Metadata.Name,
			Namespace:   e.Metadata.Namespace,
			Title:       e.Metadata.Title,
			Description: e.Metadata.Description,
			Annotations: e.Metadata.Annotations,
		},
	}
	if e.IsLocation() {
		out.Targets = e.Spec.AllTargets()
	}
	return out
}

func toProtoNode(n *catalog.Node) *catalogservice.Node {
	if n == nil {
		return nil
	}
	out := &catalogservice.Node{
		Source:       toProtoSource(n.Source),
		DocIndex:     int32(n.DocIndex),
		FileDocCount: int32(n.FileDocCount),
		Entity:       toProtoEntity(n.Entity),
		ViaTarget:    n.ViaTarget,
	}
	for _, c := range n.Children {
		out.Children = append(out.Children, toProtoNode(c))
	}
	for _, b := range n.Broken {
		out.Broken = append(out.Broken, &catalogservice.BrokenTarget{
			Target: b.Target, Error: b.Err,
		})
	}
	return out
}
