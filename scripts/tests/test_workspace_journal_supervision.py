"""Exercise daemon supervision with local stand-ins; never touch Docker/firewall."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WorkspaceJournalSupervisionTests(unittest.TestCase):
    def test_collector_failure_stops_daemon_after_installing_firewall(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "bin"
            binary.mkdir()
            (root / "run").mkdir()
            scripts = {
                "chown": "#!/bin/sh\nexit 0\n",
                "weft-workspace-journal": '#!/bin/sh\nwhile [ ! -f "$FIXTURE/daemon-ready" ]; do sleep 0.01; done\nexit 3\n',
                "dockerd-entrypoint.sh": '#!/bin/sh\ntrap \'echo stopped > "$FIXTURE/daemon-stopped"; exit 0\' TERM\ntouch "$FIXTURE/daemon-ready"\nwhile :; do sleep 0.1; done\n',
                "iptables": '#!/bin/sh\nprintf "%s\\n" "$*" >> "$FIXTURE/firewall-calls"\nexit 0\n',
            }
            for name, content in scripts.items():
                path = binary / name
                path.write_text(content)
                path.chmod(0o700)
            # Relocate only the script's installation-owned paths to the fixture.
            source = (ROOT / "deploy/sandbox-daemon/entrypoint.sh").read_text()
            source = source.replace("/run/weft", f"{root}/run/weft")
            source = source.replace("/var/lib/weft/journal", f"{root}/journal")
            script = root / "entrypoint.sh"
            script.write_text(source)
            env = {**os.environ, "PATH": f"{binary}:{os.environ['PATH']}", "FIXTURE": str(root)}
            result = subprocess.run(["sh", str(script)], env=env, capture_output=True, text=True, timeout=8)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertIn("workspace journal stopped", result.stderr)
            self.assertTrue((root / "daemon-stopped").exists())
            calls = (root / "firewall-calls").read_text()
            self.assertIn("-I DOCKER-USER", calls)
            self.assertIn("-I INPUT", calls)
            self.assertIn("-j DROP", calls)
