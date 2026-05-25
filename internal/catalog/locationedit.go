package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// RemoveTargetFromLocation entfernt einen Target-Eintrag aus einer
// location.yaml. Kommentare und Schluessel-Reihenfolge bleiben erhalten,
// da ueber die yaml.Node-API gearbeitet wird.
//
// Behandelt sowohl spec.targets (Liste) als auch spec.target (Singular).
// Gibt einen Fehler zurueck, wenn das Target nicht gefunden wurde.
func RemoveTargetFromLocation(content []byte, target string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("yaml-parse: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("leeres YAML-Dokument")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("Wurzel ist kein Mapping")
	}
	spec := mapValue(root, "spec")
	if spec == nil || spec.Kind != yaml.MappingNode {
		return nil, errors.New("kein spec-Mapping vorhanden")
	}

	removed := false

	// spec.targets (Liste)
	if t := mapValue(spec, "targets"); t != nil && t.Kind == yaml.SequenceNode {
		kept := t.Content[:0]
		for _, item := range t.Content {
			if item.Value == target {
				removed = true
				continue
			}
			kept = append(kept, item)
		}
		t.Content = kept
	}

	// spec.target (Singular) -> kompletten Key entfernen, wenn er matcht
	if tv := mapValue(spec, "target"); tv != nil && tv.Value == target {
		spec.Content = removeMapKey(spec.Content, "target")
		removed = true
	}

	if !removed {
		return nil, fmt.Errorf("target %q nicht in Location gefunden", target)
	}
	return encodeNode(&doc)
}

// RemoveDocumentFromFile entfernt das YAML-Dokument mit dem Index docIndex
// aus einer Multi-Dokument-Datei und gibt den Rest zurueck.
// Wird genutzt, wenn eine Ressource mit anderen Entitaeten in einer Datei liegt.
func RemoveDocumentFromFile(content []byte, docIndex int) ([]byte, error) {
	var docs []*yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("yaml-decode: %w", err)
		}
		docs = append(docs, &n)
	}
	if docIndex < 0 || docIndex >= len(docs) {
		return nil, fmt.Errorf("dokument-index %d ausserhalb des Bereichs (0..%d)", docIndex, len(docs)-1)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for i, d := range docs {
		if i == docIndex {
			continue
		}
		if err := enc.Encode(d); err != nil {
			_ = enc.Close()
			return nil, fmt.Errorf("yaml-encode: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeNode(n *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("yaml-encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mapValue liefert den Value-Node zu einem Key in einem Mapping-Node.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// removeMapKey entfernt ein Key/Value-Paar aus dem Content-Slice eines Mappings.
func removeMapKey(content []*yaml.Node, key string) []*yaml.Node {
	out := content[:0]
	for i := 0; i+1 < len(content); i += 2 {
		if content[i].Value == key {
			continue
		}
		out = append(out, content[i], content[i+1])
	}
	return out
}
