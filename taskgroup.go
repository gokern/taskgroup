package taskgroup

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// TaskGroup manages a collection of concurrent tasks.
//
// A TaskGroup is run once. When the first task returns, or when the run context
// ends, the group interrupts every task concurrently, waits for all tasks to
// return, then runs deferred cleanup functions in last-in-first-out order.
type TaskGroup struct {
	mu      sync.Mutex
	started bool

	tasks  []Task
	defers []DeferFunc
}

// New creates an empty TaskGroup.
func New() *TaskGroup {
	return new(TaskGroup)
}

// Add appends a task to the TaskGroup.
//
// Add panics on an uninitialized Task; use NewTask (or helpers like SignalTask)
// to construct one.
func (g *TaskGroup) Add(task Task) {
	if task.execute == nil {
		panic("taskgroup: uninitialized Task (use NewTask)")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.tasks = append(g.tasks, task)
}

// AddFunc appends a task created from execute to the TaskGroup.
func (g *TaskGroup) AddFunc(execute ExecuteFunc) {
	g.Add(NewTask(execute))
}

// Run executes all tasks in the group. The first task to return ends the run,
// as does the run context ending.
//
// Two values come out of a run. Interrupt and Defer functions receive the cause:
// whatever stopped the group. Run itself returns the result.
//
//	how the run ended                    cause              Run returns
//	-------------------------------------------------------------------------
//	a task returned nil first            nil                the cause
//	a task returned an error first       that error         the cause
//	a task panicked first                wrapped panic      the cause
//	the run context was canceled         context.Canceled   nil
//	the same, with a reason attached     Canceled+reason    the reason
//	the run context's deadline expired   DeadlineExceeded   the cause
//	a task's own Canceled, run context
//	still alive                          that error         the cause
//
// Run subtracts one sentinel from the cause and reports the rest. A bare
// context.Canceled is the caller's own doing and tells it nothing, so it comes
// back as nil. Everything else survives: a deadline, because setting a budget
// tells you nothing about whether the budget blew, and a reason passed to a
// CancelCauseFunc, because attaching one is the caller going out of its way to
// send something. Cancel with nil to say "stop, no reason" and get nil back.
//
// The bare sentinel never travels with a reason, so errors.Is against
// context.Canceled stays a reliable "this run stopped cleanly" test. Use
// errors.Is(err, context.DeadlineExceeded) for a blown budget.
//
// Whenever Run returns non-nil it returns a joined error, even when only one
// thing went wrong, so match it with errors.Is or errors.As. Run never returns a
// task's error value itself; == will not find it.
//
// What gets suppressed depends on the run's own context ending, not on the shape
// of the error, so a task that returns context.Canceled from a context of its
// own is reported like any other failure. A group handed a context that is
// already over gives the same answer every time, however the select races.
//
// Under context.WithCancelCause or context.WithTimeoutCause the cause handed to
// the hooks is the caller's reason joined with ctx.Err(), so errors.Is finds
// either. What Run returns is that pair minus a bare cancellation: the reason
// alone after a cancel, the pair intact after a deadline.
//
// Ordinary task errors are dropped once the group is stopping. A task being torn
// down reports the stop it was told to make, which is indistinguishable from a
// real failure; dropping them is what keeps http.ErrServerClosed out of the
// result. This applies however the run ended, so a run whose first task finished
// cleanly returns nil even if a second task then failed. A cancellation landing
// while a task is already failing takes that failure with it for the same
// reason. A task that knows one of its own failures is worth hearing about
// should report it where it happens instead of relying on Run to carry it.
//
// Panics are never dropped. They are recovered, wrapped so
// errors.Is(err, ErrPanic) holds, and joined onto the result. The wrapper is
// what marks a panic, not how the task ended, so an error already carrying
// ErrPanic survives shutdown and a nested group's panic stays visible in the
// group that ran it. Panics out of Interrupt functions, and errors or panics out
// of Defer functions, are joined on as well. An InterruptFunc returns nothing,
// so a panic is all it can contribute.
//
// A group with no tasks has no cause and no result of its own; the table does
// not apply, its Defer functions see a nil cause, and Run returns only what that
// cleanup contributes.
//
// Tasks are started even when ctx is already canceled. Each task is handed a
// cancelable child of ctx.
func (g *TaskGroup) Run(ctx context.Context) error {
	if ctx == nil {
		panic("taskgroup: nil context")
	}

	tasks, defers := g.start()

	panicErrs, cause, primary := run(ctx, tasks)

	deferErrs := runDefers(defers, cause)

	return joinErrors(primary, panicErrs, deferErrs)
}

func (g *TaskGroup) start() ([]Task, []DeferFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.started = true

	tasks := slices.Clone(g.tasks)
	defers := slices.Clone(g.defers)

	return tasks, defers
}

func (g *TaskGroup) mustNotHaveStarted() {
	if g.started {
		panic("taskgroup: TaskGroup already started")
	}
}

// run starts every task under a cancelable child of parent and returns the
// panics to join onto the result, why the group stopped, and how much of that
// Run should report.
//
// Canceling that child is how the group tells its tasks to stop, so run owns it
// from derivation to cancellation. Only panics come back in the first result:
// every other outcome is either the cause or dropped as shutdown noise.
func run(parent context.Context, tasks []Task) ([]error, error, error) {
	if len(tasks) == 0 {
		return nil, nil, nil
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	results := make(chan error, len(tasks))

	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Go(func() {
			results <- recoverError(func() error { return task.execute(ctx) })
		})
	}

	var first error

	select {
	case first = <-results:
	case <-ctx.Done():
	}

	// Whether the run context ended is a fact about ctx, not about which select
	// case won: when a result and a closed Done channel are both ready, the
	// runtime picks between them at random. Read before cancel(), which would
	// make ctx.Err() non-nil on every run.
	ctxErr := ctx.Err()

	cause, result := first, first

	if ctxErr != nil {
		// Hooks get the context's own error, so errors.Is against
		// context.Canceled and context.DeadlineExceeded keeps working, plus any
		// reason the caller attached for them to read.
		cause, result = ctxErr, ctxErr

		// context.Cause repeats ctx.Err() when no reason was attached, so a
		// value that differs is exactly "the caller attached one". The argument
		// order matters: a reason that wraps ctx.Err() is still a reason, and
		// testing the other way round would discard it.
		reason := context.Cause(ctx)
		if errors.Is(ctxErr, reason) {
			reason = nil
		}

		if reason != nil {
			cause = errors.Join(ctxErr, reason)
			result = cause
		}

		// Run subtracts one sentinel and no more. A bare cancellation is the
		// caller's own doing and tells it nothing it did not already know. A
		// reason it went out of its way to attach is not bare, and a deadline
		// is information in its own right, so both are reported.
		if errors.Is(ctxErr, context.Canceled) {
			result = reason
		}
	}

	cancel()

	interruptErrs := interrupt(tasks, cause)

	wg.Wait()
	close(results)

	rest := drain(results)

	// rest holds what every task reported except the one that became the cause,
	// and only its panics survive. When the context ended, no task became the
	// cause, so the one already taken off the channel belongs here too.
	if ctxErr != nil {
		rest = append(rest, first)
	}

	return append(interruptErrs, panicErrors(rest)...), cause, result
}

// drain collects every error still in the channel after every task returned.
func drain(results <-chan error) []error {
	all := make([]error, 0, len(results))

	for err := range results {
		all = append(all, err)
	}

	return all
}

// interrupt runs every task's interrupt function concurrently, telling each why
// the group is stopping, and returns the panics that came back. Tasks with no
// interrupt function, and interrupts that returned without panicking, both leave
// a nil slot for nonNilErrors to drop.
func interrupt(tasks []Task, err error) []error {
	errs := make([]error, len(tasks))

	var wg sync.WaitGroup

	for idx, task := range tasks {
		if task.interrupt == nil {
			continue
		}

		wg.Go(func() {
			errs[idx] = recoverPanic(func() { task.interrupt(err) })
		})
	}

	wg.Wait()

	return nonNilErrors(errs)
}
