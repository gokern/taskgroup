//go:build !windows

package taskgroup_test

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

func TestSignalReceived(t *testing.T) { //nolint:paralleltest
	tg := taskgroup.New()
	tg.Add(taskgroup.SignalTask(syscall.SIGUSR1))

	go func() {
		time.Sleep(100 * time.Millisecond)

		_ = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
	}()

	require.True(t, taskgroup.IsSignalError(tg.Run(t.Context())))
}

func TestSignalCopiesSignals(t *testing.T) { //nolint:paralleltest
	signals := []os.Signal{syscall.SIGUSR1}
	task := taskgroup.SignalTask(signals...)
	signals[0] = syscall.SIGUSR2

	tg := taskgroup.New()
	tg.Add(task)

	go func() {
		time.Sleep(100 * time.Millisecond)

		_ = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
	}()

	require.True(t, taskgroup.IsSignalError(tg.Run(t.Context())))
}
