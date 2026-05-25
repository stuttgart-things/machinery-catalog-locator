package catalog

// Annotation keys that link a catalog entity back to the Crossplane
// manifest in Git that produced it. The umbrella tool reads these to
// jump from a catalog node to its source-of-truth Claim/XR.
const (
	AnnotationCrossplaneKind       = "machinery.stuttgart-things.com/crossplane-kind"
	AnnotationCrossplaneAPIVersion = "machinery.stuttgart-things.com/crossplane-api-version"
	AnnotationCrossplaneClaim      = "machinery.stuttgart-things.com/crossplane-claim"
	AnnotationCrossplaneXR         = "machinery.stuttgart-things.com/crossplane-xr"
)

// CrossplaneRef describes a Crossplane manifest linked from a catalog
// entity. Source is parsed from the annotation URL via ParseBlobURL,
// so the same value works against either the GitHub reader or the
// LocalReader.
type CrossplaneRef struct {
	Kind       string    // "claim" or "xr"
	Annotation string    // the annotation key that produced this ref
	URL        string    // the raw annotation value
	Source     SourceRef // parsed blob URL
}

// FindByCrossplaneSource returns every catalog entity whose Crossplane
// annotation resolves to a manifest at the given path. Matching is on
// SourceRef.Path only — Owner/Repo/Ref are ignored, mirroring how
// LocalReader treats them. This is the reverse of CrossplaneRefs: given
// a claim/XR file, ask which catalog entity references it.
func FindByCrossplaneSource(roots []*Node, path string) []*Node {
	if path == "" {
		return nil
	}
	var out []*Node
	for _, n := range Resources(roots) {
		for _, ref := range CrossplaneRefs(n.Entity) {
			if ref.Source.Path == path {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// CrossplaneRefs returns every Crossplane manifest reference found on
// an entity's annotations. An empty slice (rather than nil) is returned
// if the entity has no Crossplane annotations.
//
// Malformed URLs are skipped silently — they would only surface a noisy
// error per-resource at display time. Callers that want to validate
// can parse the annotation value themselves via ParseBlobURL.
func CrossplaneRefs(e Entity) []CrossplaneRef {
	if len(e.Metadata.Annotations) == 0 {
		return nil
	}
	var out []CrossplaneRef
	for _, spec := range []struct {
		key, kind string
	}{
		{AnnotationCrossplaneClaim, "claim"},
		{AnnotationCrossplaneXR, "xr"},
	} {
		raw, ok := e.Metadata.Annotations[spec.key]
		if !ok || raw == "" {
			continue
		}
		src, err := ParseBlobURL(raw)
		if err != nil {
			continue
		}
		out = append(out, CrossplaneRef{
			Kind:       spec.kind,
			Annotation: spec.key,
			URL:        raw,
			Source:     src,
		})
	}
	return out
}
