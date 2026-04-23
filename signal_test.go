package taskgroup_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

func TestIsSignalError(t *testing.T) {
	t.Parallel()

	t.Run("when signal error", func(t *testing.T) {
		t.Parallel()

		require.True(t, taskgroup.IsSignalError(taskgroup.Interrupt))
	})

	t.Run("when wrapped signal error", func(t *testing.T) {
		t.Parallel()

		require.True(t, taskgroup.IsSignalError(fmt.Errorf("wrapped: %w", taskgroup.Interrupt)))
	})

	t.Run("when there is no signal error", func(t *testing.T) {
		t.Parallel()

		require.False(t, taskgroup.IsSignalError(errors.New("not a signal error")))
	})
}

func TestSignal(t *testing.T) {
	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := taskgroup.Signal()(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestSignalFromError(t *testing.T) {
	t.Parallel()

	t.Run("when signal error", func(t *testing.T) {
		t.Parallel()

		sig, ok := taskgroup.SignalFromError(taskgroup.Interrupt)
		require.True(t, ok)
		require.Equal(t, os.Interrupt, sig)
	})

	t.Run("when wrapped signal error", func(t *testing.T) {
		t.Parallel()

		sig, ok := taskgroup.SignalFromError(fmt.Errorf("wrapped: %w", taskgroup.Interrupt))
		require.True(t, ok)
		require.Equal(t, os.Interrupt, sig)
	})

	t.Run("when there is no signal error", func(t *testing.T) {
		t.Parallel()

		sig, ok := taskgroup.SignalFromError(errors.New("not a signal error"))
		require.False(t, ok)
		require.Nil(t, sig)
	})
}
