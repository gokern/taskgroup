package taskgroup_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/gokern/taskgroup"
)

// A zero-configuration group that runs one task to completion.
func ExampleTaskGroup() {
	tg := taskgroup.New()

	tg.AddFunc(func(context.Context) error {
		fmt.Println("working")

		return nil
	})

	err := tg.Run(context.Background())
	if err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// working
}

// A task with an explicit shutdown function. Interrupt runs when the group
// starts shutting down.
func ExampleNewTask() {
	task := taskgroup.NewTask(func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}).Interrupt(func(error) {
		fmt.Println("stop")
	})

	tg := taskgroup.New()
	tg.Add(task)
	// Second task returns immediately, triggering shutdown of the first.
	tg.AddFunc(func(context.Context) error {
		fmt.Println("done")

		return nil
	})

	_ = tg.Run(context.Background())
	// Output:
	// done
	// stop
}

// Deferred cleanup runs after all tasks have returned, in LIFO order.
func ExampleTaskGroup_Defer() {
	tg := taskgroup.New()

	tg.Defer(func(error) error {
		fmt.Println("close db")

		return nil
	})
	tg.Defer(func(error) error {
		fmt.Println("flush metrics")

		return nil
	})

	tg.AddFunc(func(context.Context) error {
		fmt.Println("work")

		return nil
	})

	_ = tg.Run(context.Background())
	// Output:
	// work
	// flush metrics
	// close db
}

// SignalTask stops the group on shutdown signals. Detect the cause with
// IsSignalError and SignalFromError.
func ExampleSignalTask() {
	tg := taskgroup.New()
	tg.Add(taskgroup.SignalTask())

	// Immediately canceled context so the example terminates deterministically.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tg.Run(ctx)

	switch {
	case taskgroup.IsSignalError(err):
		sig, _ := taskgroup.SignalFromError(err)
		fmt.Println("stopped by signal:", sig)
	case err != nil:
		fmt.Println("failed:", err)
	default:
		// A canceled run context is not an error: the caller asked for the
		// stop, so the group has nothing to report.
		fmt.Println("stopped cleanly")
	}
	// Output:
	// stopped cleanly
}

// Run drops what a task reports once the group is stopping. A task whose error
// should be heard anyway reports it itself.
func ExampleTaskGroup_Run_droppedError() {
	stop := make(chan struct{})

	tg := taskgroup.New()

	tg.Add(taskgroup.NewTask(func(context.Context) error {
		<-stop

		err := errors.New("flush failed")
		fmt.Println("worker:", err)

		return err
	}).Interrupt(func(error) {
		close(stop)
	}))

	// A one-shot job that finishes first and ends the run.
	tg.AddFunc(func(context.Context) error {
		return nil
	})

	fmt.Println("Run:", tg.Run(context.Background()))
	// Output:
	// worker: flush failed
	// Run: <nil>
}
