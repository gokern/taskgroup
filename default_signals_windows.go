//go:build windows

package taskgroup

import "os"

func defaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
