"""Compose safety contract, with real configuration rendering and synthetic values."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]


def render_compose(overrides=None):
    # Do not inherit installation secrets or read its .env. No services are started.
    env = {"PATH": os.environ["PATH"], "HOME": os.environ.get("HOME", "/tmp"),
           "WEFT_ADMIN_TOKEN": "synthetic-test-admin",
           "WEFT_RUNTIME_SECRET": "synthetic-test-runtime",
           "POSTGRES_PASSWORD": "synthetic-test-database",
           "GRAFANA_ADMIN_PASSWORD": "synthetic-test-grafana"}
    env.update(overrides or {})
    return subprocess.run(
        ["docker", "compose", "--env-file", os.devnull, "-f",
         str(ROOT / "docker-compose.yml"), "--profile", "ollama", "config", "--format", "json"],
        env=env, capture_output=True, text=True, timeout=30)


class ComposeSourceTests(unittest.TestCase):
    def test_publications_default_to_loopback_and_preview_is_fixed(self):
        source = (ROOT / "docker-compose.yml").read_text()
        for port in (8080, 9090, 3000, 11434):
            self.assertIn('${WEFT_BIND_ADDRESS:-127.0.0.1}:' + str(port) + ':' + str(port), source)
        self.assertIn('"127.0.0.1:8082:8082"', source)
        self.assertNotIn('/var/run/docker.sock', source)


@unittest.skipUnless(shutil.which("docker"), "Docker Compose is required for configuration integration")
class ComposeConfigurationTests(unittest.TestCase):
    def config(self, overrides=None):
        result = render_compose(overrides)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_default_ports_and_internal_services(self):
        config = self.config()
        services = config["services"]
        for service in ("weftd", "prometheus", "grafana", "ollama"):
            self.assertTrue(services[service]["ports"])
            self.assertTrue(all(p["host_ip"] == "127.0.0.1" for p in services[service]["ports"]))
        for service in ("postgres", "nats", "agents", "sandbox-daemon", "sandbox-image", "tempo", "loki", "otel-collector"):
            self.assertFalse(services[service].get("ports"), service)
        self.assertEqual(set(services["sandbox-daemon"]["networks"]), {"sandbox"})
        self.assertEqual(set(services["agents"]["networks"]), {"default"})
        for service in ("weftd", "postgres", "nats", "agents", "sandbox-daemon"):
            self.assertEqual(services[service]["restart"], "unless-stopped")
        for service, volume in (("weftd", "weftd-data"), ("weftd", "workspaces"),
                                ("postgres", "pg-data"), ("nats", "nats-data"),
                                ("sandbox-daemon", "sandbox-daemon-data")):
            self.assertTrue(any(v["type"] == "volume" and v["source"] == volume
                                for v in services[service]["volumes"]))

    def test_explicit_remote_binding_keeps_preview_local(self):
        services = self.config({"WEFT_BIND_ADDRESS": "192.0.2.10"})["services"]
        for service in ("weftd", "prometheus", "grafana", "ollama"):
            self.assertEqual(services[service]["ports"][0]["host_ip"], "192.0.2.10")
        self.assertEqual(services["weftd"]["ports"][1]["host_ip"], "127.0.0.1")

    def test_required_credentials_fail_closed(self):
        for key in ("WEFT_ADMIN_TOKEN", "WEFT_RUNTIME_SECRET", "POSTGRES_PASSWORD", "GRAFANA_ADMIN_PASSWORD"):
            with self.subTest(key=key):
                result = render_compose({key: ""})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(key, result.stderr)
