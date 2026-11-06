# Member recovery and credential retention

Members who forget their password ask the system administrator for a private
recovery link. In the Console, open **Members → Manage member → Password
recovery**, verify the member's identity, enter your own administrator password,
and share the link privately. The link is shown once, expires in 30 minutes, and
works once. Creating another invalidates earlier pending links for that account.
No mail provider is required and no email is sent.

An active system administrator needs active membership and tenant-wide
`iam:member:manage` in the issuing organization. Ordinary tenant managers cannot
reset global account credentials. The target must be an active ordinary member.
System administrators retain the operator-only `recover-admin` workflow described
in the production runbook. Recovery never changes memberships, status, or roles.

The member chooses a 12–128 character password and then signs in normally.
Completion atomically consumes the link, updates credentials, ends all access
and refresh sessions, invalidates other pending recovery links, and records an
audit event. It does not create a session or change another account's browser
cookie. Changing a password or operator reprovisioning also invalidates pending
links. Link secrets live in URL fragments and only their SHA-256 hashes persist.
Do not put recovery links in public issues, logs, or analytics.

Migration 11 adds the account-level recovery table and expiration indexes.
After applying migrations, rerun `ops/configure_database_roles.sql` to grant the
runtime role SELECT/INSERT/UPDATE on recovery records. It gets no DELETE access
to global session or recovery records. The tenant export/purge role gets no
identity access. Back up the database before upgrading.

## Bounded operator cleanup

Use an operator connection with `AMSONIA_MIGRATION_DSN`. No HTTP cleanup route is
exposed and no automatic scheduler is installed.

```sh
amsonia prune-sessions
amsonia prune-sessions --apply
amsonia prune-sessions --apply --retention-days=60 --batch-size=500
```

The first command is a dry run. JSON output reports the database-clock cutoff
and the eligible row count **capped at the batch size, per table**. `--apply`
deletes at most one batch from each of `access_sessions`, `refresh_sessions`, and
`member_password_recoveries`, in one transaction. Each invocation is bounded;
repeat later if a table reports the batch limit. Locked rows are skipped during
execution, so concurrent cleanup does not wait behind session writes. A dry-run
count is an estimate of the next batch, not a total or a promise of deleted rows.

The default preserves records for 30 days **after expiry**, including revoked and
rotated refresh tokens. This retains replay detection during the grace period;
once pruned, those old tokens still fail authentication as unknown tokens. Live
sessions are never selected. Expired recovery metadata follows the same policy;
tenant audit events and the operator administrator recovery ledger are retained.
Allowed retention is 1–3650 days; batch size is 1–5000. Choose the retention window
according to your operational audit needs before reducing the default.

## Contract and browser validation

The PostgreSQL CI job captures synthetic requests and actual responses from the
real HTTP router, backed by migrated PostgreSQL and the least-privileged runtime
role. `scripts/validate-api-contract.py` checks these exchanges against the
OpenAPI 3.1 request, path/query parameter, status, content-type, and response
schemas. Invalid request probes and mutated real response probes prove drift is
rejected. Capture stays in runner temporary storage because it contains synthetic
test credentials; never enable it on live traffic or upload the capture.

The preview job exercises the released archive through a real browser at desktop
and mobile widths, including member invitation, recovery and account devices.
Screenshots are retained as CI artifacts; browser contexts close on failures too.
