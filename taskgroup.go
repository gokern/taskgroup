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

// Run executes all tasks in the group.
//
// Run returns the first task error, the run context error, or nil when the
// first task returns cleanly. With no tasks Run returns nil and only runs
// deferred cleanup, even if ctx is already canceled. Tasks are started even
// when ctx is already canceled; each task sees ctx and decides how to handle
// it. If a task result and ctx cancellation arrive simultaneously, either
// one may become the primary error.
//
// Panics from interrupt functions, and errors or panics from deferred
// cleanup, are joined with the primary error. Ordinary task errors after
// shutdown are dropped, but task panics after shutdown are recovered and
// joined. Every recovered panic is wrapped so errors.Is(err, ErrPanic) is
// true.
func (g *TaskGroup) Run(ctx context.Context) error {
	if ctx == nil {
		panic("taskgroup: nil context")
	}

	tasks, defers := g.start()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runErrs, err := run(runCtx, cancel, tasks)
	deferErrs := runDefers(defers, err)

	return joinErrors(err, runErrs, deferErrs)
}

func (g *TaskGroup) start() ([]Task, []DeferFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.started = true

	tasks := append([]Task(nil), g.tasks...)
	defers := append([]DeferFunc(nil), g.defers...)

	return tasks, defers
}

func (g *TaskGroup) mustNotHaveStarted() {
	if g.started {
		panic("taskgroup: TaskGroup already started")
	}
}

func run(ctx context.Context, cancel context.CancelFunc, tasks []Task) ([]error, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	results := make(chan taskResult, len(tasks))

	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Go(func() {
			results <- recoverTask(func() error { return task.execute(ctx) })
		})
	}

	var result taskResult
	select {
	case result = <-results:
	case <-ctx.Done():
		result.err = ctx.Err()
	}

	cancel()

	interruptErrs := interrupt(tasks, result.err)

	wg.Wait()
	close(results)

	interruptErrs = append(interruptErrs, taskPanicErrors(results)...)

	return interruptErrs, result.err
}

func interrupt(tasks []Task, err error) []error {
	errs := make([]error, len(tasks))

	var wg sync.WaitGroup

	for idx, task := range tasks {
		if task.interrupt == nil {
			continue
		}

		wg.Go(func() {
			errs[idx] = recoverError(func() error {
				task.interrupt(err)

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
