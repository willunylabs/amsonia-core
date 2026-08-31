# Dedicated Amsonia AWS origin

Amsonia runs on an EC2 origin that is separate from the existing Willuny
origin. The instance serves only the static Amsonia product and technical site;
it does not host a Core API, Console, database, or public test credentials.

## Production inventory

| Resource | Value |
| --- | --- |
| Region | `us-east-1` |
| EC2 name | `amsonia-prod` |
| Instance ID | `i-00e3d5d4c5c6b0dac` |
| Elastic IP | `3.219.243.118` |
| Security group | `sg-0c41faffb38af169e` (`amsonia-prod-sg`) |
| Instance profile | `amsonia-prod-ssm-profile` |
| IAM role | `amsonia-prod-ssm-role` |

The security group has no public SSH rule. Ports 80 and 443 accept traffic
only from the published Cloudflare IPv4 and IPv6 proxy ranges. Operations use
AWS Systems Manager Session Manager and Run Command.

The instance role can read:

- `s3://h1-recon-results-336090301244-us-east-1/deployments/amsonia/site/*`
- the permissions required by `AmazonSSMManagedInstanceCore`

It does not reuse the Willuny runtime role or its application secrets.

## Hostname rollout

The production cutover completed on 2026-08-17. Both `amsonia.dev` and
`demo.amsonia.dev` are proxied by Cloudflare to the dedicated Elastic IP. Since
2026-08-28, the active `deploy/site/traefik-dynamic.yml` contract is:

- `amsonia.dev` serves the indexable product site on `127.0.0.1:8084`;
- `demo.amsonia.dev` returns a temporary, non-indexable redirect to the
  commercial login at `https://willuny.com/admin/login`;
- `www.amsonia.dev` redirects to the apex at the Cloudflare edge.

The historical hosted Core Demo is retired and must not be re-created from this
repository. The commercial demo runtime is owned by the commercial Amsonia
repositories.

## Release contract

Keep at least two releases and retain each published artifact for 14 days:

```text
/opt/amsonia-site/releases/<release>
```

The static origin binds only to `127.0.0.1:8084`.

## Static-site deployment and migration

`amsonia.dev` is released independently from Traefik configuration. A
successful `CI` run for a push to `main`
triggers `.github/workflows/deploy-site.yml`, which:

1. checks out the exact CI commit;
2. rebuilds and validates the Astro site;
3. builds the ARM64 `amsonia-static` server;
4. uploads an immutable, checksummed artifact to
   `s3://h1-recon-results-336090301244-us-east-1/deployments/amsonia/site/`;
5. invokes the constrained `AmsoniaDeploySite` SSM document; and
6. verifies the public product site and real 404 behavior.

The host-side `/usr/local/sbin/deploy-amsonia-site` script is root-owned. It
accepts artifacts only from the approved S3 prefix, rejects unsafe archive
paths, atomically changes `/opt/amsonia-site/current`, restarts only
`amsonia-site.service`, and restores the previous symlink on failed health
checks. It never changes Traefik or any commercial runtime.

GitHub uses OIDC rather than a stored AWS access key. The
`amsonia-github-deploy-role` trust is restricted to:

`repo:willunylabs@252529865/amsonia-core@1322524885:environment:amsonia-production`

The organization and repository IDs are part of the OIDC subject emitted by
GitHub for this repository. Keeping them in the exact-match condition binds the
role to the immutable Willuny Labs organization and Amsonia Core repository,
as well as to the protected `amsonia-production` environment.

The GitHub deployment role can publish only the `site` artifact prefix and can
invoke only `AmsoniaDeploySite` against the dedicated Amsonia instance.

This AWS origin remains the production and rollback path while the static build
is validated on Cloudflare Pages. See `deploy/cloudflare/README.md`. Do not stop
or terminate `amsonia-prod` until the Pages custom-domain cutover has completed
its observation window and the retirement is approved separately.
