//go:build windows

package main

import "os/exec"

func terminateProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	return command.Process.Kill()
}
