package taskgroup

import (
	"slices"

	"github.com/gokern/panics"
)

// ErrPanic is [panics.ErrPanic] under this package's name, retained so that
// errors.Is(err, taskgroup.ErrPanic) keeps compiling and keeps matching what it
// matched in 1.2.0. It is an assignment rather than a sentinel of its own: both
// names hold the one value.
//
// There are still two reasons to move off it. It is a var, so this package now
// holds a second mutable slot pointing at that value, and two slots are one
// assignment apart from disagreeing; [panics.Is] asks through a function and has
// nothing to drift. And the name is narrower than the behaviour — it matches a
// panic contained anywhere in the error, including one a task recovered itself
// and reported as an ordinary failure, and one a nested group contained, not
// only a panic this package recovered. That was already true of the 1.2.0
// sentinel, so the alias inherits the mismatch rather than introducing it.
//
// Deprecated: use [panics.Is]. The alias stays for the life of the 1.x line:
// removing it is the compatibility break this release exists to avoid, so there
// is no deadline here and nothing to migrate ahead of.
var ErrPanic = panics.ErrPanic

// panicErrors keeps the contained panics among errs, the outcomes that survive
// arriving after the group has stopped.
//
// [panics.Is] asks whether a panic was contained anywhere in the error, not
// whether this package recovered it, so a task that contained one itself and
// reported it as an ordinary error is kept too. That is also what keeps a nested
// group's panic visible in the group that ran it: from out here the two are the
// same error.
func panicErrors(errs []error) []error {
	recovered := make([]error, 0, len(errs))

	for _, err := range errs {
		if panics.Is(err) {
			recovered = append(recovered, err)
		}
	}

	return recovered
}

// nonNilErrors keeps the errors among errs, dropping the empty slots left by
// tasks that had no interrupt function to run and by interrupts that returned
// without panicking.
func nonNilErrors(errs []error) []error {
	return slices.DeleteFunc(errs, func(err error) bool { return err == nil })
}
