# demo — end-to-end AnsibleRun ↔ catalog ↔ Argo CD

A self-contained, real-life example that exercises the locator from both
sides at once:

- **Git declaration** — a Backstage `Location` + `Component` in
  [`catalog/`](catalog/)
- **Live resource** — a *second* Crossplane `AnsibleRun`
  (`ansible-run-demo`) in
  [`crossplane-claims/ansible/`](crossplane-claims/ansible/)
- **Delivery** — an Argo CD `Application` in [`argocd/`](argocd/) that
  syncs the AnsibleRun into the cluster

The Component and the AnsibleRun point at each other, so the locator can
walk in either direction:

```
                 crossplane-xr annotation  (forward)
 Component/ansible-baseos-demo ───────────────────────────►  AnsibleRun/ansible-run-demo
 (demo/catalog/components/…)   ◄───────────────────────────  (demo/crossplane-claims/ansible/…)
                 catalog-component label    (reverse)
```

This mirrors the patterns in `testdata/catalog`, but with a manifest
that actually runs in your cluster (`resources.stuttgart-things.com/v1alpha1`
`AnsibleRun`) instead of a synthetic Claim/XR.

## Files

```
demo/
├── catalog/
│   ├── demo-location.yaml                  Location → component  (locator root)
│   └── components/
│       └── ansible-baseos-demo.yaml        Component, crossplane-xr → the AnsibleRun
├── crossplane-claims/
│   └── ansible/
│       └── ansible-run-demo.yaml           2nd AnsibleRun + catalog-component label
└── argocd/
    └── ansible-run-demo-app.yaml           Argo CD Application syncing the AnsibleRun
```

## The links, concretely

`catalog/components/ansible-baseos-demo.yaml` (forward → manifest):

```yaml
annotations:
  machinery.stuttgart-things.com/crossplane-kind: AnsibleRun
  machinery.stuttgart-things.com/crossplane-api-version: resources.stuttgart-things.com/v1alpha1
  machinery.stuttgart-things.com/crossplane-xr: https://github.com/stuttgart-things/machinery-catalog-locator/blob/main/demo/crossplane-claims/ansible/ansible-run-demo.yaml
```

`crossplane-claims/ansible/ansible-run-demo.yaml` (reverse → catalog):

```yaml
metadata:
  labels:
    machinery.stuttgart-things.com/catalog-component: ansible-baseos-demo
```

> The blob URL uses `main`, so once this branch is merged the URL resolves
> against real GitHub. Until then, resolve it locally (below) — the server
> strips the `demo/` prefix so the same URL works offline.

---

## 1. Deploy the live resource with Argo CD

```bash
kubectl apply -f demo/argocd/ansible-run-demo-app.yaml

# Watch Argo CD pull the manifest and Crossplane reconcile it
argocd app get ansible-run-demo
kubectl get ansiblerun ansible-run-demo -o yaml | yq '.status.conditions'
```

You should end up with a second AnsibleRun alongside `ansible-run-test`:

```bash
$ kubectl get ansiblerun
NAME                SYNCED   READY   ...
ansible-run-test    True     True
ansible-run-demo    True     True
```

> Don't have Argo CD handy? Apply the manifest directly — the catalog
> links are identical either way:
> ```bash
> kubectl apply -f demo/crossplane-claims/ansible/ansible-run-demo.yaml
> ```

> The demo targets the same inventory host (`10.31.104.101`) and uses a
> distinct `crossplaneObjectName`/`pipelineRunName` (`run-ansible-demo`),
> so it won't collide with `ansible-run-test`. Edit
> `spec.ansibleVarsInventory` to point at your own host.

## 2. Resolve the catalog and follow the link (offline, no GitHub)

Boot the locator against this `demo/` directory. `-local-root demo` makes
the server strip the `demo/` path prefix, so the real blob URLs above
round-trip to the local files:

```bash
go run ./cmd/server -local-root demo
# gRPC on :50051, HTMX on :8080
```

**In the browser:** open <http://localhost:8080>, paste the root URL

```
https://github.com/stuttgart-things/machinery-catalog-locator/blob/main/demo/catalog/demo-location.yaml
```

→ the tree shows `Component/ansible-baseos-demo`; click **View claim** to
swap in the live AnsibleRun YAML right under the node.

**With grpcurl** — forward (entity → manifest):

```bash
grpcurl -plaintext -d '{
  "root_url":"https://github.com/stuttgart-things/machinery-catalog-locator/blob/main/demo/catalog/demo-location.yaml",
  "kind":"Component","name":"ansible-baseos-demo","namespace":"infra"
}' localhost:50051 catalogservice.CatalogService/GetEntityManifest
```

…and reverse (manifest → which catalog entity references it):

```bash
grpcurl -plaintext -d '{
  "root_url":"https://github.com/stuttgart-things/machinery-catalog-locator/blob/main/demo/catalog/demo-location.yaml",
  "manifest_url":"https://github.com/stuttgart-things/machinery-catalog-locator/blob/main/demo/crossplane-claims/ansible/ansible-run-demo.yaml"
}' localhost:50051 catalogservice.CatalogService/ListEntitiesByCrossplaneSource
```

## 3. Cross-check against the cluster

Tie the three views together — Git (catalog), Git (manifest), and live:

```bash
# the catalog says this Component is backed by AnsibleRun/ansible-run-demo …
# … the manifest carries the reverse label …
kubectl get ansiblerun ansible-run-demo \
  -o jsonpath='{.metadata.labels.machinery\.stuttgart-things\.com/catalog-component}{"\n"}'
# -> ansible-baseos-demo

# … and the run actually executed:
kubectl get ansiblerun ansible-run-demo \
  -o jsonpath='{.status.message}{"\n"}'
```

## 4. Tear down via PR (the locator's other half)

Against a real GitHub-backed server (not `-local-root`), the locator can
remove this resource and the referring `Location` target in a single PR —
`DeleteResource` on `Component/ansible-baseos-demo`. Argo CD then prunes
`AnsibleRun/ansible-run-demo` from the cluster on the next sync, and
Crossplane cleans up the Tekton PipelineRun. That's the full GitOps
round-trip: declared in Git → live in cluster → removed via PR → gone.
