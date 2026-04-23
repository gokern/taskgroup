package taskgroup

import (
	"errors"
	"os"
)

// Interrupt is the signal error for os.Interrupt.
var Interrupt = signalError{os.Interrupt}

var _ os.Signal = signalError{}

type signalError struct {
	sig os.Signal
}

func (err signalError) String() string {
	return err.sig.String()
}

func (err signalError) Signal() {}

func (err signalError) Error() string {
	return err.sig.String()
}

// IsSignalError reports whether err contains an error returned by Signal.
func IsSignalError(err error) bool {
	_, ok := errors.AsType[signalError](err)

	return ok
}

// SignalFromError returns the signal contained in err, and reports whether
// one was found. If err does not wrap a signal error, it returns nil, false.
func SignalFromError(err error) (os.Signal, bool) {
	if signalErr, ok := errors.AsType[signalError](err); ok {
		return signalErr.sig, true
	}

	return nil, false
}
