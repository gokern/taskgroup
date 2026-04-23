package taskgroup

import "os"

// NewSignalError constructs a signal error for tests in the _test package.
func NewSignalError(sig os.Signal) error {
	return signalError{sig}
}
