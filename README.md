<img width="1024" height="357" alt="kubby-logo-horizontal-no-tagline-dark-1024w" src="https://github.com/user-attachments/assets/5f421ebe-a9f4-4b53-a730-42b331725e28" />


A browser-based, multi-cluster **Kubernetes management and observation UI** — a Lens or
Rancher equivalent that runs on your own infrastructure.

Kubby replaces a desktop Lens install, a folder of scattered kubeconfigs and a terminal
that is always open, with one address your team opens in a browser. It is built for a
small platform team operating several clusters (prod / preprod / test), on an internal
network.

[![CI](https://github.com/erolbeyaz/kubby/actions/workflows/ci.yml/badge.svg)](https://github.com/erolbeyaz/kubby/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

> **Status: 0.11.0, packaged and published.** It runs, it is tested, and it installs from
> a published image with Helm or Compose. It has not yet been run against an
> RKE2/Rancher-class cluster in anger — read the known limits below before you point it
> at a production one.

<!-- Screenshots: drop PNGs into .github/images/ and uncomment.
![Health panel](.github/images/health.png)
![Workloads](.github/images/workloads.png)
![Cluster terminal](.github/images/terminal.png)
-->

---

## What it does

**Finding what is broken is the point.** The health panel is the front page: everything
wrong in a cluster on one screen, with the reason, before anyone opens a pod.

- **Health panel** — CrashLoopBackOff, ImagePullBackOff, OOMKilled, Pending pods, NotReady
  nodes, failed Jobs, unbound PVCs, warning events, expiring certificates. Restart reasons
  (`exitCode`, `OOMKilled`, `Error`) appear as badges in the list, not three clicks away
- **Full resource browsing** — workloads, config, network (Gateway API included), storage,
  access control and CRDs, through one generic viewer with server-side projection
- **Writes** — YAML editing with server-side dry-run and diff, scale, rollout restart and
  rollback, delete, evict, cordon/drain, CronJob trigger and suspend. Every write path is
  aware of ArgoCD ownership and says so before it fights your GitOps controller
- **Live updates** — resource lists stream over SSE; a two-way ownership graph walks from
  a Deployment down to its pods and back up from a pod to what created it
- **Terminals** — pod exec, an ephemeral debug container, an optional node shell, and a
  **cluster terminal** locked to `kubectl` and `helm` that you can drag files onto
- **Port-forward** from the browser, with the forwarded page isolated in an opaque origin
- **Logs** with search and container selection, aware of sidecars
- **Prometheus** — per-cluster connection and a cluster dashboard
- **Global search** — Ctrl+K across the whole fleet; an unreachable cluster is reported
  **by name** rather than silently returning nothing
- **Helm** release view, values and revision history
- **Audit** — who, when, which cluster, what. It cannot be turned off

## Security in one paragraph

Kubby holds credentials with **full access to every cluster you register**, so the design
starts there. Kubeconfigs are sealed with envelope encryption (AES-256-GCM, per-row AAD)
and never travel back to the browser. Authentication is argon2id plus TOTP with
progressive lockout. Every write passes four server-side gates — a deployment-wide kill
switch, role, per-cluster grant, per-cluster read-only lock — and then the cluster's own
`SelfSubjectAccessReview`. The image is distroless, non-root, shell-less and runs on a
read-only root filesystem. CI fails the build on `trivy`, `semgrep`, `gitleaks`,
`npm audit` or `govulncheck` findings.

What it does **not** protect against is written down just as plainly, under
[Known limits](#known-limits).

---

## Installing

There is **no published image**. Kubby is a tool that holds cluster credentials, so you
build it and push it to a registry you control. Both install paths below start there.

### Build and push the image

```bash
git clone https://github.com/erolbeyaz/kubby.git
cd kubby

make release VERSION=0.11.0 IMAGE_REGISTRY=registry.example.com/platform
```

`make release` verifies the image before it pushes: the expected `kubectl` and `helm`
versions, uid 65532, and no shell. It deliberately never produces a `latest` tag — a
`latest` cannot tell you what is running and makes rolling back impossible.

### Option A — Docker Compose

Two containers, Kubby and its database. Nothing else: Elasticsearch, Loki and Prometheus
are systems Kubby is *pointed at*, not things it ships.

```bash
cd deploy/compose
cp .env.example .env
```

Fill in the three values marked `REQUIRED`:

```bash
KUBBY_IMAGE=registry.example.com/platform/kubby:0.11.0
KUBBY_DB_PASSWORD=$(openssl rand -base64 24)
KUBBY_ENCRYPTION_KEY=$(openssl rand -base64 32)
```

```bash
docker compose up -d
docker compose logs -f kubby
```

Open <http://localhost:8080>.

> **Keep a copy of `KUBBY_ENCRYPTION_KEY` outside Kubby.** Every stored kubeconfig is
> sealed with it. Lose it and none of them can be opened again.

### Option B — Helm

The chart does **not** ship a database. Point it at a PostgreSQL 18 you already run.

Create the secret yourself rather than letting the chart generate one — a chart-generated
key lives in Helm's release history and `helm upgrade` will regenerate it, and a new key
cannot open what the old one sealed:

```bash
kubectl create namespace kubby

kubectl -n kubby create secret generic kubby \
  --from-literal=encryption-key="$(openssl rand -base64 32)" \
  --from-literal=db-password='<your database password>'
```

```bash
helm install kubby ./deploy/helm/kubby \
  --namespace kubby \
  --set image.registry=registry.example.com/platform \
  --set image.tag=0.11.0 \
  --set secrets.existingSecret=kubby \
  --set database.host=postgres.databases.svc.cluster.local \
  --set config.publicUrl=https://kubby.example.com \
  --set ingress.enabled=true \
  --set ingress.host=kubby.example.com
```

The chart renders a least-privilege ClusterRole with no wildcards, a NetworkPolicy, a PDB
and a locked-down `securityContext`. `values.yaml` documents every key.

To let Kubby manage the cluster it runs in, using its own ServiceAccount:

```bash
--set inCluster.enabled=true --set inCluster.access=read
```

### First run
<img width="2559" height="1267" alt="image" src="https://github.com/user-attachments/assets/03082139-d9b1-40b9-916d-ea72cd1dd76b" />

Kubby **creates its own schema at startup** — there is no separate migration step, and
upgrading is a tag change. The first browser visit asks you to create the first
administrator; no account is seeded and no default password exists anywhere.

Then: **Clusters → Add**, paste a kubeconfig. It must carry an embedded bearer token or a
client certificate — **exec plugins are not supported** and never will be, because
supporting them would mean running an arbitrary binary inside Kubby (ADR-017).

Check it is healthy:

```bash
curl -fsS http://localhost:8080/healthz     # the process is up
curl -fsS http://localhost:8080/readyz      # database reachable AND schema current
```

---

## Configuration

Everything is environment variables. `deploy/compose/.env.published.example` and
`.env.example` document all of them, each with what it is for.

The ones that matter most:

| Variable | Default | What it does |
|---|---|---|
| `KUBBY_ENCRYPTION_KEY` | — | **Required.** base64, 32 bytes. Seals every stored credential |
| `KUBBY_DB_PASSWORD` | — | **Required.** |
| `KUBBY_PUBLIC_URL` | `http://localhost:8080` | The address the browser uses. Cookies, CSP and the WebSocket origin check depend on it |
| `KUBBY_READ_ONLY` | `false` | Deployment-wide write lock. Stops everyone, administrators included |
| `KUBBY_REQUIRE_2FA_FOR_ADMIN` | `true` | Force TOTP for administrators |
| `KUBBY_TLS_CERT_FILE` / `_KEY_FILE` | — | Set both to terminate TLS in Kubby. Empty means plain HTTP behind a proxy |
| `KUBBY_EXTRA_CA_BUNDLE` | — | Internal CA. **Added** to the system pool, never replacing it |
| `KUBBY_TRUSTED_PROXIES` | — | CIDRs whose `X-Forwarded-For` is believed |
| `KUBBY_METRICS_TOKEN` | — | Lets a scraper read `/metrics`. Empty means an admin session is required — never unauthenticated |
| `KUBBY_ALLOW_LOOPBACK_CLUSTERS` | `false` | Needed for a local kind/k3d/minikube cluster |

### Backups

The encryption key is not enough on its own — you also need the rows it sealed:

```bash
export KUBBY_BACKUP_PASSPHRASE='...'            # the only thing protecting the archive

make config-export OUT=kubby-2026-08-25.bak
make config-restore IN=kubby-2026-08-25.bak DRY_RUN=1
```

The archive carries every cluster kubeconfig and is protected by that one passphrase
(argon2id, deliberately heavier than the login hash). Store it away from Kubby. A restore
is additive and never overwrites what is already there.

---

## Developing

Contributions and forks are welcome. All code, comments, identifiers, commit messages,
API fields and UI text are **English**.

### Prerequisites

Go 1.27, Node.js 24 LTS, Docker, and `make`. Versions are pinned on purpose; please do
not bump a dependency in a PR without raising it first.

```bash
git clone https://github.com/erolbeyaz/kubby.git
cd kubby

cp .env.example .env
make setup        # go mod download, npm ci, and the pinned air/goose tools
make gen-key      # prints a KUBBY_ENCRYPTION_KEY — paste it into .env
                  # also set KUBBY_DB_PASSWORD in .env

make dev          # Postgres in Docker + API with hot reload + Vite dev server
```

The UI comes up on <http://localhost:5173> and talks to the API on `:8080`.
`make check-tools` tells you what is missing if `make dev` refuses to start.

### Everyday commands

```bash
make test         # go test ./... (under TZ=UTC) + vitest
make lint         # golangci-lint + eslint + tsc --noEmit
make security     # trivy + semgrep + gitleaks + npm audit
make build        # frontend bundle embedded into a single Go binary
make dev-stop     # stop a stack started in another terminal
make help         # everything else
```

Install the pre-commit hook once — it runs `gitleaks` before every commit, which matters
in a repository whose whole subject is cluster credentials:

```bash
git config core.hooksPath .githooks
```

### Layout

```
server/                     Go backend
  cmd/kubby/                main, bootstrap, graceful shutdown
  cmd/kubby-backup/         encrypted config export and restore
  internal/
    config/                 env -> typed config, validation
    store/                  Postgres access (pgx), repositories
    auth/                   argon2id, sessions, TOTP, RBAC decisions
    audit/                  audit events and shipping to a SIEM
    cluster/                encrypted kubeconfig storage, client factory, informers
    k8s/                    resource reads and writes, describe, logs, exec, port-forward
    kubectlsh/              the cluster terminal: argv splitting, the write gate
    health/                 the health panel collector
    promql/                 Prometheus reads
    backup/                 the sealed archive format
    httpapi/                chi router, handlers, middleware
  migrations/               goose SQL, embedded and applied at startup

web/src/                    React frontend
  app/                      router, layout, providers, theme
  features/<domain>/        component + hook + types together, feature-first
  components/               shared primitives
  lib/                      API client, SSE/WS clients, hooks

deploy/helm/kubby/          Helm chart
deploy/compose/             production Compose stack
deploy/dev/                 local test clusters and their Prometheus values
```


### House rules for a pull request

- `make test lint` is green
- A destructive operation (delete, drain, scale) arrives with a test
- No dependency version bumps without discussion (ADR-025)
- Nothing raw from the Kubernetes API reaches the frontend; project it server-side
- No secret, token, kubeconfig, password or `Authorization` header is ever logged
- Authorization is enforced on the server, on every request — hiding a control in the UI
  is decoration, not authorization


## Reporting a vulnerability

Please **do not open a public issue** — a flaw in Kubby is a flaw in every cluster
connected to it. Report it privately through
[GitHub Security Advisories](https://github.com/erolbeyaz/kubby/security/advisories/new).

## License

[Apache License 2.0](LICENSE). Copyright 2026 Erol Beyaz.
