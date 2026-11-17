"""Exercise daemon supervision with local stand-ins; never touch Docker/firewall."""
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WorkspaceJournalSupervisionTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.binary = self.root / "bin"
        self.binary.mkdir()
        (self.root / "run").mkdir()
        self.install("chown", "exit 0\n")
        self.install("dockerd-entrypoint.sh", """
trap 'echo stopped > "$FIXTURE/daemon-stopped"; exit 0' TERM
touch "$FIXTURE/daemon-ready"
while :; do sleep 0.1; done
""")
        self.install("iptables", """
printf '%s\\n' "$*" >> "$FIXTURE/firewall-calls"
if [ "$1" = -nL ]; then
    test -f "$FIXTURE/daemon-ready" && test -f "$FIXTURE/journal-ready"
    exit $?
fi
if [ "$*" = "$FAIL_FIREWALL_RULE" ]; then
    touch "$FIXTURE/firewall-rejected"
    exit 1
fi
exit 0
""")
        # Relocate installation paths and prevent use of a host iptables binary.
        source = (ROOT / "deploy/sandbox-daemon/entrypoint.sh").read_text()
        source = source.replace("/run/weft", f"{self.root}/run/weft")
        source = source.replace("/var/lib/weft/journal", f"{self.root}/journal")
        source = source.replace("/usr/local/sbin/.iptables-legacy/iptables", f"{self.root}/legacy-iptables")
        self.script = self.root / "entrypoint.sh"
        self.script.write_text(source)

    def install(self, name, content):
        path = self.binary / name
        path.write_text("#!/bin/sh\n" + content)
        path.chmod(0o700)

    def run_entrypoint(self, fail_rule=""):
        env = {
            **os.environ,
            "PATH": f"{self.binary}:{os.environ['PATH']}",
            "FIXTURE": str(self.root),
            "FAIL_FIREWALL_RULE": fail_rule,
        }
        command = ["sh", str(self.script)]
        with subprocess.Popen(
            command, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, start_new_session=True,
        ) as process:
            try:
                stdout, stderr = process.communicate(timeout=8)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                stdout, stderr = process.communicate()
                self.fail(f"entrypoint timed out: {stdout}{stderr}")
            return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)

    def test_collector_failure_stops_daemon_after_installing_firewall(self):
        self.install("weft-workspace-journal", """
touch "$FIXTURE/journal-ready"
while [ ! -f "$FIXTURE/daemon-ready" ]; do sleep 0.01; done
# Let the firewall check observe a started collector before it fails.
while [ ! -f "$FIXTURE/run/weft-iptables" ]; do sleep 0.01; done
exit 3
""")
        result = self.run_entrypoint()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("workspace journal stopped", result.stderr)
        self.assertTrue((self.root / "daemon-stopped").exists())
        calls = (self.root / "firewall-calls").read_text()
        self.assertIn("-I DOCKER-USER", calls)
        self.assertIn("-I INPUT", calls)
        self.assertIn("-j DROP", calls)

    def test_entrypoint_exits_on_forward_rule_failure(self):
        self.check_failed_rule("-I DOCKER-USER -i docker0 ! -o docker0 -d 10.0.0.0/8 -j DROP")

    def test_entrypoint_exits_on_input_rule_failure(self):
        self.check_failed_rule("-I INPUT -i docker0 -m conntrack ! --ctstate ESTABLISHED,RELATED -j DROP")

    def check_failed_rule(self, rule):
        self.install("weft-workspace-journal", """
trap 'echo stopped > "$FIXTURE/journal-stopped"; exit 0' TERM
touch "$FIXTURE/journal-ready"
# Exit if a regressed entrypoint marks the partial firewall as ready.
while [ ! -f "$FIXTURE/run/weft-iptables" ]; do sleep 0.01; done
exit 3
""")
        result = self.run_entrypoint(fail_rule=rule)
        self.assertTrue((self.root / "firewall-rejected").exists())
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("could not install firewall rules", result.stderr)
        self.assertFalse((self.root / "run/weft-iptables").exists())
        self.assertTrue((self.root / "daemon-stopped").exists())
        self.assertTrue((self.root / "journal-stopped").exists())
