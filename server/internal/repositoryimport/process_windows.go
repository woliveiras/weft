//go:build windows

package repositoryimport

import "os/exec"

func isolateProcess(cmd *exec.Cmd) {}
