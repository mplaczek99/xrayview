//go:build windows

package shutdown

import "os"

func Signals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
