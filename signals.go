package taskgroup

import (
	"context"
	"os"
	"os/signal"
)

// Signal returns a task that exits when the context is canceled or a signal is received.
//
// When no signals are provided, Signal listens for the platform's standard
// shutdown signals.
func Signal(signals ...os.Signal) ExecuteFunc {
	if len(signals) == 0 {
		signals = defaultSignals()
	} else {
		signals = append([]os.Signal(nil), signals...)
	}

	return func(ctx context.Context) error {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, signals...)
		defer signal.Stop(sig)

		select {
		case s := <-sig:
			return signalError{s}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
