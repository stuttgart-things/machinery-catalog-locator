# GitHub App setup (PR-preview platform)

machinery-catalog-locator reads Git and opens PRs, so it authenticates as a
**GitHub App** (a PAT works as a dev fallback). This doc covers creating the
App, the exact permissions it needs, and wiring its private key into the
PR-preview platform via Vault + the External Secrets Operator.

The permissions below are derived from what the code actually does:

| Operation | Code | GitHub App permission |
|---|---|---|
| Read catalog files | `internal/github/reader.go` (`Repositories.GetContents`) | **Contents: Read** |
| Clone + push a head branch | `internal/github/pr.go` (`git push refs/heads/...`) | **Contents: Read and write** |
| Open the PR | `internal/github/pr.go` (`PullRequests.Create`) | **Pull requests: Read and write** |

No organization permissions, no webhooks, no account permissions.

## Org or personal? → Organization (`stuttgart-things`)

This is shared platform infra that authors PRs as stuttgart-things automation,
so the App is owned by the **org**, not a personal account (manageable by any
org owner, not tied to one person, installs naturally on the org's catalog
repos). You need **org-owner** rights to create it.

## 1. Create the App

`https://github.com/organizations/stuttgart-things/settings/apps/new`

| Field | Value |
|---|---|
| GitHub App name | `machinery-catalog-locator` (globally unique; if taken, `stuttgart-things-catalog-locator`) |
| Homepage URL | `https://github.com/stuttgart-things/machinery-catalog-locator` |
| Webhook → **Active** | **Uncheck** (no webhook receiver — removes the required webhook-URL field) |

**Repository permissions:**
- **Contents** → Read and write
- **Pull requests** → Read and write
- **Metadata** → Read-only (auto-selected, mandatory)

Leave Organization/Account permissions at *No access*. No webhook events.

**Where can this App be installed?** → *Only on this account* (keep it private).

Click **Create GitHub App**.

## 2. Capture the identifiers + key

On the App's page:
- **App ID** — shown near the top. → `github.appID` in the AppSet.
- **Generate a private key** → downloads a `.pem`. **This file goes into Vault** (§4).

## 3. Install on the org

App page → **Install App** → **stuttgart-things** → *Only select repositories* →
pick the catalog repo(s) catalog-locator resolves against → **Install**.

After installing, the URL is `…/settings/installations/<NUMBER>`; that number is
the **Installation ID** → `github.installationID` in the AppSet.

> The current installation is **`137151756`**
> (https://github.com/organizations/stuttgart-things/settings/installations/137151756).

> ⚠️ The App must be installed on **every repo catalog-locator reads or opens
> PRs against**. If a preview resolves a catalog in a repo where the App isn't
> installed, the installation token is unauthorized there (404/403).

## 4. Store the private key in Vault

The PR-preview platform pulls the PEM from Vault via ESO (no key material in
git). Add a dedicated KV mount + read policy in
`stuttgart-things/stuttgart-things` →
`clusters/labul/vsphere/infra-sthings/vault-homerun2-secrets/terraform.tfvars.sops.json`
(same shape as the existing `homerun2-pr` / `zitadel` entries):

```jsonc
// secret_engines[] — creates KV mount `machinery-catalog-locator/` and seeds
// the secret at machinery-catalog-locator/data/machinery-catalog-locator-github-app
{
  "path": "machinery-catalog-locator",
  "name": "machinery-catalog-locator-github-app",
  "description": "GitHub App private key for the machinery-catalog-locator PR-preview platform — ESO syncs private-key.pem into each preview namespace",
  "data_json": "{\"private-key.pem\":\"-----BEGIN RSA PRIVATE KEY-----\\n<PEM body, newlines escaped as \\n>\\n-----END RSA PRIVATE KEY-----\\n\"}"
}

// kv_policies[] — read access for the cluster's ESO role
{
  "name": "read-machinery-catalog-locator",
  "capabilities": "path \"machinery-catalog-locator/data/machinery-catalog-locator-github-app\" {\n  capabilities = [\"read\"]\n}\n"
}
```

Then bind the new policy to the cluster's ESO role in
`argocd/clusters/homerun2-dev/vault-k8s-auth/variables.tf` — add
`read-machinery-catalog-locator` to the `eso` role's `token_policies`:

```hcl
token_policies = ["read-homerun2-pr", "read-zitadel-zitadel", "read-machinery-catalog-locator"]
```

Apply order: `vault-homerun2-secrets` (creates mount + policy) → `vault-k8s-auth`
(binds the policy to the role). To seed/rotate the PEM without re-applying:

```bash
vault kv put machinery-catalog-locator/machinery-catalog-locator-github-app \
  private-key.pem=@machinery-catalog-locator.<id>.private-key.pem
```

## 5. Wire the identifiers into the AppSet

In `stuttgart-things/argocd` →
`platforms/machinery-catalog-locator-pr-preview/appset-machinery-catalog-locator-pr-preview.yaml`,
replace the placeholders:

```yaml
github:
  appID: "<APP_ID from §2>"
  installationID: "137151756"
  privateKeySecret:
    name: machinery-catalog-locator-github-app
    key: private-key.pem
```

The `vault-machinery-catalog-locator` `ClusterSecretStore` and the ESO
`ExternalSecret` that materialize the Secret per preview namespace are already
defined (see the platform README). Once §4–5 land, a `preview`-labelled PR
spins up a working env.

## Local / non-preview use

For local runs, skip the App entirely — either `-local-root testdata/catalog`
(read-only, no creds) or a PAT via `GITHUB_TOKEN` (see
`internal/config/config.go`).
