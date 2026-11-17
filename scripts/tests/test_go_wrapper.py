"""Unit checks for wrapper dispatch; make test supplies real Docker integration."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class GoWrapperTests(unittest.TestCase):
    def invoke(self, args, fail=False):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            docker = path / "docker"
            docker.write_text(
                "#!/usr/bin/env python3\n"
                "import json, os, sys\n"
                "with open(os.environ['WRAPPER_LOG'], 'a') as f: f.write(json.dumps(sys.argv[1:])+'\\n')\n"
                "if sys.argv[1] == 'build':\n"
                " if os.environ.get('WRAPPER_FAIL'): sys.exit(19)\n"
                " print('sha256:test-toolchain')\n"
            )
            docker.chmod(0o700)
            log = path / "calls"
            env = dict(os.environ, PATH=tmp + os.pathsep + os.environ["PATH"],
                       WRAPPER_LOG=str(log), GO_IMAGE="golang:1.26-alpine")
            if fail:
                env["WRAPPER_FAIL"] = "1"
            result = subprocess.run([str(ROOT / "scripts/go.sh"), *args], env=env,
                                    capture_output=True, text=True)
            return result, [json.loads(line) for line in log.read_text().splitlines()]

    def test_test_image_and_arguments(self):
        result, calls = self.invoke(["test", "-run", "TestOne|TestTwo", "./..."])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls[0][0], "build")
        self.assertIn("GO_IMAGE=golang:1.26-alpine", calls[0])
        self.assertEqual(calls[1][-6:], ["sha256:test-toolchain", "go", "test", "-run", "TestOne|TestTwo", "./..."])

    def test_build_keeps_base_image(self):
        result, calls = self.invoke(["build", "./cmd/weftd"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][-4:], ["golang:1.26-alpine", "go", "build", "./cmd/weftd"])

    def test_image_failure_does_not_run_tests(self):
        result, calls = self.invoke(["test", "./..."], fail=True)
        self.assertEqual(result.returncode, 19)
        self.assertEqual(len(calls), 1)
