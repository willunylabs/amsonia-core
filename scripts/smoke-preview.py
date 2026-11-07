"""Exercise the installed preview archive on an isolated Linux Docker runner."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import urllib.error
import urllib.request

from smoke_support import enter


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("bundle", type=Path)
    parser.add_argument("--browser-module", type=Path, help="Run the first-login browser check")
    parser.add_argument("--screenshots", type=Path)
    args = parser.parse_args()
    browser_script = Path(__file__).resolve().with_name("smoke-first-login.cjs")
    browser_module = args.browser_module.resolve() if args.browser_module else None
    screenshots = args.screenshots.resolve() if args.screenshots else None
    if bool(browser_module) != bool(screenshots):
        parser.error("--browser-module and --screenshots must be used together")
    os.chdir(args.bundle.resolve())
    port = "8080" if browser_module else "18080"
    base_url = f"http://127.0.0.1:{port}"
    env = {**os.environ, "COMPOSE_PROJECT_NAME": "amsonia-preview-ci", "AMSONIA_WEB_PORT": port}
    compose = ["./amsonia-compose"]

    def run(args, extra_env=None):
        subprocess.run(args, env={**env, **(extra_env or {})}, check=True, timeout=240)

    def call(path, body=None, token=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(
            base_url + path,
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
        current_console = env.get("SMOKE_CONSOLE_CONTRACT", "current") in ("current", "published-preview-5")
        original = "🔐" * 128 if current_console else "preview-original-test-password"
        replacement = "preview-replacement-test-password"
        enter(
            compose + ["exec", "api", "/usr/local/bin/amsonia", "bootstrap-admin"],
            [email, (b"Password: ", original)], env,
        )
        if browser_module:
            run(["node", browser_script, browser_module, base_url, screenshots], {
                "SMOKE_LOGIN_EMAIL": email[1], "SMOKE_LOGIN_PASSWORD": original,
            })
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
