#!/usr/bin/env python3
"""Qualify a format-1 native upgrade using disposable binaries and state only."""
import contextlib
import hashlib
import json
import pathlib
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
BASELINE = "faf4644"  # First committed native format-1 launcher, not an external artifact.


def run(args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def request(base, token, method, path, data=None):
    body = None if data is None else json.dumps(data).encode()
    req = urllib.request.Request(base + path, data=body, method=method,
                                 headers={"Authorization": "Bearer " + token,
                                          "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=5) as response:
        return response.status, json.load(response)


@contextlib.contextmanager
def server(binary, profile, base):
    process = subprocess.Popen([str(binary), "--local", "--data-dir", str(profile)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(150):
            if process.poll() is not None:
                raise RuntimeError("disposable native server exited during startup")
            try:
                with urllib.request.urlopen(base + "/healthz", timeout=1) as response:
                    if response.status == 200:
                        break
            except (urllib.error.URLError, TimeoutError):
                time.sleep(0.05)
        else:
            raise RuntimeError("disposable native startup timed out")
        yield
    finally:
        if process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise RuntimeError("disposable native server did not stop gracefully")
        if process.returncode != 0:
            raise RuntimeError("disposable native server failed")


def snapshot(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob("*") if p.is_file()}


def main():
    with tempfile.TemporaryDirectory(prefix="weft-native-upgrade-") as directory:
        temp = pathlib.Path(directory)
        source = temp / "baseline"
        source.mkdir()
        archive = temp / "baseline.tar"
        with archive.open("wb") as output:
            subprocess.run(["git", "archive", BASELINE, "server"], cwd=ROOT,
                           check=True, stdout=output)
        run(["tar", "-xf", str(archive), "-C", str(source)])
        old, current = temp / "weftd-old", temp / "weftd-current"
        run(["go", "build", "-o", str(old), "./cmd/weftd"], cwd=source / "server")
        run(["go", "build", "-o", str(current), "./cmd/weftd"], cwd=ROOT / "server")
        web = temp / "web"
        web.mkdir()
        (web / "index.html").write_text("<title>Upgrade fixture</title>")
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        profile = temp / "profile"
        run([str(old), "--init-local", "--data-dir", str(profile), "--web-dir", str(web), "--port", str(port)])
        # Only this test-created synthetic credential is read; never a user profile.
        token = (profile / "admin.token").read_text().strip()
        base = "http://127.0.0.1:" + str(port)
        graph = {"metadata": {"id": "upgrade-fixture", "name": "Preserve this graph"},
                 "spec": {"nodes": [{"id": "worker", "runtime": {"kind": "echo"},
                                      "loop": {"mode": "once"}, "tools": []}]}}
        with server(old, profile, base):
            assert request(base, token, "POST", "/api/v1/graphs", graph)[0] == 201
            original = request(base, token, "GET", "/api/v1/graphs/upgrade-fixture")[1]
        run([str(current), "--backup-local", str(temp / "before-upgrade.tar.gz"), "--data-dir", str(profile)])
        with server(current, profile, base):
            assert request(base, token, "GET", "/api/v1/graphs/upgrade-fixture")[1] == original
            factory = request(base, token, "GET", "/api/v1/graphs/software-factory")[1]
            assert factory["graph"]["metadata"]["name"] == "Software Factory"
            assert request(base, token, "GET", "/api/v1/graphs/upgrade-fixture/executions")[1] == []
        with server(current, profile, base):
            assert request(base, token, "GET", "/api/v1/graphs/upgrade-fixture")[1] == original
            assert request(base, token, "GET", "/api/v1/graphs/software-factory")[1] == factory
        manifest = profile / "installation.json"
        manifest.write_text('{"version":999}')
        before = snapshot(profile)
        result = subprocess.run([str(current), "--local", "--data-dir", str(profile)],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        assert result.returncode != 0, "unsupported format accepted"
        assert snapshot(profile) == before, "failed upgrade changed persistent files"
    print("Native format-1 upgrade, restart and unsupported-format preservation passed (baseline " + BASELINE + ").")


if __name__ == "__main__":
    main()
