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
internal/config/        environment-based configuration
internal/catalog/       platform-agnostic resolver
  types.go              domain types (SourceRef, Entity, Node)
  reader.go             FileReader interface + LocalReader
  resolver.go           recursive Location resolution with cycle detection
  locationedit.go       YAML editing (remove target/document, preserves comments)
internal/github/        GitHub forge adapter
  client.go             App- or token-based authentication
  reader.go             FileReader implementation
  pr.go                 PR engine (clone/commit/push via go-git)
internal/grpcserver/    CatalogServiceServer implementation
  server.go             unary RPCs (ResolveTree, ListResources, RemoveTarget, DeleteResource)
  watch.go              WatchTree — poll-based snapshot diff streamer
  convert.go            catalog domain ↔ proto translation
internal/web/           HTMX frontend
  web.go                HTTP handlers calling the gRPC server in-process
  templates/*.html      embedded templates (index, tree, action_result)
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
- Each resource exposes `Delete` which POSTs to `DeleteResource`
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
```

For ad-hoc probing use [`grpcurl`](https://github.com/fullstorydev/grpcurl).
Reflection is enabled, so no proto file is needed:

```bash
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 list catalogservice.CatalogService

grpcurl -plaintext \
  -d '{"root_url":"https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml"}' \
  localhost:50051 catalogservice.CatalogService/ResolveTree
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
