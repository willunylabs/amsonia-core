"""Verify the shipped Nginx config and prefix rewrite on an isolated CI runner."""

from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request


class Backend(BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps({"path": self.path, "host": self.headers.get("Host")}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, timeout=120, **kwargs).strip()


@contextmanager
def nginx(config, headers, html):
    container = run("docker", "run", "--detach", "--rm", "--add-host", "api:host-gateway",
                    "--publish", "127.0.0.1::8080",
                    "--mount", f"type=bind,source={config},target=/etc/nginx/conf.d/default.conf,readonly",
                    "--mount", f"type=bind,source={headers},target=/etc/nginx/includes/security-headers.conf,readonly",
                    "--mount", f"type=bind,source={html},target=/usr/share/nginx/html,readonly",
                    "nginx:1.28-alpine")
    try:
        port = run("docker", "port", container, "8080/tcp").rsplit(":", 1)[1]
        base = f"http://127.0.0.1:{port}"
        deadline = time.monotonic() + 15
        while True:
            try:
                with urllib.request.urlopen(base, timeout=1):
                    break
            except (OSError, urllib.error.URLError):
                if time.monotonic() > deadline:
                    raise
                time.sleep(0.1)
        yield base
    finally:
        run("docker", "stop", "--time", "3", container)


def main():
    root = Path(__file__).resolve().parent.parent
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Backend)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        with tempfile.TemporaryDirectory(prefix="core-web-proxy-") as directory:
            directory = Path(directory)
            html = directory / "html"
            (html / "assets").mkdir(parents=True)
            (html / "index.html").write_text("<!doctype html><title>Amsonia Core</title>")
            (html / "assets" / "app.js").write_text("console.log('fixture');")
            config = directory / "nginx.conf"
            for prefix in ("", "/backend"):
                config.write_text((root / "ops/nginx.conf").read_text())
                run("node", str(root / "scripts/configure-web-proxy.mjs"), str(config),
                    env={**os.environ, "VITE_API_URL": prefix})
                with nginx(config, root / "ops/nginx-security-headers.conf", html) as base:
                    for path, status in (("/", 200), ("/spa/route", 200),
                                         ("/assets/app.js", 200), ("/assets/missing.js", 404)):
                        try:
                            response = urllib.request.urlopen(base + path, timeout=5)
                        except urllib.error.HTTPError as error:
                            response = error
                        with response:
                            assert response.status == status, (path, response.status)
                            headers = response.headers
                            assert headers["X-Content-Type-Options"] == "nosniff", path
                            assert headers["Referrer-Policy"] == "no-referrer", path
                            assert headers["X-Frame-Options"] == "DENY", path
                            assert "frame-ancestors 'none'" in headers["Content-Security-Policy"], path
                            if path == "/assets/app.js":
                                assert "immutable" in ",".join(headers.get_all("Cache-Control")), path
                            if path in ("/", "/spa/route"):
                                assert headers["Cache-Control"] == "no-cache", path
                    with urllib.request.urlopen(base + prefix + "/api/v1/proxy-check?q=one", timeout=5) as response:
                        proxied = json.load(response)
                        assert proxied["path"] == "/api/v1/proxy-check?q=one"
                        assert proxied["host"] == base.removeprefix("http://"), proxied
                print(f"Shipped Nginx: security headers, cache policy, SPA, 404 and API prefix {prefix!r} passed.")
            for prefix in ("/", "backend", "/backend/", "/a/../b", "/a?x", "/a;", "//host", "/a\n"):
                result = subprocess.run(["node", str(root / "scripts/configure-web-proxy.mjs"), str(config)],
                                        env={**os.environ, "VITE_API_URL": prefix}, capture_output=True, timeout=5)
                assert result.returncode != 0, f"Unsafe prefix accepted: {prefix!r}"
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=5)


if __name__ == "__main__":
    main()
