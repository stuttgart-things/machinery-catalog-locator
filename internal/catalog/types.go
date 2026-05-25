// Package catalog enthaelt den plattformunabhaengigen Backstage-Location-Resolver.
//
// Das Paket kennt weder GitHub noch GitLab - es arbeitet ausschliesslich gegen
// die FileReader-Schnittstelle. Forge-spezifische Adapter liegen ausserhalb.
package catalog

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// SourceRef zeigt eindeutig auf eine Datei in einem Git-Repository.
type SourceRef struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"` // Branch oder Tag
	Path  string `json:"path"`
}

func (s SourceRef) String() string {
	return fmt.Sprintf("%s/%s@%s:%s", s.Owner, s.Repo, s.Ref, s.Path)
}

// SameRepo meldet, ob zwei Refs auf dasselbe Repo und denselben Branch zeigen.
func (s SourceRef) SameRepo(o SourceRef) bool {
	return s.Owner == o.Owner && s.Repo == o.Repo && s.Ref == o.Ref
}

// Entity ist eine Backstage-Catalog-Entitaet. Spec-Felder sind auf den
// Location-Anteil reduziert; bei Component/API/System bleibt Spec leer.
type Entity struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind"       json:"kind"`
	Metadata   EntityMetadata `yaml:"metadata"   json:"metadata"`
	Spec       LocationSpec   `yaml:"spec"       json:"spec,omitempty"`
}

// IsLocation meldet, ob die Entitaet eine Location ist (case-insensitive).
func (e Entity) IsLocation() bool {
	return strings.EqualFold(e.Kind, "Location")
}

// EntityMetadata haelt die fuer die Anzeige relevanten Metadaten.
type EntityMetadata struct {
	Name        string            `yaml:"name"                  json:"name"`
	Namespace   string            `yaml:"namespace,omitempty"   json:"namespace,omitempty"`
	Title       string            `yaml:"title,omitempty"       json:"title,omitempty"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// LocationSpec deckt sowohl spec.target (Singular) als auch spec.targets ab.
type LocationSpec struct {
	Type    string   `yaml:"type,omitempty"    json:"type,omitempty"`
	Target  string   `yaml:"target,omitempty"  json:"target,omitempty"`
	Targets []string `yaml:"targets,omitempty" json:"targets,omitempty"`
}

// AllTargets liefert Target und Targets als gemeinsame Liste.
func (l LocationSpec) AllTargets() []string {
	var out []string
	if l.Target != "" {
		out = append(out, l.Target)
	}
	return append(out, l.Targets...)
}

// Node ist ein Knoten im aufgeloesten Catalog-Baum.
type Node struct {
	Source       SourceRef      `json:"source"`              // Datei, aus der die Entitaet stammt
	DocIndex     int            `json:"docIndex"`            // Index des YAML-Dokuments in der Datei
	FileDocCount int            `json:"fileDocCount"`        // Anzahl Dokumente in der Datei
	Entity       Entity         `json:"entity"`              // die Entitaet selbst
	Parent       *Node          `json:"-"`                   // verweisende Location (nil = Root)
	ViaTarget    string         `json:"viaTarget,omitempty"` // exakter Target-String im Parent
	Children     []*Node        `json:"children,omitempty"`  // nur bei Location befuellt
	Broken       []BrokenTarget `json:"broken,omitempty"`    // nicht aufloesbare Targets
}

// BrokenTarget dokumentiert ein Target, das nicht gelesen werden konnte.
type BrokenTarget struct {
	Target string `json:"target"`
	Err    string `json:"error"`
}

// ParseBlobURL zerlegt eine GitHub-Blob-URL in eine SourceRef.
// Erwartetes Format: https://github.com/{owner}/{repo}/blob/{ref}/{path...}
func ParseBlobURL(raw string) (SourceRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return SourceRef{}, fmt.Errorf("URL ungueltig: %w", err)
	}
	parts := strings.SplitN(strings.Trim(u.Path, "/"), "/", 5)
	if len(parts) < 5 || parts[2] != "blob" {
		return SourceRef{}, fmt.Errorf("nicht unterstuetzte Blob-URL: %s", raw)
	}
	return SourceRef{Owner: parts[0], Repo: parts[1], Ref: parts[3], Path: parts[4]}, nil
}

// resolveTarget loest einen Target-String relativ zur Parent-Location auf.
func resolveTarget(parent SourceRef, target string) (SourceRef, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return ParseBlobURL(target)
	}
	// relativer Pfad -> relativ zum Verzeichnis der Parent-Datei
	return SourceRef{
		Owner: parent.Owner,
		Repo:  parent.Repo,
		Ref:   parent.Ref,
		Path:  path.Join(path.Dir(parent.Path), target),
	}, nil
}

// Flatten liefert alle Knoten eines Baums als flache Liste (Tiefensuche).
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

// Resources liefert alle Nicht-Location-Knoten (die "echten" Ressourcen).
func Resources(roots []*Node) []*Node {
	var out []*Node
	for _, n := range Flatten(roots) {
		if !n.Entity.IsLocation() {
			out = append(out, n)
		}
	}
	return out
}

// Find sucht einen Nicht-Location-Knoten nach Kind, Name und Namespace.
// Ein leerer Namespace matcht "default".
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
