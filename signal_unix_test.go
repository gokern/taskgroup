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

func TestSignalReceived(t *testing.T) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
	}()

	err := taskgroup.Signal(syscall.SIGUSR1)(t.Context())
	require.True(t, taskgroup.IsSignalError(err))
}

func TestSignalCopiesSignals(t *testing.T) {
	signals := []os.Signal{syscall.SIGUSR1}
	execute := taskgroup.Signal(signals...)
	signals[0] = syscall.SIGUSR2

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
	}()

	err := execute(t.Context())
	require.True(t, taskgroup.IsSignalError(err))
}
