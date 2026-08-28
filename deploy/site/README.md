# Amsonia site origin

The dedicated `amsonia-prod` EC2 instance serves only the indexable static
product and technical site at `amsonia.dev`. The hosted Amsonia Core Demo was
retired on 2026-08-28; Core evaluation now happens through source, tests,
documentation, and local execution.

`demo.amsonia.dev` is retained as a non-indexable commercial entry point and
temporarily redirects to `https://willuny.com/admin/login`. Its runtime belongs
to the commercial Amsonia repositories, not to Amsonia Core.

Tracked host assets:

- `amsonia-site.service`: static site origin on `127.0.0.1:8084`;
- `deploy-amsonia-site.sh`: constrained immutable site release installer;
- `traefik-static.yml`: static Traefik configuration;
- `traefik-dynamic.yml`: product-site route and retired-Demo redirect;
- `traefik.service`: reverse proxy service.

No Core API, Console, database, public test credentials, or hosted-Demo
deployment workflow may be provisioned from this repository.
