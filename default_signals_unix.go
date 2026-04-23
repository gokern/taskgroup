//go:build !windows

package taskgroup

import (
	"os"
	"syscall"
)

func defaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
