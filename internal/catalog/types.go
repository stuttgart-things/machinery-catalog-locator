// Package catalog contains the platform-agnostic Backstage Location
// resolver.
//
// The package knows nothing about GitHub or GitLab — it works only
// against the FileReader interface. Forge-specific adapters live
// outside this package.
package catalog

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// SourceRef uniquely identifies a file in a Git repository.
type SourceRef struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"` // branch or tag
	Path  string `json:"path"`
}

func (s SourceRef) String() string {
	return fmt.Sprintf("%s/%s@%s:%s", s.Owner, s.Repo, s.Ref, s.Path)
}

// SameRepo reports whether two refs point at the same repo and branch.
func (s SourceRef) SameRepo(o SourceRef) bool {
	return s.Owner == o.Owner && s.Repo == o.Repo && s.Ref == o.Ref
}

// Entity is a Backstage catalog entity. Spec is narrowed to the
// Location-relevant fields; Component/API/System entities keep Spec
// empty.
type Entity struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind"       json:"kind"`
	Metadata   EntityMetadata `yaml:"metadata"   json:"metadata"`
	Spec       LocationSpec   `yaml:"spec"       json:"spec,omitempty"`
}

// IsLocation reports whether the entity is a Location (case-insensitive).
func (e Entity) IsLocation() bool {
	return strings.EqualFold(e.Kind, "Location")
}

// EntityMetadata holds the metadata fields relevant to rendering.
type EntityMetadata struct {
	Name        string            `yaml:"name"                  json:"name"`
	Namespace   string            `yaml:"namespace,omitempty"   json:"namespace,omitempty"`
	Title       string            `yaml:"title,omitempty"       json:"title,omitempty"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// LocationSpec covers both spec.target (singular) and spec.targets (list).
type LocationSpec struct {
	Type    string   `yaml:"type,omitempty"    json:"type,omitempty"`
	Target  string   `yaml:"target,omitempty"  json:"target,omitempty"`
	Targets []string `yaml:"targets,omitempty" json:"targets,omitempty"`
}

// AllTargets returns Target and Targets as a single list.
func (l LocationSpec) AllTargets() []string {
	var out []string
	if l.Target != "" {
		out = append(out, l.Target)
	}
	return append(out, l.Targets...)
}

// Node is a node in the resolved catalog tree.
type Node struct {
	Source       SourceRef      `json:"source"`              // file the entity came from
	DocIndex     int            `json:"docIndex"`            // YAML document index within the file
	FileDocCount int            `json:"fileDocCount"`        // total documents in the file
	Entity       Entity         `json:"entity"`              // the entity itself
	Parent       *Node          `json:"-"`                   // referring Location (nil = root)
	ViaTarget    string         `json:"viaTarget,omitempty"` // exact target string in the parent
	Children     []*Node        `json:"children,omitempty"`  // populated only for Locations
	Broken       []BrokenTarget `json:"broken,omitempty"`    // targets that could not be resolved
}

// BrokenTarget records a target that could not be read.
type BrokenTarget struct {
	Target string `json:"target"`
	Err    string `json:"error"`
}

// ParseBlobURL splits a GitHub blob URL into a SourceRef.
// Expected format: https://github.com/{owner}/{repo}/blob/{ref}/{path...}
func ParseBlobURL(raw string) (SourceRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return SourceRef{}, fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.SplitN(strings.Trim(u.Path, "/"), "/", 5)
	if len(parts) < 5 || parts[2] != "blob" {
		return SourceRef{}, fmt.Errorf("unsupported blob URL: %s", raw)
	}
	return SourceRef{Owner: parts[0], Repo: parts[1], Ref: parts[3], Path: parts[4]}, nil
}

// resolveTarget resolves a target string relative to the parent location.
func resolveTarget(parent SourceRef, target string) (SourceRef, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return ParseBlobURL(target)
	}
	// Relative path — relative to the parent file's directory.
	return SourceRef{
		Owner: parent.Owner,
		Repo:  parent.Repo,
		Ref:   parent.Ref,
		Path:  path.Join(path.Dir(parent.Path), target),
	}, nil
}

// Flatten returns every node of a forest as a flat slice (depth-first).
func Flatten(roots []*Node) []*Node {
	var out []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}

// Resources returns every non-Location node — the real resources.
func Resources(roots []*Node) []*Node {
	var out []*Node
	for _, n := range Flatten(roots) {
		if !n.Entity.IsLocation() {
			out = append(out, n)
		}
	}
	return out
}

// Find looks up a non-Location node by kind, name, and namespace.
// An empty namespace matches "default".
func Find(roots []*Node, kind, name, namespace string) (*Node, bool) {
	norm := func(ns string) string {
		if ns == "" {
			return "default"
		}
		return ns
	}
	for _, n := range Resources(roots) {
		if strings.EqualFold(n.Entity.Kind, kind) &&
			n.Entity.Metadata.Name == name &&
			norm(n.Entity.Metadata.Namespace) == norm(namespace) {
			return n, true
		}
	}
	return nil, false
}
