package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Resolver loest eine Backstage-Location rekursiv in einen Entitaetsbaum auf.
type Resolver struct {
	Reader FileReader
}

// NewResolver erstellt einen Resolver mit dem angegebenen FileReader.
func NewResolver(r FileReader) *Resolver {
	return &Resolver{Reader: r}
}

// Resolve liest die Root-Location und folgt allen Targets rekursiv.
// Das Ergebnis ist eine Liste von Wurzelknoten (eine Datei kann mehrere
// YAML-Dokumente enthalten).
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
		return nil, nil // Zyklus - Datei wurde bereits verarbeitet
	}
	seen[ref.String()] = true

	data, err := r.Reader.Read(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("lesen von %s: %w", ref, err)
	}

	// Datei kann mehrere ---getrennte Dokumente enthalten.
	var entities []Entity
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var e Entity
		if decErr := dec.Decode(&e); decErr != nil {
			if errors.Is(decErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("yaml-decode %s: %w", ref, decErr)
		}
		if e.Kind == "" {
			continue // leeres Dokument ueberspringen
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

// expandLocation folgt allen Targets einer Location-Entitaet.
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
