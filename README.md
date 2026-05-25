# machinery-catalog-locator

A Go API that recursively resolves Backstage `Location` entities, lists every
referenced resource, and can remove resources from Git via Pull Request.

The resolver is **platform-agnostic** — it depends only on the
`catalog.FileReader` interface. The GitHub side (reading via the Contents API,
PRs via go-git + go-github) is isolated in `internal/github`. A GitLab adapter
or a purely local reader can be added without changing the resolver.

## Layout

```
cmd/server/         main.go — wiring & HTTP server
internal/config/    environment-based configuration
internal/catalog/   platform-agnostic resolver
  types.go          domain types (SourceRef, Entity, Node)
  reader.go         FileReader interface + LocalReader
  resolver.go       recursive Location resolution with cycle detection
  locationedit.go   YAML editing (remove target/document, preserves comments)
internal/github/    GitHub forge adapter
  client.go         App- or token-based authentication
  reader.go         FileReader implementation
  pr.go             PR engine (clone/commit/push via go-git)
internal/api/       HTTP handlers
```

## Concept

Backstage walks from a few entry points through `Location` entities into more
files — a directed graph. `machinery-catalog-locator` rebuilds that graph: each
resolved node knows the file it came from (`Source`), the referring location
(`Parent`), and the exact target string (`ViaTarget`).

Those three pieces are exactly what deletion needs: removing a resource and
its referring target in a **single PR** keeps Backstage from logging an error
for the orphaned target.

## Setup

Create a GitHub App (recommended) with repository permissions *Contents: Read &
write* and *Pull requests: Read & write*. Download the private key as PEM.

```bash
cp .env.example .env      # fill in the values
task tidy                 # resolve dependencies (network required)
task test
task run
```

For local development a `GITHUB_TOKEN` in `.env` is sufficient as a fallback.

## API

| Method | Path                       | Purpose |
|--------|----------------------------|---------|
| GET    | `/healthz`                 | health check |
| GET    | `/locations/tree?root=...` | full resolved tree |
| GET    | `/resources?root=...`      | flat list of all non-Location entities |
| POST   | `/locations/remove-target` | Operation 1: remove a target from a `location.yaml` (PR) |
| POST   | `/resources/delete`        | Operation 2: delete a resource from Git (PR) |

`root` is a GitHub blob URL, e.g.
`https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml`.

### Examples

Show the tree:

```bash
curl 'localhost:8080/locations/tree?root=https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml'
```

Remove a target from a location (Operation 1):

```bash
curl -X POST localhost:8080/locations/remove-target \
  -H 'content-type: application/json' \
  -d '{
        "location": "https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml",
        "target": "./components/claim-router/catalog-info.yaml"
      }'
```

Delete a resource (Operation 2 — removes the file **and** the parent target):

```bash
curl -X POST localhost:8080/resources/delete \
  -H 'content-type: application/json' \
  -d '{
        "root": "https://github.com/stuttgart-things/catalog/blob/main/all-locations.yaml",
        "kind": "Component",
        "name": "claim-router"
      }'
```

Either call returns `{"pullRequest": "https://github.com/.../pull/42"}`.

## Notes & limits

- If a file contains several `---`-separated entities, deletion extracts only
  the affected document instead of removing the whole file.
- Cross-repo: when a location points into another repo via an absolute blob
  URL, the parent target is **not** removed automatically (a separate PR is
  required).
- The `LocalReader` in `internal/catalog/reader.go` enables offline validation
  and is used by the tests.

## Roadmap

- GitLab adapter: implement `catalog.FileReader` plus a `PRService` equivalent
  — the resolver stays untouched.
- Optional mode that uses the Backstage Catalog API (`/api/catalog/entities`,
  annotation `backstage.io/managed-by-location`) instead of resolving on its
  own.
