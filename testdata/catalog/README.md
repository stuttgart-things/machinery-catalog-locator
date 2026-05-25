# testdata/catalog

Realistic Backstage catalog fixtures used by the resolver tests and
handy for ad-hoc development against `catalog.LocalReader`.

Two independent test surfaces live here:

## 1. The main tree — `all-locations.yaml`

A normal multi-target root that fans out to every "happy path" example:

```
all-locations.yaml
├── components/claim-router/catalog-info.yaml      Component (namespaced)
├── components/payment-api/catalog-info.yaml       Component + Resource (multi-doc)
└── systems/platform-location.yaml                 Location → nested
    └── services/api-gateway.yaml                  Component (reached via nesting)
```

Resolving `all-locations.yaml` exercises:
- multi-target `spec.targets`
- relative-path resolution from the parent file's directory
- multi-document YAML files (`---`-separated) — `payment-api`
- recursive descent through a child Location — `platform-location.yaml`
- namespaces (`claim-router` is in `insurance`; the others default)

Expected non-Location resources after resolve: **4** (claim-router,
payment-api, payment-api-db, api-gateway).

### Linked Crossplane manifests — `crossplane-claims/`

Every non-Location resource in the main tree carries a
`machinery.stuttgart-things.com/crossplane-claim` or `…/crossplane-xr`
annotation pointing at a Git blob URL. The URL's path component
matches a real fixture under `crossplane-claims/`, so the resolver,
`LocalReader`, and `catalog.CrossplaneRefs` round-trip end-to-end
without any GitHub call:

```
crossplane-claims/
├── insurance/claim-router.yaml       Claim — linked from components/claim-router
├── payments/payment-api-xr.yaml      XR    — linked from components/payment-api
├── payments/payment-api-db.yaml      Claim — linked from payment-api-db (multi-doc sibling)
└── platform/api-gateway-xr.yaml      XR    — linked from systems/services/api-gateway
```

`TestCrossplaneRefs_FetchesFixture` (in `internal/catalog`) asserts
that each catalog entity's annotation resolves to the manifest with
the expected `kind`. Renaming a file here without updating the
annotation will fail that test.

## 2. Edge cases — `edge-cases/`

Each file here is a deliberately isolated scenario; resolve it as a
root in its own right, not from `all-locations.yaml`.

| File                     | What it tests                                                   |
|--------------------------|-----------------------------------------------------------------|
| `singular-target.yaml`   | `spec.target` (singular) — Backstage accepts both forms         |
| `only-component.yaml`    | The target reached via `singular-target.yaml`                   |
| `cycle-a.yaml` ⇄ `cycle-b.yaml` | Location cycle — resolver must terminate, not loop       |
| `broken-targets.yaml`    | One valid + one missing target — surfaced in `Node.Broken`      |

## How tests use this

Tests construct a `catalog.LocalReader{Root: "../../testdata/catalog"}`
(path is relative to the test's package directory) and pass it to
`catalog.NewResolver`. Owner/Repo/Ref on `SourceRef` are ignored by
`LocalReader` — only `Path` matters.

Files here are load-bearing for:

- `internal/catalog/locationedit_test.go` — the main tree + edge-case fixtures
- `internal/catalog/crossplane_test.go` — `crossplane-claims/**` + annotation URLs on each catalog entity
- `internal/grpcserver/server_test.go` — `GetEntityManifest` and `ListEntitiesByCrossplaneSource` walk the same paths

Renaming or moving a fixture without updating the annotation URLs that
reference it will break these tests.

## Editing safely

If you change a fixture, the resolver tests will tell you. Specifically:

- Removing or renaming `claim-router/catalog-info.yaml` will change the
  resource count assertion in `TestResolveTree`.
- Editing `payment-api/catalog-info.yaml` should preserve the
  `---` document separator and the two distinct entities.
- `cycle-a.yaml` / `cycle-b.yaml` must remain a true cycle (each
  pointing only at the other) — adding a third target breaks the
  termination assertion in `TestCycleDetection`.
