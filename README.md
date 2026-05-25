# machinery-catalog-locator

The **GitOps half** of the [machinery](https://github.com/stuttgart-things/machinery)
stack: machinery shows the live status of resources in the cluster;
machinery-catalog-locator shows the same resources from the Git side
(the catalog declarations that produced them) and removes them via
Pull Request when they should go away.

Two surfaces, sharing one implementation:

- **gRPC API** &mdash; for the umbrella tool that fronts both services
- **HTMX dashboard** &mdash; for humans

Both go through the same in-process `CatalogService` so behaviour and
errors are identical regardless of the caller.

The resolver is **platform-agnostic** — it depends only on the
`catalog.FileReader` interface. The GitHub adapter (reads via the
Contents API, PRs via go-git + go-github) is isolated in
`internal/github`; a GitLab adapter can be added without touching the
resolver.

## Layout

```
catalogservice/         catalog_service.proto + generated Go (gRPC contract)
cmd/server/             main.go — gRPC server + HTMX HTTP server, graceful shutdown
cmd/local-resolve/      main.go — offline CLI that resolves testdata via LocalReader
internal/config/        environment-based configuration
internal/catalog/       platform-agnostic resolver
  types.go              domain types (SourceRef, Entity, Node)
  reader.go             FileReader interface + LocalReader (StripPathPrefix)
  resolver.go           recursive Location resolution with cycle detection
  locationedit.go       YAML editing (remove target/document, preserves comments)
  crossplane.go         CrossplaneRefs + FindByCrossplaneSource — catalog ↔ claim
internal/github/        GitHub forge adapter
  client.go             App- or token-based authentication
  reader.go             FileReader implementation
  pr.go                 PR engine (clone/commit/push via go-git)
internal/grpcserver/    CatalogServiceServer implementation
  server.go             unary RPCs (ResolveTree, ListResources, RemoveTarget,
                        DeleteResource, GetEntityManifest, ListEntitiesByCrossplaneSource)
  watch.go              WatchTree — poll-based snapshot diff streamer
  convert.go            catalog domain ↔ proto translation
internal/web/           HTMX frontend
  web.go                HTTP handlers calling the gRPC server in-process
  templates/*.html      embedded templates (index, tree, action_result)
testdata/catalog/       fixtures: catalog tree + linked Crossplane manifests
  crossplane-claims/    real Claim/XR manifests referenced from catalog entities
```

## Concept

Backstage walks from a few entry points through `Location` entities into
more files — a directed graph. machinery-catalog-locator rebuilds that
graph: each resolved node knows the file it came from (`Source`), the
referring location (`Parent`), and the exact target string
(`ViaTarget`). Those three pieces are exactly what deletion needs:
removing a resource and its referring target in a **single PR** keeps
Backstage from logging an error for the orphaned target.

## gRPC API

Defined in [`catalogservice/catalog_service.proto`](catalogservice/catalog_service.proto).

| RPC               | Type          | Purpose                                                                 |
|-------------------|---------------|-------------------------------------------------------------------------|
| `ResolveTree`     | unary         | Full catalog graph rooted at a blob URL                                 |
| `ListResources`   | unary         | Flat list of all non-Location entities (optional kind filter)           |
| `RemoveTarget`    | unary         | Open a PR removing one target from a `location.yaml`                    |
| `DeleteResource`  | unary         | Open a PR removing a resource and the referring parent target          |
| `WatchTree`       | server-stream | Replay snapshot as `ADDED`, then stream poll diffs (`ADDED`/`MODIFIED`/`DELETED`) |
| `GetEntityManifest` | unary       | Fetch the Crossplane Claim/XR manifests linked from a catalog entity    |
| `ListEntitiesByCrossplaneSource` | unary | Reverse lookup: which catalog entities reference this Crossplane manifest URL |

Health: standard `grpc.health.v1.Health`.
Keepalive: 2 min ping / 20 s timeout — long-lived `WatchTree` streams
survive idle gateway timeouts.

Generate Go code after editing the proto:

```bash
task proto
```

(Requires `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` on `PATH`.)

## HTMX UI

A small dark-themed dashboard at `HTTP_PORT` (default `:8080`):

- Enter a root blob URL → resolves and shows the full tree
- Each `Location` row exposes `✕ <target>` buttons that POST to
  `RemoveTarget`
- Each resource exposes `Delete` which POSTs to `DeleteResource`, and
  `View claim` which GETs `GetEntityManifest` and swaps the linked
  Crossplane YAML in below the node
- The action panel above the tree shows the resulting PR URL (or the
  error)

Every HTMX form calls the same `grpcserver.Server` methods that remote
gRPC callers hit — there is no separate REST surface to maintain.

## Setup

Create a GitHub App (recommended) with repository permissions *Contents:
Read & write* and *Pull requests: Read & write*. Download the private
key as PEM. For local development a `GITHUB_TOKEN` is sufficient.

```bash
cp .env.example .env      # fill in the values
task tidy                 # resolve dependencies (network required)
task test
task run                  # gRPC on :50051, HTMX on :8080
```

## Local development (no GitHub)

`cmd/local-resolve` drives the resolver against a directory of fixtures
through `catalog.LocalReader`, so you can iterate on YAML without
credentials or network access. The `testdata/catalog` tree (see its
[README](testdata/catalog/README.md)) covers the multi-target root,
multi-document files, nested locations, cycles, and broken targets.

```bash
task local                                         # gum picker over every *.yaml
task local FILE=edge-cases/singular-target.yaml    # skip the picker
go run ./cmd/local-resolve -root testdata/catalog -file all-locations.yaml
```

Output is an indented tree showing each entity's kind/name/namespace,
the file it came from, the target it was reached by, and any
annotations.

### Linking entities to Crossplane claims / XRs

Catalog entities reference their Crossplane source manifest in Git via
`metadata.annotations`. The convention used in `testdata/catalog`:

| Annotation | Purpose |
|------------|---------|
| `machinery.stuttgart-things.com/crossplane-kind`        | XR / Claim `kind`         |
| `machinery.stuttgart-things.com/crossplane-api-version` | XR / Claim `apiVersion`   |
| `machinery.stuttgart-things.com/crossplane-claim`       | Git blob URL of the Claim |
| `machinery.stuttgart-things.com/crossplane-xr`          | Git blob URL of the XR    |

```yaml
metadata:
  name: claim-router
  namespace: insurance
  annotations:
    machinery.stuttgart-things.com/crossplane-kind: ClaimRouter
    machinery.stuttgart-things.com/crossplane-api-version: insurance.stuttgart-things.com/v1alpha1
    machinery.stuttgart-things.com/crossplane-claim: https://github.com/stuttgart-things/catalog/blob/main/crossplane-claims/insurance/claim-router.yaml
```

Annotations ride on the existing `EntityMetadata` field — they flow
through the gRPC `Entity` message and the HTMX templates unchanged, so
downstream tools (the umbrella dashboard, `machinery`) can read them
without any schema work.

The helper `catalog.CrossplaneRefs(entity)` returns the parsed
references (kind=`"claim"|"xr"`, URL, `SourceRef`) so callers can fetch
the linked manifest with the same `FileReader` they already use for the
catalog. Because `ParseBlobURL` populates `SourceRef.Path` from the
blob URL's path component, and `LocalReader` ignores Owner/Repo/Ref,
the **same annotation URL works against both GitHub and the local
fixtures** — as long as the URL's path matches the fixture layout under
`testdata/catalog/`. That's how `task local` round-trips from a catalog
entity to its Crossplane manifest with no remote calls.

`cmd/local-resolve` follows the annotations by default:

```bash
task local                                  # prints summary of each linked manifest
go run ./cmd/local-resolve -claims=false    # skip the manifest fetch
go run ./cmd/local-resolve -claims-content  # also dump the full manifest YAML
```

### Resolving just one entity's claim

To go straight from a catalog entity name to its Crossplane manifest:

```bash
task claim NAME=claim-router                # YAML to stdout
task claim NAME=claim-router@insurance      # namespace-qualified lookup
task claim NAME=payment-api-db > /tmp/claim.yaml | kubectl apply -f -

# Equivalent direct invocation:
go run ./cmd/local-resolve -name claim-router -claim-only
```

`-name` alone (without `-claim-only`) keeps the tree render but limits
it to the matching entity, which is handy for inspecting one resource's
annotations + linked manifest in one shot.

Programmatic equivalent for callers embedding the package:

```go
roots, _ := resolver.Resolve(ctx, root)
node, _ := catalog.Find(roots, "Component", "claim-router", "insurance")
for _, ref := range catalog.CrossplaneRefs(node.Entity) {
    body, _ := reader.Read(ctx, ref.Source) // same reader, no new auth
    // ref.Kind ∈ {"claim","xr"}; body is the raw manifest YAML
}
```

### Reverse lookup: claim → catalog entity

When you point `local-resolve` (or `task local` via the gum picker) at
a Crossplane manifest, it also scans the catalog tree (default
`all-locations.yaml`, override with `-catalog`) and reports the catalog
entity referencing it:

```
$ go run ./cmd/local-resolve -file crossplane-claims/insurance/claim-router.yaml
…
resolved 1 non-Location resource(s)

referenced by (via catalog all-locations.yaml):
  Component/claim-router @ insurance
    catalog file: components/claim-router/catalog-info.yaml
```

The underlying helper is `catalog.FindByCrossplaneSource(roots, path)`
— takes a path, returns every catalog node whose Crossplane annotation
resolves to it.

### Running the full server locally (no GitHub)

`cmd/server -local-root <dir>` boots the same gRPC + HTMX server the
production binary runs, but with `catalog.LocalReader` instead of the
GitHub adapter — no credentials needed. Read-only RPCs (`ResolveTree`,
`ListResources`, `GetEntityManifest`, `ListEntitiesByCrossplaneSource`)
work normally; the PR-opening ones (`RemoveTarget`, `DeleteResource`)
return `Unimplemented`.

```bash
task run-local
# gRPC on :50051, HTMX on :8080

# Open http://localhost:8080 in a browser and paste any GitHub blob URL
# pointing at testdata/catalog/all-locations.yaml — even the realistic
# https://github.com/stuttgart-things/machinery-catalog-locator/blob/main/testdata/catalog/all-locations.yaml
# works (see the prefix-stripping note below).
```

URLs whose path includes the local-root prefix
(`testdata/catalog/all-locations.yaml`) would otherwise produce the
doubled path `testdata/catalog/testdata/catalog/all-locations.yaml`.
The server defaults `LocalReader.StripPathPrefix` to the local root so
the prefix is removed before joining — annotation URLs (which use the
shorter `crossplane-claims/...` paths) are untouched and resolve
directly.

## Calling from another tool

A third tool (e.g. a unified machinery + catalog-locator dashboard)
imports the generated client and dials the gRPC server:

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
)

conn, _ := grpc.Dial("catalog-locator:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := catalogservice.NewCatalogServiceClient(conn)

tree, _ := client.ResolveTree(ctx, &catalogservice.ResolveTreeRequest{
    RootUrl: "https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml",
})

// "Show this entity's claim" — one RPC, server handles the GitHub fetch.
manifests, _ := client.GetEntityManifest(ctx, &catalogservice.GetEntityManifestRequest{
    RootUrl:   "https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml",
    Kind:      "Component",
    Name:      "claim-router",
    Namespace: "insurance",
})
for _, m := range manifests.Manifests {
    // m.LinkKind ∈ {"claim","xr"}, m.Source identifies the file, m.Body is raw YAML.
}
```

For ad-hoc probing use [`grpcurl`](https://github.com/fullstorydev/grpcurl).
Reflection is enabled, so no proto file is needed:

```bash
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 list catalogservice.CatalogService

grpcurl -plaintext \
  -d '{"root_url":"https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml"}' \
  localhost:50051 catalogservice.CatalogService/ResolveTree

# Fetch the Crossplane Claim linked from a single catalog entity:
grpcurl -plaintext \
  -d '{
    "root_url":"https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml",
    "kind":"Component","name":"claim-router","namespace":"insurance"
  }' \
  localhost:50051 catalogservice.CatalogService/GetEntityManifest

# Reverse: which catalog entity references this Crossplane manifest?
grpcurl -plaintext \
  -d '{
    "root_url":"https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml",
    "manifest_url":"https://github.com/stuttgart-things/catalog/blob/main/crossplane-claims/insurance/claim-router.yaml"
  }' \
  localhost:50051 catalogservice.CatalogService/ListEntitiesByCrossplaneSource
```

## Notes & limits

- If a file contains several `---`-separated entities, deletion extracts
  only the affected document instead of removing the whole file.
- Cross-repo: when a location points into another repo via an absolute
  blob URL, the parent target is **not** removed automatically (a
  separate PR is required). `DeleteResourceResponse.parent_touched`
  reports this.
- `WatchTree` is poll-based — there is no native push for Git contents.
  The interval is clamped to `[5s, 1h]`; two consecutive resolve
  failures end the stream so a transient GitHub outage doesn't tear
  down every watcher silently.
- The `LocalReader` in `internal/catalog/reader.go` enables offline
  validation and is used by the tests.

## Roadmap

- GitLab adapter: implement `catalog.FileReader` plus a `PRService`
  equivalent — the resolver stays untouched.
- Optional mode that uses the Backstage Catalog API
  (`/api/catalog/entities`, annotation `backstage.io/managed-by-location`)
  instead of resolving on its own.
