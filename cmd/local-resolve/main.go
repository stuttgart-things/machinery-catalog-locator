// Command local-resolve is a tiny offline harness for the catalog
// resolver. It points catalog.LocalReader at a directory of fixtures
// (default: testdata/catalog) and prints the resolved tree, so you can
// iterate on YAML changes without spinning up the gRPC/HTMX server or
// talking to GitHub.
//
// With -claims (the default) the CLI also follows the Crossplane
// annotations on each non-Location entity and prints a summary of the
// linked Claim/XR manifest — proving the round-trip from catalog entity
// to source-of-truth manifest works end-to-end against the same
// LocalReader.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
)

func main() {
	root := flag.String("root", "testdata/catalog", "directory holding the catalog fixtures")
	entry := flag.String("file", "all-locations.yaml", "entry file (relative to -root)")
	catalogEntry := flag.String("catalog", "all-locations.yaml", "catalog root used for the reverse lookup when -file is a Crossplane manifest")
	claims := flag.Bool("claims", true, "follow Crossplane annotations and summarise the linked manifests")
	claimsContent := flag.Bool("claims-content", false, "with -claims, also dump the full manifest YAML")
	nameFilter := flag.String("name", "", "only render the entity matching this name (supports name@namespace)")
	claimOnly := flag.Bool("claim-only", false, "skip the tree render and dump just the linked manifest YAML to stdout")
	flag.Parse()

	reader := catalog.LocalReader{Root: *root}
	resolver := catalog.NewResolver(reader)
	ctx := context.Background()

	nodes, err := resolver.Resolve(ctx, catalog.SourceRef{Path: *entry})
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve:", err)
		os.Exit(1)
	}

	wantName, wantNS := parseName(*nameFilter)

	if *claimOnly {
		if err := dumpClaims(os.Stdout, ctx, reader, nodes, wantName, wantNS); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("%s (root=%s)\n", *entry, *root)
	if wantName == "" {
		for _, n := range nodes {
			printNode(os.Stdout, n, "  ", reader, *claims, *claimsContent, ctx)
		}
	} else {
		matches := filterResources(catalog.Resources(nodes), wantName, wantNS)
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "no entity matched %q\n", *nameFilter)
			os.Exit(1)
		}
		for _, n := range matches {
			printNode(os.Stdout, n, "  ", reader, *claims, *claimsContent, ctx)
		}
	}

	resources := catalog.Resources(nodes)
	fmt.Printf("\nresolved %d non-Location resource(s)\n", len(resources))

	if *entry != *catalogEntry {
		printReverseLookup(os.Stdout, ctx, resolver, *entry, *catalogEntry)
	}
}

// printReverseLookup answers the inverse question: given the picked
// file path, which catalog entity references it via a Crossplane
// annotation? Only runs when the entry differs from the catalog root,
// since otherwise we'd be scanning the same tree we just rendered.
func printReverseLookup(
	w io.Writer,
	ctx context.Context,
	resolver *catalog.Resolver,
	entryPath, catalogPath string,
) {
	catalogRoots, err := resolver.Resolve(ctx, catalog.SourceRef{Path: catalogPath})
	if err != nil {
		fmt.Fprintf(w, "\nreverse lookup skipped (resolve %s: %v)\n", catalogPath, err)
		return
	}
	owners := catalog.FindByCrossplaneSource(catalogRoots, entryPath)
	if len(owners) == 0 {
		return
	}
	fmt.Fprintf(w, "\nreferenced by (via catalog %s):\n", catalogPath)
	for _, n := range owners {
		ns := n.Entity.Metadata.Namespace
		if ns == "" {
			ns = "default"
		}
		fmt.Fprintf(w, "  %s/%s @ %s\n", n.Entity.Kind, n.Entity.Metadata.Name, ns)
		fmt.Fprintf(w, "    catalog file: %s\n", n.Source.Path)
	}
}

// parseName splits a filter expression "name" or "name@namespace".
// An empty namespace means "match any".
func parseName(spec string) (name, namespace string) {
	if spec == "" {
		return "", ""
	}
	if i := strings.Index(spec, "@"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

func filterResources(res []*catalog.Node, name, namespace string) []*catalog.Node {
	norm := func(ns string) string {
		if ns == "" {
			return "default"
		}
		return ns
	}
	var out []*catalog.Node
	for _, n := range res {
		if n.Entity.Metadata.Name != name {
			continue
		}
		if namespace != "" && norm(n.Entity.Metadata.Namespace) != norm(namespace) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// dumpClaims writes the raw YAML of every Crossplane manifest linked
// from the selected entities, separated by `---`. With no -name filter,
// every resource's manifests are dumped (useful for sanity-checking the
// whole catalog); with -name, just the matching entity's.
func dumpClaims(
	w io.Writer,
	ctx context.Context,
	reader catalog.FileReader,
	nodes []*catalog.Node,
	name, namespace string,
) error {
	resources := catalog.Resources(nodes)
	if name != "" {
		resources = filterResources(resources, name, namespace)
		if len(resources) == 0 {
			return fmt.Errorf("no entity matched %q", name)
		}
	}

	wrote := 0
	for _, n := range resources {
		for _, ref := range catalog.CrossplaneRefs(n.Entity) {
			body, err := reader.Read(ctx, ref.Source)
			if err != nil {
				return fmt.Errorf("fetch %s: %w", ref.Source.Path, err)
			}
			if wrote > 0 {
				fmt.Fprintln(w, "---")
			}
			w.Write(body)
			if !bytes.HasSuffix(body, []byte("\n")) {
				fmt.Fprintln(w)
			}
			wrote++
		}
	}
	if wrote == 0 {
		return fmt.Errorf("matched entity has no Crossplane annotations")
	}
	return nil
}

func printNode(
	w io.Writer,
	n *catalog.Node,
	indent string,
	reader catalog.FileReader,
	claims, claimsContent bool,
	ctx context.Context,
) {
	ns := n.Entity.Metadata.Namespace
	if ns == "" {
		ns = "default"
	}
	fmt.Fprintf(w, "%s%s/%s @ %s\n", indent, n.Entity.Kind, n.Entity.Metadata.Name, ns)

	inner := indent + "   "
	fmt.Fprintf(w, "%ssource: %s\n", inner, n.Source.Path)
	if n.ViaTarget != "" {
		fmt.Fprintf(w, "%svia:    %s\n", inner, n.ViaTarget)
	}
	if n.FileDocCount > 1 {
		fmt.Fprintf(w, "%sdoc:    %d/%d\n", inner, n.DocIndex+1, n.FileDocCount)
	}
	if len(n.Entity.Metadata.Annotations) > 0 {
		fmt.Fprintf(w, "%sannotations:\n", inner)
		keys := make([]string, 0, len(n.Entity.Metadata.Annotations))
		for k := range n.Entity.Metadata.Annotations {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := strings.TrimSpace(n.Entity.Metadata.Annotations[k])
			fmt.Fprintf(w, "%s  %s: %s\n", inner, k, v)
		}
	}
	for _, b := range n.Broken {
		fmt.Fprintf(w, "%sBROKEN target %q: %s\n", inner, b.Target, b.Err)
	}

	if claims && !n.Entity.IsLocation() {
		printCrossplaneRefs(w, n, inner, reader, claimsContent, ctx)
	}

	for _, c := range n.Children {
		printNode(w, c, indent+"  ", reader, claims, claimsContent, ctx)
	}
}

func printCrossplaneRefs(
	w io.Writer,
	n *catalog.Node,
	indent string,
	reader catalog.FileReader,
	dumpContent bool,
	ctx context.Context,
) {
	refs := catalog.CrossplaneRefs(n.Entity)
	if len(refs) == 0 {
		return
	}
	fmt.Fprintf(w, "%scrossplane:\n", indent)
	for _, ref := range refs {
		fmt.Fprintf(w, "%s  %s %s\n", indent, ref.Kind, ref.Source.Path)
		body, err := reader.Read(ctx, ref.Source)
		if err != nil {
			fmt.Fprintf(w, "%s    FETCH FAILED: %s\n", indent, err)
			continue
		}
		apiV, kind, name, mNS, err := decodeManifest(body)
		if err != nil {
			fmt.Fprintf(w, "%s    DECODE FAILED: %s\n", indent, err)
			continue
		}
		if mNS == "" {
			mNS = "default"
		}
		fmt.Fprintf(w, "%s    %s/%s name=%s ns=%s\n", indent, apiV, kind, name, mNS)
		if dumpContent {
			for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
				fmt.Fprintf(w, "%s    | %s\n", indent, line)
			}
		}
	}
}

// decodeManifest pulls just the identifying fields out of a Crossplane
// manifest. We intentionally avoid the full catalog.Entity shape — a
// Crossplane Claim/XR has its own spec layout that doesn't match
// Backstage's.
func decodeManifest(body []byte) (apiVersion, kind, name, namespace string, err error) {
	var doc struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	if decErr := dec.Decode(&doc); decErr != nil && !errors.Is(decErr, io.EOF) {
		return "", "", "", "", decErr
	}
	return doc.APIVersion, doc.Kind, doc.Metadata.Name, doc.Metadata.Namespace, nil
}
