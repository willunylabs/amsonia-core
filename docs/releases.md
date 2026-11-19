# Core releases and compatibility

Use the tagged archive for a reproducible self-hosted installation. The
[archive installation guide](../deploy/preview/README.md) describes
the archive layout and backup procedure.

| Release | Status | Verified environment |
| --- | --- | --- |
| [Latest release](https://github.com/willunylabs/amsonia-core/releases/latest) | Recommended install bundle | Linux amd64, Docker Compose 2.24.4 or newer, PostgreSQL 16, Amsonia schema 12 |

Older versions are recorded in the [changelog](../CHANGELOG.md). Use the
current release for new installations and Go SDK dependencies.

The published archive contains the API, CLI, Console and PostgreSQL 16
images. Use its `VERSION` and `REVISION` files when reporting a problem. For
source builds and other environments, see [Getting started](getting-started.md).
CI verifies PostgreSQL 16 and the Linux amd64 archive.

## Verified upgrades

| Source | Target | Database change | Evidence |
| --- | --- | --- | --- |
| v0.2.0-preview.8 | v0.2.0 | Schema 12 to 12 | [Release build and upgrade](https://github.com/willunylabs/amsonia-core/actions/runs/44470207114) |
| v0.2.0-preview.7 | v0.2.0-preview.8 | Schema 12 to 12 | [Release bundle workflow](https://github.com/willunylabs/amsonia-core/actions/workflows/preview.yml) |
| v0.2.0-preview.6 | v0.2.0-preview.7 | Schema 11 to 12 | [Public archive upgrade](https://github.com/willunylabs/amsonia-core/actions/runs/42359656311) |
| v0.2.0-preview.3 | v0.2.0-preview.7 | Schema 10 to 12 | [Release candidate archive upgrade](https://github.com/willunylabs/amsonia-core/actions/runs/42359111768) |

These rehearsals restore a backup before applying the upgrade. For a different
source revision or PostgreSQL major version, verify the upgrade on your own
restored copy before upgrading the live database. Deploy the API, CLI and
Console from the same revision. Follow the [self-hosting runbook](self-hosting.md)
for backup and rollback.

Security fixes land on `main` and ship in subsequent releases.
See the [security policy](../SECURITY.md#supported-versions) for
maintenance scope and private reporting.
