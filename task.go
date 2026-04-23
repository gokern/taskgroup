package taskgroup

import "context"

// ExecuteFunc is the main body of a task.
type ExecuteFunc func(context.Context) error

// InterruptFunc is called when the TaskGroup starts shutting down.
//
// Interrupt functions must be safe to call after their task has already
// returned. They may run concurrently with other interrupt functions and should
// make the corresponding ExecuteFunc return promptly.
type InterruptFunc func(error)

// Task is a unit of work scheduled by a TaskGroup.
type Task struct {
	execute   ExecuteFunc
	interrupt InterruptFunc
}

// NewTask creates a task from execute.
func NewTask(execute ExecuteFunc) Task {
	if execute == nil {
		panic("taskgroup: nil execute function")
	}

	return Task{
		execute:   execute,
		interrupt: nil,
	}
}

// Interrupt returns a copy of t with the interrupt function set. See
// InterruptFunc for required semantics.
func (t Task) Interrupt(interrupt InterruptFunc) Task {
	if interrupt == nil {
		panic("taskgroup: nil interrupt function")
	}

	t.interrupt = interrupt

	return t
}
