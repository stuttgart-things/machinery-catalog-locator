package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// RemoveTargetFromLocation removes one target entry from a location.yaml.
// Comments and key order survive because the edit goes through the
// yaml.Node API.
//
// Handles both spec.targets (list) and spec.target (singular). Returns
// an error when the target is not present.
func RemoveTargetFromLocation(content []byte, target string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("empty YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("root is not a mapping")
	}
	spec := mapValue(root, "spec")
	if spec == nil || spec.Kind != yaml.MappingNode {
		return nil, errors.New("no spec mapping present")
	}

	removed := false

	// spec.targets (list)
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

	// spec.target (singular) — remove the whole key if it matches.
	if tv := mapValue(spec, "target"); tv != nil && tv.Value == target {
		spec.Content = removeMapKey(spec.Content, "target")
		removed = true
	}

	if !removed {
		return nil, fmt.Errorf("target %q not found in Location", target)
	}
	return encodeNode(&doc)
}

// RemoveDocumentFromFile removes the YAML document at docIndex from a
// multi-document file and returns the remainder. Used when a resource
// shares a file with other entities.
func RemoveDocumentFromFile(content []byte, docIndex int) ([]byte, error) {
	var docs []*yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("yaml decode: %w", err)
		}
		docs = append(docs, &n)
	}
	if docIndex < 0 || docIndex >= len(docs) {
		return nil, fmt.Errorf("document index %d out of range (0..%d)", docIndex, len(docs)-1)
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
			return nil, fmt.Errorf("yaml encode: %w", err)
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
		return nil, fmt.Errorf("yaml encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mapValue returns the value node for a key in a mapping node.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// removeMapKey removes a key/value pair from a mapping's Content slice.
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
