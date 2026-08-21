# Amsonia Brand and Product Boundaries

Status: Accepted

Decision date: 2026-08-21

This document prevents public copy, source documentation, and repository metadata
from confusing the company, product, open-source component, and commercial offer.

## Hierarchy

- **Willuny Labs** is the company, publisher, seller, and support organization.
- **Amsonia** is a Willuny software product.
- **Amsonia Core** is the Apache-2.0 open-source foundation inside Amsonia.
- **Amsonia Source Distribution** is the commercial source-code distribution of
  Amsonia. It is an offer/SKU, not a second company or umbrella product brand.
- **GitHub** is the code, release, issue, and contribution surface. It is not the
  canonical product website.

## Canonical homes

| Surface | Canonical URL | Purpose |
| --- | --- | --- |
| Company | `https://willuny.com` | Company, portfolio, publisher trust, legal, and transactions |
| Product | `https://amsonia.dev` | Product positioning, technical evaluation, docs, and releases |
| Core | `https://amsonia.dev/core` | Open-source component overview and documentation |
| Repository | `https://github.com/willunylabs/amsonia-core` | Source, releases, issues, and contribution |
| Demo | `https://demo.amsonia.dev` | Read-only evaluation; always excluded from indexing |
| Commercial offer | `https://willuny.com/amsonia` | Scope, licensing, purchase, delivery, and support |

## Publishing rules

1. Lead with Amsonia when describing the product.
2. Lead with Amsonia Core when describing only this repository.
3. Use Amsonia Source Distribution only for the commercial source package.
4. Never imply that commercial-only modules are part of the Apache-2.0 Core.
5. Never publish an unreleased component as an available product.
6. Product explanations live on Amsonia; company and transaction explanations
   live on Willuny; code and contribution instructions live on GitHub.
7. Do not duplicate the same body copy across the product and company domains.

Future Gateway or Content work remains an Amsonia module when it shares the same
buyer, runtime, value proposition, release train, and distribution. A capability
becomes a separate Willuny product only after a recorded architecture decision
establishes standalone value, audience, licensing, deployment, roadmap, and
support.
