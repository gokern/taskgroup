//go:build unix

package taskgroup_test

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

// TestSignalTaskCopiesSignals guards a line that can otherwise be deleted with a
// green suite: SignalTask copies the slice it is given.
//
// The copy is load-bearing because the task body reads the slice when it runs,
// not when SignalTask is called, so an aliased slice stays writable for the
// whole life of the task. Without the copy the task listens for the signal the
// caller wrote afterwards, never wakes, and this fails on the context deadline
// below rather than on the signal.
//
// SIGUSR1 and SIGUSR2 are used because nothing else in the suite touches them,
// and the test installs its own handler first so the default action, which kills
// the process, cannot fire in the window before the task calls signal.Notify.
func TestSignalTaskCopiesSignals(t *testing.T) {
	t.Parallel()

	guard := make(chan os.Signal, 1)

	signal.Notify(guard, syscall.SIGUSR1)
	defer signal.Stop(guard)

	requested := []os.Signal{syscall.SIGUSR1}

	tg := taskgroup.New()
	tg.Add(taskgroup.SignalTask(requested...))

	// The caller reuses its slice, as it is entitled to.
	requested[0] = syscall.SIGUSR2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := make(chan error, 1)
	go func() { result <- tg.Run(ctx) }()

	// Resent on a ticker because the task installs its own handler somewhere
	// inside Run, and a signal delivered before that is simply not seen.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var err error

	for waiting := true; waiting; {
		select {
		case err = <-result:
			waiting = false
		case <-ticker.C:
			require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
		}
	}

	sig, ok := taskgroup.SignalFromError(err)
	require.True(t, ok, "the task should have woken on the signal it was asked for, got %v", err)
	require.Equal(t, syscall.SIGUSR1, sig)
}
