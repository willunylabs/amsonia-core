# Open-source scaffold baseline

Reviewed against upstream documentation on 2026-11-03. This compares the
provided defaults and documented workflows, not every extension or commercial
edition. No competitor code was copied or run.

| Reference | What it does well | What Core should match |
| --- | --- | --- |
| [Full Stack FastAPI Template](https://github.com/fastapi/full-stack-fastapi-template) (Python/React) | Runnable full-stack example, generated API client, browser tests and [self-hosting instructions](https://github.com/fastapi/full-stack-fastapi-template/blob/master/deployment-docker-compose.md) | A pinned installation, tested upgrade path, and an example that takes a developer from storage to an authorized route. Retain the existing typed client and OpenAPI contract. |
| [Django admin](https://docs.djangoproject.com/en/5.2/ref/contrib/admin/) (Python) | Quick internal model administration, with an [interactive password command](https://docs.djangoproject.com/en/5.2/ref/django-admin/#changepassword) | Make operator recovery a standard command. Keep purpose-built screens for invitations, memberships, roles and audit. |
| [Laravel Starter Kits](https://laravel.com/framework/docs/starter-kits) (PHP) and [Rails authentication generator](https://guides.rubyonrails.org/security.html#authentication) (Ruby) | Ready-made sign-in, account flows and email recovery | Match the closed-loop operator workflow. Email self-service remains an explicit gap for public-signup products; it is outside the default invitation-only console. |
| [go-admin](https://github.com/go-admin-team/go-admin) (Go/Vue) | Broader admin toolbox, sample module and CRUD generation | Match discoverable lists and focused forms, and make the existing example easy to copy. Menus, dictionaries, jobs and builders are outside Core's scope. |
| [ABP multi-tenancy](https://abp.io/docs/9.0/framework/architecture/multi-tenancy) (.NET) | Tenant-aware entities, an application data filter and a separate-database option | Keep deployment modes explicit. Document what the PostgreSQL policy covers and the limits of the shared-database model. |

## Where Core earns its place

For a small Go team running a shared PostgreSQL database, a handler can forget
a tenant predicate even when it has already checked the caller's permission.
The [business-table proof](business-data-rls.md) shows how a signed
transaction binding and forced row-level security keep that omission from
exposing another organization's rows. It also checks a cross-tenant insert
and forged settings. This applies only to correctly configured tables and
non-owner, non-bypass runtime roles. A compromised trusted runtime or an
unprotected table is outside that proof.

Core also tests that delegated administrators cannot exceed their own grant
coverage, create delegation cycles, or silently overwrite an outdated role
version. The audit trail gives the operator a record of policy changes.
These are guarantees the general templates above do not document as a
combined default. Other frameworks can implement them; the comparison does
not claim that they cannot.

## Keep the footprint small

- Base: single organization, invitation-only accounts, membership, roles,
  sessions and audit. Multi mode stays opt-in.
- Keep the distribution path verified: checksummed Linux amd64 preview bundles
  already contain prebuilt API/CLI, Console and PostgreSQL images. Preserve
  matching revisions and tested upgrade/restore instructions; see the
  [archive install guide](../deploy/preview/README.md).
- Use the existing RLS example as the extension reference. Its opt-in list
  and creation form include a real permission check, read-only denial, and
  a negative isolation test. The default console has no invoice routes.
- Revisit self-service email recovery or external identity only if the
  intended audience expands beyond invitation-only internal administration.
