package taskgroup

import (
	"context"
	"sync"
)

// TaskGroup manages a collection of concurrent tasks.
//
// A TaskGroup is run once. When the first task returns, or when the run context
// is canceled, the group interrupts every task concurrently, waits for all
// tasks to return, then runs deferred cleanup functions in last-in-first-out
// order.
type TaskGroup struct {
	mu      sync.Mutex
	started bool

	actors []actor
	defers []DeferFunc
}

// New creates an empty TaskGroup.
func New() *TaskGroup {
	return &TaskGroup{}
}

// Run executes all tasks in the group.
//
// Run returns the first task error, the run context error, or nil when the
// first task returns without error. Tasks are started even when ctx is already
// canceled; each task receives ctx and decides how to handle it.
//
// Errors and panics from interrupt and deferred cleanup functions are joined
// with the primary error. Ordinary task errors after shutdown begins are
// dropped, but task panics after shutdown are recovered and joined with the
// returned error.
func (g *TaskGroup) Run(ctx context.Context) error {
	if ctx == nil {
		panic("taskgroup: nil context")
	}

	actors, defers := g.start()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runErrs, err := run(runCtx, cancel, actors)
	deferErrs := runDefers(defers, err)

	return joinErrors(err, runErrs, deferErrs)
}

func (g *TaskGroup) start() ([]actor, []DeferFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.started = true

	actors := append([]actor(nil), g.actors...)
	defers := append([]DeferFunc(nil), g.defers...)

	return actors, defers
}

func (g *TaskGroup) mustNotHaveStarted() {
	if g.started {
		panic("taskgroup: TaskGroup already started")
	}
}

func run(ctx context.Context, cancel context.CancelFunc, actors []actor) ([]error, error) {
	if len(actors) == 0 {
		return nil, nil
	}

	results := make(chan taskResult, len(actors))

	var wg sync.WaitGroup
	for _, a := range actors {
		wg.Go(func() {
			results <- recoverTask(func() error {
				return a.execute(ctx)
			})
		})
	}

	var result taskResult
	select {
	case result = <-results:
	case <-ctx.Done():
		result.err = ctx.Err()
	}

	cancel()
	interruptErrs := interrupt(actors, result.err)
	wg.Wait()
	close(results)

	runErrs := interruptErrs
	runErrs = append(runErrs, taskPanicErrors(results)...)

	return runErrs, result.err
}

func interrupt(actors []actor, err error) []error {
	var count int
	for _, a := range actors {
		if a.interrupt != nil {
			count++
		}
	}
	if count == 0 {
		return nil
	}

	errs := make([]error, count)

	var wg sync.WaitGroup
	var i int
	for _, a := range actors {
		if a.interrupt == nil {
			continue
		}

		interrupt := a.interrupt
		index := i
		i++

		wg.Go(func() {
			errs[index] = recoverError(func() error {
				interrupt(err)
				return nil
			})
		})
	}

	wg.Wait()

	return compactErrors(errs)
}

func taskPanicErrors(results <-chan taskResult) []error {
	errs := make([]error, 0, len(results))
	for result := range results {
		if result.panic {
			errs = append(errs, result.err)
		}
	}

	return errs
}
