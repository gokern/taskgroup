// Package taskgroup coordinates related long-running tasks that should live
// and stop as one unit: servers, workers, signal handlers, background loops,
// and cleanup steps.
//
// A TaskGroup is run once. When the first task returns, or when the run
// context is canceled, the group interrupts every task concurrently, waits
// for all tasks to return, then runs deferred cleanup functions in
// last-in-first-out order.
//
// Run returns the first task error, the run context error, or nil. Errors and
// panics from Interrupt and Defer functions, and panics from any task, are
// joined with the primary error; recovered panics are wrapped with ErrPanic.
//
// See the Example functions for common patterns.
package taskgroup
