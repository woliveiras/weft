//go:build windows

package mcpconfig

import "os/exec"

func prepareMCPProcess(*exec.Cmd) error {
	return discoveryError("local MCP process lifecycle is not qualified on Windows")
}
