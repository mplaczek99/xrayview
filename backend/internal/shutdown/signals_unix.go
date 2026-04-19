//go:build !windows

package shutdown

import (
	"os"
	"syscall"
)

func Signals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
