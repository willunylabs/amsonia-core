"""Verify a real backup/restore upgrade on an isolated PostgreSQL 16 cluster."""

import argparse
import base64
from contextlib import contextmanager
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
from urllib.parse import urlsplit, urlunsplit
import urllib.request

from smoke_support import enter

ROOT = Path(__file__).resolve().parent.parent
ADMIN_EMAIL = "upgrade-owner@example.test"
MEMBER_EMAIL = "upgrade-member@example.test"
ORG_NAME = "Restored organization"


def run(args, **kwargs):
    return subprocess.run(args, check=True, timeout=90, **kwargs)


@contextmanager
def api(binary, env, log):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    address = f"127.0.0.1:{port}"
    with log.open("w+") as output:
        proc = subprocess.Popen([binary], env={**env, "AMSONIA_HTTP_ADDR": address},
                                stdout=output, stderr=output)
        try:
            def request(path, *, method="GET", body=None, token=None, status=200):
                headers = {"Content-Type": "application/json"}
                if token:
                    headers["Authorization"] = "Bearer " + token
                req = urllib.request.Request(
                    f"http://{address}{path}", method=method, headers=headers,
                    data=json.dumps(body).encode() if body is not None else None,
                )
                try:
                    with urllib.request.urlopen(req, timeout=3) as response:
                        actual, raw = response.status, response.read()
                except urllib.error.HTTPError as exc:
                    actual, raw = exc.code, exc.read()
                if actual != status:
                    raise AssertionError(f"{method} {path}: HTTP {actual}, expected {status}")
                return json.loads(raw) if raw else None

            deadline = time.monotonic() + 15
            while True:
                if proc.poll() is not None or time.monotonic() >= deadline:
                    raise RuntimeError("API did not become ready")
                try:
                    assert request("/readyz")["status"] == "ready"
                    break
                except urllib.error.URLError:
                    time.sleep(0.2)
            yield request
        except Exception:
            output.seek(0)
            print(output.read()[-8000:], flush=True)
            raise
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()


def login(request, email, password):
    session = request("/api/v1/auth/login", method="POST", body={"email": email, "password": password})
    return session["account"]["id"], session["access_token"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ["old-cli", "old-api", "new-cli", "new-api"]:
        parser.add_argument("--" + name, type=lambda p: Path(p).resolve(), required=True)
    parser.add_argument("--old-role-sql", type=lambda p: Path(p).resolve(), required=True)
    parser.add_argument("--pg-container", help="Use the matching clients inside the CI service container")
    args = parser.parse_args()
    admin = urlsplit(os.environ["TEST_DATABASE_ADMIN_URL"])
    if admin.scheme not in ("postgres", "postgresql") or not admin.hostname:
        raise ValueError("Use a PostgreSQL test-cluster URL")
    nonce = secrets.token_hex(6)
    source, target = f"upgrade_src_{nonce}", f"upgrade_dst_{nonce}"
    runtime, maintenance = f"upgrade_rt_{nonce}", f"upgrade_mt_{nonce}"
    roles, databases = [], []
    runtime_password = secrets.token_hex(24)
    maintenance_password = secrets.token_hex(24)
    owner_password, member_password = secrets.token_hex(20), secrets.token_hex(20)
    runtime_secret, maintenance_secret = secrets.token_bytes(32), secrets.token_bytes(32)

    def dsn(db, use_runtime=False):
        netloc = admin.netloc
        if use_runtime:
            netloc = f"{runtime}:{runtime_password}@{admin.netloc.rsplit('@', 1)[-1]}"
        return urlunsplit(admin._replace(netloc=netloc, path="/" + db))

    def pg(command, *options, **kwargs):
        if args.pg_container:
            executable = ["docker", "exec", "-i", args.pg_container, command]
        else:
            executable = [str(Path(os.environ.get("PG_BINDIR", "")) / command)]
        return run([*executable, *options], **kwargs)

    def sql(db, statement):
        pg("psql", "--no-psqlrc", "-v", "ON_ERROR_STOP=1", dsn(db),
           input=statement, text=True, stdout=subprocess.DEVNULL)

    def environment(db):
        return {
            **os.environ, "AMSONIA_MIGRATION_DSN": dsn(db),
            "AMSONIA_DATABASE_DSN": dsn(db, use_runtime=True),
            "AMSONIA_TENANT_BINDING_SECRET": base64.urlsafe_b64encode(runtime_secret).decode().rstrip("="),
            "AMSONIA_TENANCY_MODE": "single", "AMSONIA_ORGANIZATION_ID": "",
            "AMSONIA_ORGANIZATION_NAME": "Original organization",
        }

    def grants(db, role_sql):
        pg("psql", "--no-psqlrc", dsn(db), "-v", f"runtime_role={runtime}",
           "-v", f"maintenance_role={maintenance}", "-v", f"runtime_secret_hex={runtime_secret.hex()}",
           "-v", f"maintenance_secret_hex={maintenance_secret.hex()}",
           input=role_sql.read_bytes(), stdout=subprocess.DEVNULL)

    base_db = admin.path.lstrip("/")
    try:
        for name, password in ((runtime, runtime_password), (maintenance, maintenance_password)):
            sql(base_db, f"CREATE ROLE {name} LOGIN PASSWORD '{password}' NOSUPERUSER NOBYPASSRLS")
            roles.append(name)
        for db in (source, target):
            sql(base_db, f"CREATE DATABASE {db}")
            databases.append(db)
        with tempfile.TemporaryDirectory(prefix="amsonia-upgrade-") as path:
            temp = Path(path)
            old_env, new_env = environment(source), environment(target)
            run([args.old_cli, "migrate"], env=old_env)
            status = run([args.old_cli, "migration-status"], env=old_env, capture_output=True, text=True)
            assert status.stdout.splitlines()[-1].startswith("000009 "), "Baseline must be schema 9"
            grants(source, args.old_role_sql)
            enter([args.old_cli, "bootstrap-admin"],
                  [(b"Administrator email: ", ADMIN_EMAIL), (b"Password: ", owner_password)], old_env)
            run([args.old_cli, "provision-demo-viewer"], env={
                **old_env, "AMSONIA_DEMO_VIEWER_EMAIL": MEMBER_EMAIL,
                "AMSONIA_DEMO_VIEWER_PASSWORD": member_password,
                "AMSONIA_DEMO_TENANT_NAME": "Original organization",
            })
            with api(args.old_api, old_env, temp / "old.log") as old:
                owner_id, token = login(old, ADMIN_EMAIL, owner_password)
                member_id, _ = login(old, MEMBER_EMAIL, member_password)
                tenants = old("/api/v1/tenants", token=token)["items"]
                assert len(tenants) == 1
                base = f"/api/v1/tenants/{tenants[0]['id']}"
                path = base + "/settings"
                original = old(path, token=token)
                saved = old(path, method="PATCH", token=token,
                            body={"name": ORG_NAME, "expected_version": original["version"]})
                assert saved["name"] == ORG_NAME and saved["version"] == original["version"] + 1
                audit_before = old(base + "/audit-events", token=token)["items"]
                assert any(e["operation"] == "organization.update" for e in audit_before)

            backup = temp / "source.dump"
            with backup.open("wb") as stream:
                pg("pg_dump", "--format=custom", "--dbname", dsn(source), stdout=stream)
            with backup.open("rb") as stream:
                pg("pg_restore", "--exit-on-error", "--single-transaction", "--dbname", dsn(target), stdin=stream)
            # The preceding application must still work on the restored database.
            with api(args.old_api, new_env, temp / "restored.log") as restored:
                assert restored(path, token=login(restored, ADMIN_EMAIL, owner_password)[1]) == saved
            run([args.new_cli, "migrate"], env=new_env)
            status = run([args.new_cli, "migration-status"], env=new_env, capture_output=True, text=True)
            assert int(status.stdout.splitlines()[-1].split()[0]) > 9, "Upgrade must advance the schema"
            grants(target, ROOT / "ops/configure_database_roles.sql")
            with api(args.new_api, new_env, temp / "new.log") as new:
                restored_owner, token = login(new, ADMIN_EMAIL, owner_password)
                restored_member, member_token = login(new, MEMBER_EMAIL, member_password)
                assert (restored_owner, restored_member) == (owner_id, member_id)
                assert new(path, token=token) == saved
                assert new(path, token=member_token)["can_manage"] is False
                new(path, method="PATCH", token=member_token,
                    body={"name": "Forbidden update", "expected_version": saved["version"]}, status=403)
                assert new(path, token=token) == saved
                audit_after = new(base + "/audit-events", token=token)["items"]
                assert audit_after == audit_before
            print("Backup restored; old and new applications preserved accounts, organization and audit. Member write returned 403.")
    finally:
        for db in reversed(databases):
            sql(base_db, f"DROP DATABASE {db} WITH (FORCE)")
        for name in reversed(roles):
            sql(base_db, f"DROP ROLE {name}")


if __name__ == "__main__":
    main()
