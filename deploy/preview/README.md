# Install Amsonia Core preview

This bundle is for a Linux amd64 Docker engine with Docker Compose 2.24.4
or newer. The installation also uses Bash, OpenSSL and standard Unix tools.
No Go or Node.js toolchain is needed. Other architectures are not validated.

Download the archive and its matching `SHA256SUMS` from the same GitHub
Release. On Linux, verify and unpack it:

```sh
sha256sum --check SHA256SUMS
tar -xzf amsonia-core-*-linux-amd64.tar.gz
cd amsonia-core-*-linux-amd64
docker load --input images.tar
./init-env
./amsonia-compose up -d --wait --wait-timeout 180
./amsonia-compose exec api /usr/local/bin/amsonia bootstrap-admin
```

Open <http://127.0.0.1:8080> and sign in. The console binds to loopback and
PostgreSQL is not published on the host. For network access, put a trusted
HTTPS reverse proxy in front of the console. The `VERSION` and `REVISION`
files identify the application release and source commit.

Keep the generated `.env` private. It contains the original database
passwords and binding secrets; rerunning `init-env` refuses to replace it.
`./amsonia-compose down` stops the services and preserves the named volume.

## Upgrade an existing preview installation

Take and verify a backup as described in [the operator runbook](docs/self-hosting.md).
Stop the old stack with `./amsonia-compose down` (without `-v`), unpack the
new bundle separately, and copy the original `.env` with mode 0600. Keep the
same Compose project name so the new stack reuses the named database volume.
Load the new images and run the new bundle's `up` command above. Migration
and role provisioning run before the API starts. Do not bootstrap again.
This procedure assumes the new bundle uses a compatible PostgreSQL major
version; a major upgrade requires a separate database migration.

To recover an existing administrator without exposing database-owner
credentials to the API container:

```sh
./amsonia-compose run --rm --entrypoint /usr/local/bin/amsonia migrate recover-admin
```
