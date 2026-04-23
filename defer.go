package taskgroup

// DeferFunc runs after all tasks have returned.
//
// It receives the primary shutdown error: the first task error (including a
// recovered panic from the first task to return), the run context error, or
// nil when the first task returned without error.
type DeferFunc func(error) error

// Defer appends a cleanup function to the TaskGroup.
//
// Deferred functions run after all tasks have returned, in last-in-first-out
// order, like Go defer statements.
func (g *TaskGroup) Defer(fn DeferFunc) *TaskGroup {
	if fn == nil {
		panic("taskgroup: nil defer function")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.defers = append(g.defers, fn)

	return g
}

func runDefers(defers []DeferFunc, err error) []error {
	errs := make([]error, 0, len(defers))
	for i := len(defers) - 1; i >= 0; i-- {
		deferErr := recoverError(func() error {
			return defers[i](err)
		})
		if deferErr != nil {
			errs = append(errs, deferErr)
		}
	}

	return errs
}
