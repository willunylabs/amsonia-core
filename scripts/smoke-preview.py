"""Exercise the installed preview archive on an isolated Linux Docker runner."""

import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.error
import urllib.request

from smoke_support import enter


def main():
    os.chdir(Path(sys.argv[1]).resolve())
    env = {**os.environ, "COMPOSE_PROJECT_NAME": "amsonia-preview-ci", "AMSONIA_WEB_PORT": "18080"}
    compose = ["./amsonia-compose"]

    def run(args):
        subprocess.run(args, env=env, check=True, timeout=240)

    def call(path, body=None, token=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(
            "http://127.0.0.1:18080" + path,
            data=json.dumps(body).encode() if body else None,
            headers=headers,
        )
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.read()

    def login(password):
        return json.loads(call("/api/v1/auth/login", {"email": "owner@example.test", "password": password}))

    run(["docker", "load", "--input", "images.tar"])
    run(["./init-env"])
    config = json.loads(subprocess.check_output(compose + ["config", "--format", "json"], env=env))
    assert all("build" not in service for service in config["services"].values())
    assert all(service["pull_policy"] == "never" for service in config["services"].values())
    assert not config["services"]["postgres"].get("ports")
    assert [p["host_ip"] for p in config["services"]["web"]["ports"]] == ["127.0.0.1"]
    try:
        run(compose + ["up", "-d", "--wait", "--wait-timeout", "180"])
        assert json.loads(call("/readyz"))["status"] == "ready"
        assert b"Amsonia Core" in call("/")
        email = (b"Administrator email: ", "owner@example.test")
        original = "preview-original-test-password"
        replacement = "preview-replacement-test-password"
        enter(
            compose + ["exec", "api", "/usr/local/bin/amsonia", "bootstrap-admin"],
            [email, (b"Password: ", original)], env,
        )
        token = login(original)["access_token"]
        tenants = json.loads(call("/api/v1/tenants", token=token))["items"]
        assert len(tenants) == 1
        path = "/api/v1/tenants/" + tenants[0]["id"] + "/settings"
        assert json.loads(call(path, token=token))["name"] == "My organization"
        enter(
            compose + ["run", "--rm", "--entrypoint", "/usr/local/bin/amsonia", "migrate", "recover-admin"],
            [email, (b"New password (revokes all existing sessions): ", replacement),
             (b"Confirm new password: ", replacement)], env,
        )
        try:
            call(path, token=token)
        except urllib.error.HTTPError as exc:
            assert exc.code == 401
        else:
            raise AssertionError("Pre-recovery session remained valid")
        assert json.loads(call(path, token=login(replacement)["access_token"]))["name"] == "My organization"
        print("Preview archive installed successfully: web, readiness, bootstrap, sign-in, settings and recovery.")
    finally:
        run(compose + ["down", "--volumes"])


if __name__ == "__main__":
    main()
