# Self-hosting and recovery

Use a reviewed Git tag or commit, not a moving branch, for each deployment.
Build the API, CLI and console from the same revision. Use the Compose stack
for local evaluation; for a live installation use a trusted HTTPS reverse
proxy and keep PostgreSQL off the public network. Configure the [three database
roles](getting-started.md) before starting the API. Keep the database-owner DSN
in the operator environment only.

## Recover an administrator

Run the CLI from the deployed revision in an interactive operator terminal,
with `AMSONIA_MIGRATION_DSN` pointing at the intended database:

```sh
go run ./cmd/amsonia recover-admin
```

Enter the existing active administrator's email and the new password twice.
Passwords are not echoed or accepted as flags. The command clears lockout,
replaces the password and revokes every pre-recovery device session in one
transaction. It does not create, promote or reactivate accounts. Verify that
sign-in works with the new password and an earlier browser session no longer
works. No mail server is needed. This is operator recovery for system
administrators; invited members do not have a self-service reset flow.

The database owner can inspect successful recoveries:

```sql
SELECT account_id, database_operator, recovered_at
FROM amsonia.administrator_recoveries
ORDER BY id DESC;
```

The record identifies the database login, which may be shared across operators;
use deployment access logs to attribute individual operators. This ledger is
not granted to the standard API or maintenance role.

## Back up, restore and upgrade

Use a PostgreSQL backup operator that can read **all** rows, including tables
with forced RLS. The normal API role is not a backup role. Keep encrypted,
access-controlled backups: they contain credential hashes, sessions, and
database binding secrets. Save the deployed revision, the deployment
configuration and protected secrets separately. A database dump does not
create cluster roles on the restore server.

1. Stop application writes during the upgrade window. Use PostgreSQL client
   tools compatible with the source server and take a full custom-format dump:
   `pg_dump --dbname="$BACKUP_DSN" --format=custom --file="$BACKUP_FILE"`.
2. Restore into a separate **empty** database first. Provision the matching
   roles on that cluster, then use
   `pg_restore --exit-on-error --single-transaction --dbname="$RESTORE_DSN" "$BACKUP_FILE"`
   with a restore operator allowed to recreate the original ownership and ACLs.
   Keep test restores private; restored session credentials may still be valid.
3. Verify the old revision against the restore, then run the new CLI's
   `migrate` and `migration-status` against the copy. Reapply
   `ops/configure_database_roles.sql` with the intended secrets and review
   `AMSONIA_TENANCY_MODE` / `AMSONIA_ORGANIZATION_ID` before starting the new API.
4. Check `/readyz`, administrator sign-in, an ordinary member's permissions,
   the organization settings page, and audit history. For multi mode, verify
   that a member cannot read a second organization's resources.
5. If a migration is dirty or verification fails, keep the live service
   stopped while diagnosing. For rollback, restore the pre-upgrade backup into
   a fresh database and switch back to the matching prior binaries and
   configuration. Do not point old binaries at a newer schema or manually
   mark a failed migration clean. A pre-upgrade restore discards later writes.

## What CI verifies

The PostgreSQL job runs [the upgrade smoke check](https://github.com/willunylabs/amsonia-core/blob/main/scripts/smoke-upgrade.py).
It starts from the application at commit `d0f90fb` (schema 9), creates a system
administrator and a read-only member, and changes the organization name to
produce an audit event. It restores a custom-format dump into an empty database,
checks it with the old API, then runs the current migration CLI and API.
Both users keep their identities and can sign in; the name, version and audit
history are preserved, and the read-only member still receives HTTP 403 on
a settings update.

This is a PostgreSQL 16 test using two databases in one disposable cluster. It
does not prove every historical upgrade, a PostgreSQL major version upgrade, or
cluster role restoration on another server. Rehearse each deployment's own
backup and restore before cutting over.
