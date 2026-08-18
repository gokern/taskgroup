package taskgroup

import (
	"slices"

	"github.com/gokern/panics"
)

// DeferFunc runs after all tasks have returned.
//
// It receives the cause of the shutdown: the first task error or recovered
// panic, the run context's error when the context ended the run, or nil when
// the first task returned cleanly. Interrupt functions receive the same value.
// It is not always what Run returns; a canceled run is a cause here and not an
// error there. See [TaskGroup.Run] for the table.
type DeferFunc func(error) error

// Defer appends a cleanup function to the TaskGroup.
//
// Deferred functions run after all tasks have returned, in last-in-first-out
// order, like Go defer statements.
func (g *TaskGroup) Defer(fn DeferFunc) {
	if fn == nil {
		panic("taskgroup: nil defer function")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.defers = append(g.defers, fn)
}

func runDefers(defers []DeferFunc, err error) []error {
	errs := make([]error, 0, len(defers))

	for _, fn := range slices.Backward(defers) {
		deferErr := panics.CatchError(func() error {
			return fn(err)
		})
		if deferErr != nil {
			errs = append(errs, deferErr)
		}
	}

	return errs
}
