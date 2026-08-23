# Dedicated Amsonia AWS origin

Amsonia runs on an EC2 origin that is separate from the existing Willuny
origin. Do not install Amsonia releases, databases, or runtime secrets on the
Willuny instance.

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

The instance role can read only:

- `s3://h1-recon-results-336090301244-us-east-1/deployments/amsonia/*`
- SSM parameters under `/amsonia/prod/*`
- the permissions required by `AmazonSSMManagedInstanceCore`

It does not reuse the Willuny runtime role or its application secrets.

## Hostname rollout

The production cutover completed on 2026-08-17. Both `amsonia.dev` and
`demo.amsonia.dev` are proxied by Cloudflare to the dedicated Elastic IP. The
active Traefik dynamic configuration is `traefik-dynamic.yml`:

- `amsonia.dev` serves the indexable product site on `127.0.0.1:8084`;
- `demo.amsonia.dev` serves the Console and API on ports 8083 and 8082;
- `www.amsonia.dev` redirects to the apex at the Cloudflare edge.

The previous phase-specific configurations remain as rollout history and
rollback inputs; they are not the active production configuration. Disable the
legacy Amsonia services on the Willuny origin only after both hostnames have
passed their rollback window.

Rollback is hostname-scoped: restore the previous Cloudflare A record for the
affected hostname and leave the other hostname unchanged.

## Release contract

Keep at least two releases and retain each published artifact for 14 days:

```text
/opt/amsonia-core-demo/releases/<release>
/opt/amsonia-core-demo-web/releases/<release>
/opt/amsonia-site/releases/<release>
```

The database listens only on `127.0.0.1:5433`; the API and static origins bind
only to `127.0.0.1:8082`, `127.0.0.1:8083`, and `127.0.0.1:8084`.

## Static-site deployment

`amsonia.dev` is released independently from the Demo API, Console, database,
and Traefik configuration. A successful `CI` run for a push to `main`
triggers `.github/workflows/deploy-site.yml`, which:

1. checks out the exact CI commit;
2. rebuilds and validates the Astro site;
3. builds the ARM64 `amsonia-static` server;
4. uploads an immutable, checksummed artifact to
   `s3://h1-recon-results-336090301244-us-east-1/deployments/amsonia/site/`;
5. invokes the constrained `AmsoniaDeploySite` SSM document; and
6. verifies both the public site and that the Demo health endpoints remain
   available.

The host-side `/usr/local/sbin/deploy-amsonia-site` script is root-owned. It
accepts artifacts only from the approved S3 prefix, rejects unsafe archive
paths, atomically changes `/opt/amsonia-site/current`, restarts only
`amsonia-site.service`, and restores the previous symlink on failed health
checks. It never changes the Demo API, Console, PostgreSQL, or Traefik.

GitHub uses OIDC rather than a stored AWS access key. The
`amsonia-github-deploy-role` trust is restricted to:

`repo:willunylabs/amsonia-core:environment:amsonia-production`

Its policy can upload only the site artifact prefix and invoke only the
`AmsoniaDeploySite` document against the dedicated Amsonia instance.
