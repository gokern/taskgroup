package taskgroup

import (
	"context"
	"os"
	"os/signal"
)

// SignalTask returns a task that exits when the context is canceled or a
// signal is received.
//
// When no signals are provided, SignalTask listens for the platform's standard
// shutdown signals.
//
// On signal the task returns an opaque error. Detect it with IsSignalError,
// and extract the concrete os.Signal with SignalFromError.
func SignalTask(signals ...os.Signal) Task {
	if len(signals) == 0 {
		signals = defaultSignals()
	} else {
		signals = append([]os.Signal(nil), signals...)
	}

	return NewTask(func(ctx context.Context) error {
		sig := make(chan os.Signal, 1)

		signal.Notify(sig, signals...)
		defer signal.Stop(sig)

		select {
		case s := <-sig:
			return signalError{s}
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}
