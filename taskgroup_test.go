package taskgroup_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

func TestTaskGroup_Add(t *testing.T) {
	t.Parallel()

	t.Run("zero task", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.PanicsWithValue(t,
			"taskgroup: uninitialized Task (use NewTask)",
			func() { tg.Add(taskgroup.Task{}) },
		)
	})

	t.Run("nil execute function", func(t *testing.T) {
		t.Parallel()

		require.PanicsWithValue(t,
			"taskgroup: nil execute function",
			func() { taskgroup.NewTask(nil) },
		)
	})

	t.Run("add func", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		expectedErr := errors.New("task error")

		tg.AddFunc(func(context.Context) error {
			return expectedErr
		})

		require.ErrorIs(t, tg.Run(context.Background()), expectedErr)
	})

	t.Run("add nil func", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.PanicsWithValue(t,
			"taskgroup: nil execute function",
			func() { tg.AddFunc(nil) },
		)
	})

	t.Run("nil interrupt function", func(t *testing.T) {
		t.Parallel()

		task := taskgroup.NewTask(func(context.Context) error { return nil })

		require.PanicsWithValue(t,
			"taskgroup: nil interrupt function",
			func() { task.Interrupt(nil) },
		)
	})

	t.Run("add after run", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.NoError(t, tg.Run(context.Background()))
		require.PanicsWithValue(t,
			"taskgroup: TaskGroup already started",
			func() {
				tg.Add(taskgroup.NewTask(func(context.Context) error { return nil }))
			},
		)
	})

	t.Run("add func after run", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.NoError(t, tg.Run(context.Background()))
		require.PanicsWithValue(t,
			"taskgroup: TaskGroup already started",
			func() { tg.AddFunc(func(context.Context) error { return nil }) },
		)
	})
}

func TestTaskGroup_Run(t *testing.T) {
	t.Parallel()

	t.Run("no tasks", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.NoError(t, tg.Run(context.Background()))
	})

	t.Run("nil context", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.PanicsWithValue(t,
			"taskgroup: nil context",
			func() { _ = tg.Run(nil) },
		)
	})

	t.Run("returns first task error", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		expectedErr := errors.New("task error")

		tg.AddFunc(func(context.Context) error {
			return expectedErr
		})

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("passes run context to task", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		tg := taskgroup.New()
		ctx := context.WithValue(context.Background(), contextKey{}, "value")

		tg.AddFunc(func(ctx context.Context) error {
			require.Equal(t, "value", ctx.Value(contextKey{}))

			return nil
		})

		require.NoError(t, tg.Run(ctx))
	})

	t.Run("starts tasks with already cancelled context", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ran := false

		tg.AddFunc(func(ctx context.Context) error {
			ran = true

			return ctx.Err()
		})

		err := tg.Run(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, ran)
	})

	t.Run("context cancellation interrupts tasks", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		interrupted := make(chan error, 1)
		result := make(chan error, 1)

		tg.Add(taskgroup.NewTask(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()

			return ctx.Err()
		}).Interrupt(func(err error) {
			interrupted <- err
		}))

		go func() {
			result <- tg.Run(ctx)
		}()

		<-started
		cancel()

		err := <-result
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, <-interrupted, context.Canceled)
	})

	t.Run("interrupts other tasks when one returns", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		interrupted := make(chan struct{})
		expectedErr := errors.New("returning early")

		tg.Add(taskgroup.NewTask(func(context.Context) error {
			<-interrupted

			return nil
		}).Interrupt(func(error) {
			close(interrupted)
		}))

		tg.AddFunc(func(context.Context) error {
			return expectedErr
		})

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("handles panic in task", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		tg.AddFunc(func(context.Context) error {
			panic("test panic")
		})

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, taskgroup.ErrPanic)
		require.Contains(t, err.Error(), "test panic")
	})

	t.Run("joins task panic after shutdown starts", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		interrupted := make(chan struct{})
		primaryErr := errors.New("primary")
		panicErr := errors.New("secondary panic")

		tg.Add(taskgroup.NewTask(func(context.Context) error {
			<-interrupted
			panic(panicErr)
		}).Interrupt(func(error) {
			close(interrupted)
		}))

		tg.AddFunc(func(context.Context) error {
			return primaryErr
		})

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, primaryErr)
		require.ErrorIs(t, err, panicErr)
		require.ErrorIs(t, err, taskgroup.ErrPanic)
	})

	t.Run("joins interrupt panic with primary error", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		primaryErr := errors.New("primary")
		interruptErr := errors.New("interrupt")

		tg.Add(taskgroup.NewTask(func(context.Context) error {
			return primaryErr
		}).Interrupt(func(error) {
			panic(interruptErr)
		}))

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, primaryErr)
		require.ErrorIs(t, err, interruptErr)
		require.ErrorIs(t, err, taskgroup.ErrPanic)
	})

	t.Run("runs interrupts concurrently", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		interruptStarted := make(chan struct{}, 2)
		releaseInterrupts := make(chan struct{})
		primaryErr := errors.New("primary")

		for range 2 {
			tg.Add(taskgroup.NewTask(func(ctx context.Context) error {
				<-ctx.Done()

				return nil
			}).Interrupt(func(error) {
				interruptStarted <- struct{}{}

				<-releaseInterrupts
			}))
		}

		tg.AddFunc(func(context.Context) error {
			return primaryErr
		})

		result := make(chan error, 1)

		go func() {
			result <- tg.Run(context.Background())
		}()

		for range 2 {
			select {
			case <-interruptStarted:
			case <-time.After(time.Second):
				close(releaseInterrupts)
				t.Fatal("interrupt functions did not start concurrently")
			}
		}

		close(releaseInterrupts)
		require.ErrorIs(t, <-result, primaryErr)
	})

	t.Run("runs only once", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.NoError(t, tg.Run(context.Background()))
		require.PanicsWithValue(t,
			"taskgroup: TaskGroup already started",
			func() { _ = tg.Run(context.Background()) },
		)
	})
}
