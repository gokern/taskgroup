package taskgroup

import (
	"context"
	"os"
	"os/signal"
	"slices"
)

// SignalTask returns a task that exits when the context is canceled or a
// signal is received.
//
// When no signals are provided, SignalTask listens for the platform's standard
// shutdown signals. The slice is copied, so the caller may reuse it.
//
// On signal the task returns an opaque error. Detect it with IsSignalError
// and extract the signal with SignalFromError. On cancellation it returns
// ctx.Err(), which a canceled Run drops rather than reports.
//
// Signals delivered before the task body runs get the OS default action.
// Programs that need to catch signals at startup should call signal.Notify
// or signal.Ignore before Run.
func SignalTask(signals ...os.Signal) Task {
	if len(signals) == 0 {
		signals = defaultSignals()
	} else {
		// The task body reads signals when it runs, not now, so it must not
		// keep the caller's slice: a later write to it would change which
		// signals the task notifies on. defaultSignals returns a fresh slice.
		signals = slices.Clone(signals)
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
