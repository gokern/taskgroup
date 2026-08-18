// Package taskgroup coordinates related long-running tasks that should live
// and stop as one unit: servers, workers, signal handlers, background loops,
// and cleanup steps.
//
// A TaskGroup is run once. When the first task returns, or when the run
// context ends, the group interrupts every task concurrently, waits for all
// tasks to return, then runs deferred cleanup functions in last-in-first-out
// order.
//
// A run produces two values. The cause is why the group is stopping; every
// Interrupt and Defer function receives it. The result is what Run returns: the
// same value, minus a bare context.Canceled. A deadline is reported, and so is
// a reason passed to a CancelCauseFunc. See [TaskGroup.Run] for the table and
// the rest of the rules.
//
// Ordinary task errors that arrive once the group is stopping are dropped;
// panics never are. Panics from Interrupt functions, errors and panics from
// Defer functions, and panics from any task are joined with the result.
// Recovered panics match panics.Is and carry the stack they came from; reach it
// with panics.As(err).
//
// See the Example functions for common patterns.
package taskgroup
