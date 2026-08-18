package taskgroup_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/gokern/panics"
	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

func TestNewTask(t *testing.T) {
	t.Parallel()

	t.Run("nil execute function", func(t *testing.T) {
		t.Parallel()

		require.PanicsWithValue(t,
			"taskgroup: nil execute function",
			func() { taskgroup.NewTask(nil) },
		)
	})
}

func TestTask_Interrupt(t *testing.T) {
	t.Parallel()

	t.Run("nil interrupt function", func(t *testing.T) {
		t.Parallel()

		task := taskgroup.NewTask(func(context.Context) error { return nil })

		require.PanicsWithValue(t,
			"taskgroup: nil interrupt function",
			func() { task.Interrupt(nil) },
		)
	})
}

// The package recovers at three separate points, one per hook kind, so each one
// needs its own proof that the stack comes back out of Run.
func TestRun_panicCarriesTheStack(t *testing.T) {
	t.Parallel()

	t.Run("raised by a task", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		tg.AddFunc(func(context.Context) error {
			raiseInTask()

			return nil
		})

		requirePanicSite(t, tg.Run(t.Context()), ".raiseInTask")
	})

	t.Run("raised by an interrupt", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		tg.Add(taskgroup.NewTask(func(context.Context) error { return nil }).
			Interrupt(func(error) { raiseInInterrupt() }))

		requirePanicSite(t, tg.Run(t.Context()), ".raiseInInterrupt")
	})

	t.Run("raised by a defer", func(t *testing.T) {
		t.Parallel()

		tg := taskgroup.New()
		tg.AddFunc(func(context.Context) error { return nil })
		tg.Defer(func(error) error {
			raiseInDefer()

			return nil
		})

		requirePanicSite(t, tg.Run(t.Context()), ".raiseInDefer")
	})
}

// requirePanicSite asserts that err reports a recovered panic whose stack starts
// at the named function.
func requirePanicSite(t *testing.T, err error, site string) {
	t.Helper()

	require.ErrorIs(t, err, panics.ErrPanic, "the shared sentinel must hold")

	p, ok := panics.As(err)
	require.True(t, ok, "a recovered panic must reach the caller as a *panics.Panic")

	frame, _ := runtime.CallersFrames(p.StackTrace()).Next()
	require.True(t, strings.HasSuffix(frame.Function, site),
		"taskgroup must report where the panic happened, want %s, got %q", site, frame.Function)
}

// TestRun_panicValueRendering pins how a panic value reaches the caller: a value
// that is neither a string nor an error is rendered with %#v, so the concrete
// type and the field names survive into the message.
//
// The rendering lives in the dependency, which the mutation harness cannot patch,
// so this assertion is its only guard. Every other panic in the suite is a string
// or an error, and those reach the message by their own route: nothing else here
// goes through %#v.
func TestRun_panicValueRendering(t *testing.T) {
	t.Parallel()

	tg := taskgroup.New()
	tg.AddFunc(func(context.Context) error { panic(payload{Attempt: 2}) })

	err := tg.Run(t.Context())

	require.ErrorIs(t, err, panics.ErrPanic)
	require.ErrorContains(t, err, "panic: taskgroup_test.payload{Attempt:2}")
}

type payload struct{ Attempt int }

//go:noinline
func raiseInTask() { panic("boom") }

//go:noinline
func raiseInInterrupt() { panic("boom in an interrupt") }

//go:noinline
func raiseInDefer() { panic("boom in a defer") }
