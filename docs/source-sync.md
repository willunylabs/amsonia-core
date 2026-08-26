# Shared Source Synchronization

Amsonia Core receives shared production source only through reviewed, exact
export manifests. The Amsonia Source Distribution repositories remain the source mainline
for exported code. The public repository owns Core-only composition, contracts,
migrations, documentation, examples, and releases.

## Manifests

- Backend: `amsonia-server/amsonia-core.export.json`
- Frontend: `amsonia-web/amsonia-core.export.json`

Every path not listed in a manifest is denied. Export manifests must not use
wildcards, symlinks, absolute paths, traversal, credentials, customer
configuration, private dependencies, or commercial modules.

The initial sync uses dedicated, exclusively controlled worktrees for the source
and destination roots. This local tool handles in-process errors and rollback;
process-crash recovery and hostile concurrent writers are outside this phase's
trust model.

## Synchronize

From the Amsonia Core repository, set `ADMIN_ROOT` and `UI_ROOT` to clean,
dedicated source worktrees:

```bash
ADMIN_SHA="$(git -C "$ADMIN_ROOT" rev-parse HEAD)"
UI_SHA="$(git -C "$UI_ROOT" rev-parse HEAD)"

go run ./cmd/core-sync \
  --mode sync \
  --manifest "$ADMIN_ROOT/amsonia-core.export.json" \
  --source-root "$ADMIN_ROOT" \
  --destination-root . \
  --source-commit "$ADMIN_SHA" \
  --provenance provenance/amsonia-server.json

go run ./cmd/core-sync \
  --mode sync \
  --manifest "$UI_ROOT/amsonia-core.export.json" \
  --source-root "$UI_ROOT" \
  --destination-root . \
  --source-commit "$UI_SHA" \
  --provenance provenance/amsonia-web.json
```

Review every generated diff. Synchronization never commits, pushes, merges, or
publishes automatically. A synchronization pull request must show the source
commits, provenance changes, copied files, license boundary, and all validation
results.

## Check And Verify

Use `check` before opening a synchronization pull request to compare the current
source worktrees with committed Core files and provenance:

```bash
go run ./cmd/core-sync \
  --mode check \
  --manifest "$ADMIN_ROOT/amsonia-core.export.json" \
  --source-root "$ADMIN_ROOT" \
  --destination-root . \
  --source-commit "$ADMIN_SHA" \
  --provenance provenance/amsonia-server.json

go run ./cmd/core-sync \
  --mode check \
  --manifest "$UI_ROOT/amsonia-core.export.json" \
  --source-root "$UI_ROOT" \
  --destination-root . \
  --source-commit "$UI_SHA" \
  --provenance provenance/amsonia-web.json
```

Public CI verifies committed destination files against their provenance without
access to the source repositories:

```bash
go run ./cmd/core-sync --mode verify --destination-root . --provenance provenance/amsonia-server.json
go run ./cmd/core-sync --mode verify --destination-root . --provenance provenance/amsonia-web.json
```

## Ownership And Contributions

Shared production code is maintained in the Amsonia Source Distribution repositories and
exported only through their exact allowlists. Core-only composition, OpenAPI
contracts, migrations, documentation, examples, and release metadata are owned
by the public repository.

External changes to exported shared files must be backported to the owning
product repository before the next synchronization, so a future export cannot
overwrite the accepted contribution. Public-only documentation, examples, and
deployment changes do not require product-repository backporting.
