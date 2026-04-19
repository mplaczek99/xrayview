//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

func terminateProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		return command.Process.Kill()
	}

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(sidecarShutdownTimeout):
		return command.Process.Kill()
	}
}
