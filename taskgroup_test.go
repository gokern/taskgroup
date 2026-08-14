package taskgroup_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
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

// TestTaskGroup_Run covers the structural guarantees of a run: that tasks are
// started and interrupted as promised, that panics are recovered wherever they
// happen, that interrupts run concurrently, and that a group runs once. Which
// error a run reports is not settled here. TestTaskGroup_Contract owns the
// result table, and rows belong there rather than as subtests below.
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

	t.Run("passes run context to task", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		tg := taskgroup.New()
		ctx := context.WithValue(context.Background(), contextKey{}, "value")

		// Read the value here and assert below, on the test goroutine. A
		// failing require inside a task calls FailNow there instead, which
		// kills the goroutine before it delivers its result, and the run then
		// blocks until the whole binary times out. Run waits for every task,
		// so reading got after it returns is safe.
		var got any

		tg.AddFunc(func(ctx context.Context) error {
			got = ctx.Value(contextKey{})

			return nil
		})

		require.NoError(t, tg.Run(ctx))
		require.Equal(t, "value", got)
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

		// The task runs and reports ctx.Err(), and the canceled run drops it.
		require.NoError(t, tg.Run(ctx))
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

		// The cause reaches the interrupt; the result stays clean.
		require.NoError(t, <-result)
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

// ending is how a contract case's run comes to an end: the task decides, or
// the context does. Which way the context ends is the distinction the result
// table turns on.
type ending int

const (
	// endingTaskDecides never ends the context; whatever a task returns ends
	// the run.
	endingTaskDecides ending = iota
	// endingCancel cancels once every task has started.
	endingCancel
	// endingCancelCause and endingDeadlineCause attach a reason the caller
	// wants the cleanup code to see.
	endingCancelCause
	endingDeadlineCause
	// endingDeadline expires shortly after the run begins.
	endingDeadline
	// endingCanceledUpFront and endingExpiredDeadline hand Run a context that
	// is already over. Both leave a task result and a closed Done channel ready
	// at the same moment, which is the interleaving the group has to answer the
	// same way every time.
	endingCanceledUpFront
	endingExpiredDeadline
)

// deadlineGrace is long enough that a task reliably reaches its <-ctx.Done()
// before the deadline fires, and short enough not to drag the suite out.
const deadlineGrace = 20 * time.Millisecond

// errBudgetSpent is the reason a contract case attaches to its context, to
// check that a caller-supplied cause reaches Interrupt and Defer.
var errBudgetSpent = errors.New("budget spent")

func (e ending) context() (context.Context, context.CancelFunc) {
	switch e {
	case endingTaskDecides, endingCancel:
		return context.WithCancel(context.Background())
	case endingCancelCause:
		ctx, cancel := context.WithCancelCause(context.Background())

		return ctx, func() { cancel(errBudgetSpent) }
	case endingDeadlineCause:
		return context.WithTimeoutCause(context.Background(), deadlineGrace, errBudgetSpent)
	case endingCanceledUpFront:
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		return ctx, cancel
	case endingDeadline:
		return context.WithTimeout(context.Background(), deadlineGrace)
	case endingExpiredDeadline:
		return context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	default:
		panic("taskgroup_test: unknown ending")
	}
}

// cancelsMidRun reports whether the case has to cancel by hand once the tasks
// are up, rather than handing Run a context that ends on its own.
func (e ending) cancelsMidRun() bool {
	return e == endingCancel || e == endingCancelCause
}

// racy reports whether the run starts with its context already over, so that
// the select in run has both cases ready and picks between them at random.
// Those rows are repeated: the answer has to be the same every time, and a
// single pass lands in the rarer interleaving only about once in a hundred.
func (e ending) racy() bool {
	return e == endingCanceledUpFront || e == endingExpiredDeadline
}

// racyRepeats is sized against that rate: at roughly one pass in a hundred,
// 500 of them leave a row that answers differently on the rarer interleaving
// about a 1% chance of getting through a suite run unnoticed.
const racyRepeats = 500

// raceBlockers is how many blockers the one row that needs them asks for. The
// number is empirical, not derived: with none the interleaving that row exists
// to reach never happens at all, and the odds climb with the crowd. It is the
// only handle the suite has on that interleaving, so measure detection before
// changing it rather than trusting one green run.
const raceBlockers = 32

// taskBody is the shape of a contract case's task, named once so the table
// rows stay readable. The helpers below build the bodies the rows need.
type taskBody = func(context.Context) error

func returns(err error) taskBody {
	return func(context.Context) error { return err }
}

func panics(value string) taskBody {
	return func(context.Context) error { panic(value) }
}

func blockThen(err error) taskBody {
	return func(ctx context.Context) error {
		<-ctx.Done()

		return err
	}
}

func blockThenPanic(value string) taskBody {
	return func(ctx context.Context) error {
		<-ctx.Done()

		panic(value)
	}
}

// returnsOwnCancellation hands back the exact context.Canceled sentinel from a
// context of the task's own, while the run context is still alive.
func returnsOwnCancellation() taskBody {
	return func(context.Context) error {
		inner, cancelInner := context.WithCancel(context.Background())
		cancelInner()

		return inner.Err()
	}
}

// returnsRunCancellation is the standard Go idiom, and the one this package's
// own examples use. It must not put context.Canceled back into the result
// through a task.
func returnsRunCancellation() taskBody {
	return func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}
}

// contractCase is one row of the result table. A row leaves out the fields it
// has nothing to say about — no blockers, nothing forbidden in the result —
// but always spells out wantCause and wantRun, nil included, because those two
// are the row's answer.
type contractCase struct {
	name string
	// tasks run concurrently; an empty list means a group with no tasks.
	// Where a row turns on which result reaches the channel first, the task
	// that decides the answer is listed first.
	tasks []taskBody
	// blockers are extra tasks that do nothing but wait. Spawning them
	// keeps run busy long enough for the first task's result to reach the
	// channel before the select does, which is the only way to exercise the
	// interleaving where a result wins the race against an ended context.
	blockers int
	ending   ending
	// Each list is asserted with errors.Is; an empty list means the value
	// must be nil. notWantRun is what the result must not carry.
	wantCause  []error
	wantRun    []error
	notWantRun []error
}

// contractCases spells out the result table, one entry per row.
func contractCases() []contractCase {
	errTask := errors.New("task failed")
	errTeardown := errors.New("failed while tearing down")

	return []contractCase{
		{
			name:      "a task that returns nil ends the run cleanly",
			tasks:     []taskBody{returns(nil)},
			ending:    endingTaskDecides,
			wantCause: nil,
			wantRun:   nil,
		},
		{
			name:      "a task error is the run's result",
			tasks:     []taskBody{returns(errTask)},
			ending:    endingTaskDecides,
			wantCause: []error{errTask},
			wantRun:   []error{errTask},
		},
		{
			name:      "a task's own cancellation is a failure like any other",
			tasks:     []taskBody{returnsOwnCancellation()},
			ending:    endingTaskDecides,
			wantCause: []error{context.Canceled},
			wantRun:   []error{context.Canceled},
		},
		{
			name:      "a panic that arrives first is the run's result",
			tasks:     []taskBody{panics("boom")},
			ending:    endingTaskDecides,
			wantCause: []error{taskgroup.ErrPanic},
			wantRun:   []error{taskgroup.ErrPanic},
		},
		{
			name: "a failure behind a clean finisher is dropped too",
			// The rule is about the group stopping, not about what stopped it:
			// once the first task has decided the run, the second one is being
			// torn down and reports the stop it was told to make. This is the
			// same shape as a server answering http.ErrServerClosed after a
			// signal task returns, which is the package's main example.
			tasks:      []taskBody{returns(nil), blockThen(errTeardown)},
			ending:     endingTaskDecides,
			wantCause:  nil,
			wantRun:    nil,
			notWantRun: []error{errTeardown},
		},
		{
			name:      "an explicit cancel is not a failure",
			tasks:     []taskBody{blockThen(nil)},
			ending:    endingCancel,
			wantCause: []error{context.Canceled},
			wantRun:   nil,
		},
		{
			name:       "an explicit cancel takes the teardown noise with it",
			tasks:      []taskBody{blockThen(errTeardown)},
			ending:     endingCancel,
			wantCause:  []error{context.Canceled},
			wantRun:    nil,
			notWantRun: []error{errTeardown},
		},
		{
			name: "a server closing on the way out is not a failure",
			// The shape the package promises to handle: http.ErrServerClosed is
			// the sound of a clean stop, not a reason to fail the run.
			tasks:      []taskBody{blockThen(http.ErrServerClosed)},
			ending:     endingCancel,
			wantCause:  []error{context.Canceled},
			wantRun:    nil,
			notWantRun: []error{http.ErrServerClosed},
		},
		{
			name:       "a task handing back the run's own cancellation is dropped",
			tasks:      []taskBody{returnsRunCancellation()},
			ending:     endingCancel,
			wantCause:  []error{context.Canceled},
			wantRun:    nil,
			notWantRun: []error{context.Canceled},
		},
		{
			name:       "a panic during an explicit cancel survives the cancel",
			tasks:      []taskBody{blockThenPanic("boom while stopping")},
			ending:     endingCancel,
			wantCause:  []error{context.Canceled},
			wantRun:    []error{taskgroup.ErrPanic},
			notWantRun: []error{context.Canceled},
		},
		{
			name:       "a deadline is the run's result, and takes the teardown noise with it",
			tasks:      []taskBody{blockThen(errTeardown)},
			ending:     endingDeadline,
			wantCause:  []error{context.DeadlineExceeded},
			wantRun:    []error{context.DeadlineExceeded},
			notWantRun: []error{errTeardown},
		},
		{
			name:      "a panic after a deadline is joined onto it",
			tasks:     []taskBody{blockThenPanic("boom after deadline")},
			ending:    endingDeadline,
			wantCause: []error{context.DeadlineExceeded},
			wantRun:   []error{context.DeadlineExceeded, taskgroup.ErrPanic},
		},
		{
			name: "a cancel already in hand is answered the same however the select races",
			// The failing task is listed first on purpose. The result that wins
			// the race is the one that reached the channel first, which is near
			// enough always the first task's, so putting the clean task there
			// hides the race behind a nil that looks correct.
			tasks:      []taskBody{blockThen(errTeardown), blockThen(nil)},
			ending:     endingCanceledUpFront,
			wantCause:  []error{context.Canceled},
			wantRun:    nil,
			notWantRun: []error{errTeardown},
		},
		{
			name:       "a deadline already past is answered the same however the select races",
			tasks:      []taskBody{blockThen(errTeardown), blockThen(nil)},
			ending:     endingExpiredDeadline,
			wantCause:  []error{context.DeadlineExceeded},
			wantRun:    []error{context.DeadlineExceeded},
			notWantRun: []error{errTeardown},
		},
		{
			name: "a panic winning the race against a cancel is still joined on",
			// The panicking result may be the one the select pulls off the
			// channel. It stops being the group's verdict once the run is
			// canceled, so it has to rejoin the others rather than be dropped
			// with the ordinary errors.
			tasks:      []taskBody{blockThenPanic("boom in the race")},
			blockers:   raceBlockers,
			ending:     endingCanceledUpFront,
			wantCause:  []error{context.Canceled},
			wantRun:    []error{taskgroup.ErrPanic},
			notWantRun: []error{context.Canceled},
		},
		{
			name: "a cancel's reason is reported, the cancellation is not",
			// Attaching a reason is the caller going out of its way to send
			// something, so it comes back. The bare sentinel does not travel
			// with it: this run failed, and errors.Is against context.Canceled
			// must not read it as a clean stop.
			tasks:      []taskBody{blockThen(nil)},
			ending:     endingCancelCause,
			wantCause:  []error{context.Canceled, errBudgetSpent},
			wantRun:    []error{errBudgetSpent},
			notWantRun: []error{context.Canceled},
		},
		{
			name: "a deadline's reason is carried into the result",
			// The deadline is news, and so is the reason attached to it.
			tasks:     []taskBody{blockThen(nil)},
			ending:    endingDeadlineCause,
			wantCause: []error{context.DeadlineExceeded, errBudgetSpent},
			wantRun:   []error{context.DeadlineExceeded, errBudgetSpent},
		},
		{
			name: "a group with no tasks is clean even past its deadline",
			// The early return for an empty group, which never reaches the
			// context at all: the one corner where a dead deadline is not the
			// result.
			tasks:     nil,
			ending:    endingExpiredDeadline,
			wantCause: nil,
			wantRun:   nil,
		},
	}
}

// TestTaskGroup_Contract pins the whole result table in one place: how a run
// ended, what Interrupt and Defer are told, and what Run returns. The rows are
// worth reading against each other: "explicit cancel" and "a task's own
// cancellation" carry the identical error value and end differently, because
// what the group looks at is whether its own context ended, never what an error
// happens to look like.
func TestTaskGroup_Contract(t *testing.T) {
	t.Parallel()

	for _, testCase := range contractCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runContractCase(t, testCase)
		})
	}
}

// runContractCase drives one row. A racy row is run repeatedly, because the
// answer it pins has to be the same on every interleaving.
func runContractCase(t *testing.T, testCase contractCase) {
	t.Helper()

	repeats := 1
	if testCase.ending.racy() {
		repeats = racyRepeats
	}

	for iteration := range repeats {
		runContractCaseOnce(t, testCase, iterationLabel(iteration, repeats))
	}
}

// iterationLabel says which repeat a failure came from, which is the difference
// between a row that is wrong on every interleaving and one that is wrong only
// on the rare one. A row that runs once has nothing to say.
func iterationLabel(iteration, repeats int) string {
	if repeats == 1 {
		return ""
	}

	return fmt.Sprintf(" (iteration %d of %d)", iteration+1, repeats)
}

// runContractCaseOnce performs one run of a row and checks every value it pins.
func runContractCaseOnce(t *testing.T, testCase contractCase, label string) {
	t.Helper()

	ctx, cancel := testCase.ending.context()
	defer cancel()

	bodies := slices.Clone(testCase.tasks)
	for range testCase.blockers {
		bodies = append(bodies, blockThen(nil))
	}

	// Interrupts run concurrently on their own goroutines and Defer runs inside
	// Run on another, so both causes travel back over channels rather than
	// plain variables. Receiving from them is also how the row checks that each
	// hook ran at all.
	interruptCause := make(chan error, len(bodies))
	deferCause := make(chan error, 1)

	// A row that cancels by hand must not cancel before every task is up, or it
	// collapses into the already-over case the racy rows exist to cover.
	var started sync.WaitGroup

	started.Add(len(bodies))

	tg := taskgroup.New()

	tg.Defer(func(err error) error {
		deferCause <- err

		return nil
	})

	for _, body := range bodies {
		tg.Add(taskgroup.NewTask(func(ctx context.Context) error {
			started.Done()

			return body(ctx)
		}).Interrupt(func(err error) {
			interruptCause <- err
		}))
	}

	result := make(chan error, 1)
	go func() { result <- tg.Run(ctx) }()

	started.Wait()

	if testCase.ending.cancelsMidRun() {
		cancel()
	}

	err := <-result

	requireErrors(t, "run result"+label, err, testCase.wantRun)
	requireErrors(t, "defer cause"+label, <-deferCause, testCase.wantCause)

	for range bodies {
		requireErrors(t, "interrupt cause"+label, <-interruptCause, testCase.wantCause)
	}

	for _, unwanted := range testCase.notWantRun {
		require.NotErrorIsf(t, err, unwanted,
			"%v should not have survived into the result%s", unwanted, label,
		)
	}
}

// requireErrors asserts that err carries every error in want, or is nil when
// want is empty.
func requireErrors(t *testing.T, subject string, err error, want []error) {
	t.Helper()

	if len(want) == 0 {
		require.NoErrorf(t, err, "%s should be nil", subject)

		return
	}

	for _, target := range want {
		require.ErrorIsf(t, err, target, "%s should carry %v", subject, target)
	}
}

// TestTaskGroup_SuppressionKeepsJoinedErrors guards the seam the result table
// cannot reach: suppressing the cause must empty out the primary error and
// nothing else. Interrupt panics, defer errors and defer panics all still have
// to come back from a run whose own answer is nil.
func TestTaskGroup_SuppressionKeepsJoinedErrors(t *testing.T) {
	t.Parallel()

	t.Run("an interrupt panic survives a suppressed cancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})

		tg := taskgroup.New()

		tg.Add(taskgroup.NewTask(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()

			return nil
		}).Interrupt(func(error) {
			panic("interrupt blew up")
		}))

		result := make(chan error, 1)
		go func() { result <- tg.Run(ctx) }()

		<-started
		cancel()

		err := <-result
		require.ErrorIs(t, err, taskgroup.ErrPanic)
		require.Contains(t, err.Error(), "interrupt blew up")
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("a defer error survives a suppressed cancel", func(t *testing.T) {
		t.Parallel()

		cleanupErr := errors.New("could not flush the journal")

		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})

		tg := taskgroup.New()

		tg.Defer(func(error) error { return cleanupErr })

		tg.AddFunc(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()

			return nil
		})

		result := make(chan error, 1)
		go func() { result <- tg.Run(ctx) }()

		<-started
		cancel()

		err := <-result
		require.ErrorIs(t, err, cleanupErr)
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("a defer panic survives a suppressed cancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})

		tg := taskgroup.New()

		tg.Defer(func(error) error { panic("defer blew up") })

		tg.AddFunc(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()

			return nil
		})

		result := make(chan error, 1)
		go func() { result <- tg.Run(ctx) }()

		<-started
		cancel()

		err := <-result
		require.ErrorIs(t, err, taskgroup.ErrPanic)
		require.Contains(t, err.Error(), "defer blew up")
	})
}
