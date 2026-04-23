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

type actor struct {
	execute   ExecuteFunc
	interrupt InterruptFunc
}

// Task is a handle returned by Add for configuring a task.
type Task struct {
	group *TaskGroup
	index int
}

// Add appends a task to the TaskGroup.
func (g *TaskGroup) Add(execute ExecuteFunc) *Task {
	if execute == nil {
		panic("taskgroup: nil execute function")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.mustNotHaveStarted()
	g.actors = append(g.actors, actor{
		execute: execute,
	})

	return &Task{
		group: g,
		index: len(g.actors) - 1,
	}
}

// Interrupt sets the task's interrupt function. See InterruptFunc for the
// required semantics. Passing nil panics. Calling Interrupt after the group
// has started panics.
func (t *Task) Interrupt(interrupt InterruptFunc) *Task {
	if interrupt == nil {
		panic("taskgroup: nil interrupt function")
	}

	t.group.mu.Lock()
	defer t.group.mu.Unlock()

	t.group.mustNotHaveStarted()
	t.group.actors[t.index].interrupt = interrupt

	return t
}
