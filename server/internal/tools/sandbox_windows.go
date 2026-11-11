//go:build windows

package tools

import "os/exec"

// Windows has no process groups in the POSIX sense; killing the direct child
// is the portable fallback. Production deployments use the docker sandbox.
func setProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
