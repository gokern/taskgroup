package taskgroup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

func TestTaskGroup_Defer(t *testing.T) {
	t.Parallel()

	t.Run("nil defer function", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.Panics(t, func() {
			tg.Defer(nil)
		})
	})

	t.Run("defer after run", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()

		require.NoError(t, tg.Run(context.Background()))
		require.Panics(t, func() {
			tg.Defer(func(error) error { return nil })
		})
	})

	t.Run("runs without tasks", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		ran := false

		tg.Defer(func(err error) error {
			require.NoError(t, err)
			ran = true
			return nil
		})

		require.NoError(t, tg.Run(context.Background()))
		require.True(t, ran)
	})

	t.Run("runs in lifo order", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		var order []int

		tg.Defer(func(error) error {
			order = append(order, 1)
			return nil
		})
		tg.Defer(func(error) error {
			order = append(order, 2)
			return nil
		})
		tg.Defer(func(error) error {
			order = append(order, 3)
			return nil
		})

		require.NoError(t, tg.Run(context.Background()))
		require.Equal(t, []int{3, 2, 1}, order)
	})

	t.Run("runs after all tasks complete", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		taskDone := make(chan struct{})
		interrupted := make(chan struct{})
		expectedErr := errors.New("stop")

		tg.Add(func(context.Context) error {
			<-interrupted
			close(taskDone)
			return nil
		}).Interrupt(func(error) {
			close(interrupted)
		})

		tg.Add(func(context.Context) error {
			return expectedErr
		})

		tg.Defer(func(error) error {
			select {
			case <-taskDone:
			default:
				t.Fatal("defer ran before all tasks completed")
			}
			return nil
		})

		require.ErrorIs(t, tg.Run(context.Background()), expectedErr)
	})

	t.Run("receives primary error", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		expectedErr := errors.New("primary")
		var got error

		tg.Add(func(context.Context) error {
			return expectedErr
		})

		tg.Defer(func(err error) error {
			got = err
			return nil
		})

		require.ErrorIs(t, tg.Run(context.Background()), expectedErr)
		require.ErrorIs(t, got, expectedErr)
	})

	t.Run("joins defer errors with primary error", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		primaryErr := errors.New("primary")
		deferErr := errors.New("defer")

		tg.Add(func(context.Context) error {
			return primaryErr
		})

		tg.Defer(func(error) error {
			return deferErr
		})

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, primaryErr)
		require.ErrorIs(t, err, deferErr)
	})

	t.Run("joins defer panic and continues running defers", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		panicErr := errors.New("defer panic")
		var ran bool

		tg.Defer(func(error) error {
			ran = true
			return nil
		})
		tg.Defer(func(error) error {
			panic(panicErr)
		})

		err := tg.Run(context.Background())
		require.ErrorIs(t, err, panicErr)
		require.True(t, ran)
	})

	t.Run("returns defer error when primary error is nil", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		deferErr := errors.New("defer")

		tg.Add(func(context.Context) error {
			return nil
		})

		tg.Defer(func(error) error {
			return deferErr
		})

		require.ErrorIs(t, tg.Run(context.Background()), deferErr)
	})
}
