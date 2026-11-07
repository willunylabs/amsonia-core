"""Upgrade a public preview in place on an isolated Linux Docker runner."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.request

from smoke_support import enter


UP = ["up", "-d", "--wait", "--wait-timeout", "180"]
EMAIL = "upgrade-owner@example.test"
PASSWORD = "preview-upgrade-test-password"
ORG_NAME = "My organization after upgrade"
BASE_URL = "http://127.0.0.1:8080"

# Ignore the bookkeeping timestamp that role provisioning refreshes.
# For the upgrade to schema 12, compare the previous session columns separately.
# Compare hashes rather than putting credentials or application data in CI logs.
SNAPSHOT = r"""
SELECT format(
    'SELECT %L, count(*), md5(coalesce(string_agg(row_data, E''\n'' ORDER BY row_data COLLATE "C"), '''')) FROM
        (SELECT (%s)::text AS row_data FROM %I.%I t) rows',
    schemaname || '.' || tablename,
    CASE WHEN schemaname = 'amsonia' AND tablename = 'runtime_secrets'
         THEN 'to_jsonb(t) - ''rotated_at'''
         WHEN :omit_session_start AND schemaname = 'amsonia'
             AND tablename IN ('access_sessions', 'refresh_sessions')
         THEN 'to_jsonb(t) - ''session_started_at'''
         ELSE 'to_jsonb(t)' END,
    schemaname, tablename)
FROM pg_tables
WHERE schemaname IN ('amsonia', 'public')
ORDER BY schemaname, tablename
\gexec
"""

SESSION_BACKFILL = """
WITH sessions AS (
    SELECT account_id, session_id, created_at, session_started_at FROM amsonia.access_sessions
    UNION ALL
    SELECT account_id, session_id, created_at, session_started_at FROM amsonia.refresh_sessions
)
SELECT count(*) > 0 AND bool_and(session_started_at IS NOT DISTINCT FROM first_created_at)
FROM (
    SELECT session_started_at, min(created_at) OVER (PARTITION BY account_id, session_id) AS first_created_at
    FROM sessions
) families;
"""


def verify_upgrade(before, after, old_migrations, new_migrations):
    changed = sorted(name for name in before
                     if name != "amsonia.schema_migrations" and before[name] != after.get(name))
    assert not changed, f"Unexpected data changes in: {', '.join(changed)}"
    for name in after.keys() - before.keys():
        assert name == "amsonia.member_password_recoveries", f"Unexpected new table: {name}"
        assert after[name] == "0|d41d8cd98f00b204e9800998ecf8427e", f"New table is not empty: {name}"

    assert old_migrations, "Source migration history is missing"
    assert new_migrations[:len(old_migrations)] == old_migrations, "Existing migration history changed"
    assert [row["version"] for row in new_migrations] == list(range(1, len(new_migrations) + 1)), \
        "Migration history is not contiguous"
    assert all(row["dirty"] is False for row in new_migrations), "Migration history contains dirty rows"
    # API startup also verifies names and checksums against the new binary.


def request(path, *, method="GET", token=None, body=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(
        BASE_URL + path, method=method, headers=headers,
        data=json.dumps(body).encode() if body is not None else None,
    )
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)


def login():
    return request("/api/v1/auth/login", method="POST", body={"email": EMAIL, "password": PASSWORD})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("old", "new", "browser", "screenshots"):
        parser.add_argument(name, type=lambda path: Path(path).resolve())
    args = parser.parse_args()
    env = {**os.environ, "COMPOSE_PROJECT_NAME": "amsonia-preview-upgrade-ci"}
    browser_script = Path(__file__).resolve().with_name("smoke-first-login.cjs")

    def run(bundle, command, **kwargs):
        return subprocess.run(command, cwd=bundle, env=env, check=True, timeout=240, **kwargs)

    def compose(bundle, *command, **kwargs):
        return run(bundle, ["./amsonia-compose", *command], **kwargs)

    def pg(bundle, command, *options, **kwargs):
        return compose(bundle, "exec", "-T", "postgres", command, "-U", "amsonia_owner", *options, **kwargs)

    def snapshot(bundle, database="amsonia_core", *, omit_session_start=False):
        result = pg(bundle, "psql", "-XAt", "-v", "ON_ERROR_STOP=1", "-d", database,
                    "-v", f"omit_session_start={'true' if omit_session_start else 'false'}",
                    input=SNAPSHOT, text=True, capture_output=True)
        return dict(line.split("|", 1) for line in result.stdout.splitlines())

    def migration_history(bundle):
        result = pg(bundle, "psql", "-XAt", "-v", "ON_ERROR_STOP=1", "-d", "amsonia_core",
                    input="SELECT json_agg(t ORDER BY version) FROM amsonia.schema_migrations t;",
                    text=True, capture_output=True)
        return json.loads(result.stdout)

    run(args.old, ["docker", "load", "--input", "images.tar"])
    run(args.old, ["./init-env"])
    active = args.old
    try:
        compose(args.old, *UP)
        enter([args.old / "amsonia-compose", "exec", "api", "/usr/local/bin/amsonia", "bootstrap-admin"],
              [(b"Administrator email: ", EMAIL), (b"Password: ", PASSWORD)], env)
        original = login()
        token = original["access_token"]
        tenants = request("/api/v1/tenants", token=token)["items"]
        assert len(tenants) == 1
        base = "/api/v1/tenants/" + tenants[0]["id"]
        settings_path = base + "/settings"
        original_settings = request(settings_path, token=token)
        saved = request(settings_path, method="PATCH", token=token,
                        body={"name": ORG_NAME, "expected_version": original_settings["version"]})
        assert saved["name"] == ORG_NAME
        invite_path = base + "/invitations"
        request(invite_path, method="POST", token=token, body={"email": "pending@example.test"})
        invitations = request(invite_path, token=token)["items"]
        audit = request(base + "/audit-events", token=token)["items"]
        assert invitations and any(event["operation"] == "organization.update" for event in audit)

        compose(args.old, "stop", "web", "api")
        before = snapshot(args.old)
        old_migrations = migration_history(args.old)
        assert "amsonia.audit_events" in before
        with tempfile.TemporaryFile() as backup:
            pg(args.old, "pg_dump", "-Fc", "-d", "amsonia_core", stdout=backup)
            pg(args.old, "createdb", "amsonia_preview_restore")
            backup.seek(0)
            pg(args.old, "pg_restore", "--exit-on-error", "--single-transaction",
               "-d", "amsonia_preview_restore", stdin=backup)
            assert snapshot(args.old, "amsonia_preview_restore") == before, "Restored backup differs"
        pg(args.old, "dropdb", "amsonia_preview_restore")
        print(f"Verified backup by restoring and comparing {len(before)} database tables.", flush=True)

        compose(args.old, "down")
        config = (args.old / ".env").read_bytes()
        target_config = args.new / ".env"
        with open(os.open(target_config, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as stream:
            stream.write(config)
        run(args.new, ["docker", "load", "--input", "images.tar"])
        active = args.new
        compose(args.new, *UP)
        assert request("/readyz")["status"] == "ready"
        after = snapshot(args.new)
        new_migrations = migration_history(args.new)
        if len(old_migrations) < 12 <= len(new_migrations):
            verify_upgrade(before, snapshot(args.new, omit_session_start=True), old_migrations, new_migrations)
            result = pg(args.new, "psql", "-XAt", "-v", "ON_ERROR_STOP=1", "-d", "amsonia_core",
                        input=SESSION_BACKFILL, text=True, capture_output=True)
            assert result.stdout.strip() == "t", "Session start backfill differs from earliest retained family rows"
        else:
            verify_upgrade(before, after, old_migrations, new_migrations)
        print(f"In-place upgrade preserved {len(before) - 1} data tables and prior migration history; "
              f"schema {len(old_migrations)} → {len(new_migrations)}.", flush=True)
        if "amsonia.member_password_recoveries" in after:
            result = compose(args.new, "run", "--rm", "-T", "--entrypoint", "/usr/local/bin/amsonia",
                             "migrate", "prune-sessions", capture_output=True, text=True)
            preview = json.loads(result.stdout)
            assert preview["applied"] is False and preview["batch_size"] == 1000
            assert all(preview[key] == 0 for key in ("access_sessions", "refresh_sessions", "password_recoveries"))
            assert snapshot(args.new) == after, "Default session cleanup changed data"
            print("Packaged session cleanup defaults to a non-mutating dry run.", flush=True)

        upgraded = login()
        for field in ("id", "email", "system_admin", "created_at"):
            assert upgraded["account"][field] == original["account"][field], f"Administrator {field} changed"
        assert upgraded["account"]["system_admin"]
        token = upgraded["access_token"]
        assert request(settings_path, token=token) == saved
        assert request(invite_path, token=token)["items"] == invitations
        assert request(base + "/audit-events", token=token)["items"] == audit
        subprocess.run(
            ["node", browser_script, args.browser, BASE_URL, args.screenshots],
            env={**env, "SMOKE_LOGIN_EMAIL": EMAIL, "SMOKE_LOGIN_PASSWORD": PASSWORD,
                 "SMOKE_ORGANIZATION": ORG_NAME}, check=True, timeout=120,
        )
        print("Original administrator, organization settings, invitation and audit history verified.")
    finally:
        compose(active, "down", "--volumes")


if __name__ == "__main__":
    main()
