package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Resolver recursively resolves a Backstage Location into an entity tree.
type Resolver struct {
	Reader FileReader
}

func NewResolver(r FileReader) *Resolver {
	return &Resolver{Reader: r}
}

// Resolve reads the root location and follows every target recursively.
// The result is a slice of root nodes — a file can hold several YAML
// documents.
func (r *Resolver) Resolve(ctx context.Context, root SourceRef) ([]*Node, error) {
	return r.resolveFile(ctx, root, nil, "", map[string]bool{})
}

func (r *Resolver) resolveFile(
	ctx context.Context,
	ref SourceRef,
	parent *Node,
	viaTarget string,
	seen map[string]bool,
) ([]*Node, error) {
	if seen[ref.String()] {
		return nil, nil // cycle — file already processed
	}
	seen[ref.String()] = true

	data, err := r.Reader.Read(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ref, err)
	}

	// A file can contain multiple ---separated documents.
	var entities []Entity
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var e Entity
		if decErr := dec.Decode(&e); decErr != nil {
			if errors.Is(decErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("yaml decode %s: %w", ref, decErr)
		}
		if e.Kind == "" {
			continue // skip empty document
		}
		entities = append(entities, e)
	}

	nodes := make([]*Node, 0, len(entities))
	for i, e := range entities {
		n := &Node{
			Source:       ref,
			DocIndex:     i,
			FileDocCount: len(entities),
			Entity:       e,
			Parent:       parent,
			ViaTarget:    viaTarget,
		}
		if e.IsLocation() {
			r.expandLocation(ctx, n, seen)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// expandLocation follows every target of a Location entity.
func (r *Resolver) expandLocation(ctx context.Context, n *Node, seen map[string]bool) {
	for _, t := range n.Entity.Spec.AllTargets() {
		childRef, err := resolveTarget(n.Source, t)
		if err != nil {
			n.Broken = append(n.Broken, BrokenTarget{Target: t, Err: err.Error()})
			continue
		}
		kids, err := r.resolveFile(ctx, childRef, n, t, seen)
		if err != nil {
			n.Broken = append(n.Broken, BrokenTarget{Target: t, Err: err.Error()})
			continue
		}
		n.Children = append(n.Children, kids...)
	}
}
