# `taskgroup`: scoped task lifecycles for Go

[![CI](https://github.com/gokern/taskgroup/actions/workflows/ci.yml/badge.svg)](https://github.com/gokern/taskgroup/actions/workflows/ci.yml)
[![Lint](https://github.com/gokern/taskgroup/actions/workflows/lint.yml/badge.svg)](https://github.com/gokern/taskgroup/actions/workflows/lint.yml)
[![CodeQL](https://github.com/gokern/taskgroup/actions/workflows/codeql.yml/badge.svg)](https://github.com/gokern/taskgroup/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gokern/taskgroup.svg)](https://pkg.go.dev/github.com/gokern/taskgroup)
[![Go Report Card](https://goreportcard.com/badge/github.com/gokern/taskgroup)](https://goreportcard.com/report/github.com/gokern/taskgroup)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gokern/taskgroup)](go.mod)
[![Release](https://img.shields.io/github/v/release/gokern/taskgroup?include_prereleases&sort=semver)](https://github.com/gokern/taskgroup/releases)
[![License](https://img.shields.io/github/license/gokern/taskgroup)](LICENSE)

<p align="center">
  <img src="img/preview.png" alt="taskgroup — scoped task lifecycles for Go" width="900">
</p>

`taskgroup` helps wire together long-running tasks that should live and stop as
one unit: servers, workers, signal handlers, background loops, and cleanup
steps.

```sh
go get github.com/gokern/taskgroup
```

## At a glance

- Use `taskgroup.New()` to create a one-shot group of tasks.
- Use `tg.AddFunc(fn)` to run a simple task with a context derived from `Run(ctx)`.
- Use `taskgroup.NewTask(fn)` to define a reusable task.
- Use `tg.Add(task)` to run a ready-made task.
- Use `task.Interrupt(stop)` when a task needs explicit shutdown logic.
- Use `tg.Defer(fn)` for cleanup that must run after all tasks have exited.
- Use `taskgroup.SignalTask()` to stop the group on common shutdown signals.
- Use `errors.Is` / `errors.As` with the error returned by `Run`.

## Why

Go makes it easy to start goroutines. It is harder to make sure they have a
clear owner, stop together, and clean up in the right order.

`taskgroup` is intentionally small. It focuses on one pattern:

1. Start a set of related tasks.
2. Wait for the first task to return, or for the run context to be canceled.
3. Interrupt the rest of the tasks.
4. Wait for all tasks to exit.
5. Run deferred cleanup functions in last-in-first-out order.

## Goals

### Goal #1: Keep task lifetimes scoped

A `TaskGroup` owns the tasks added to it. `Run` does not return until every task
has returned.

Simple tasks are started with `AddFunc`:

```go
tg := taskgroup.New()

tg.AddFunc(func(ctx context.Context) error {
	return worker.Run(ctx)
})

err := tg.Run(ctx)
```

`TaskGroup` is run once. This keeps ownership simple: create it, configure it,
run it, and let it go out of scope.

Tasks are values, so helper packages can expose ready-to-add tasks:

```go
func APIServerTask(srv *http.Server) taskgroup.Task {
	return taskgroup.NewTask(func(context.Context) error {
		return srv.ListenAndServe()
	}).Interrupt(func(error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = srv.Shutdown(ctx)
	})
}

func run(ctx context.Context, srv *http.Server) error {
	tasks := taskgroup.New()
	tasks.Add(APIServerTask(srv))
	tasks.Add(taskgroup.SignalTask())

	return tasks.Run(ctx)
}
```

### Goal #2: Make shutdown explicit

Some tasks stop by observing context cancellation. Others need explicit shutdown
logic. `Interrupt` is for that second case.

```go
tg.Add(taskgroup.NewTask(func(context.Context) error {
	return srv.ListenAndServe()
}).Interrupt(func(error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = srv.Shutdown(ctx)
}))
```

Interrupt functions run concurrently. They should be safe to call after their
task has already returned, and they should make the task return promptly.

### Goal #3: Keep cleanup ordered

`Defer` runs after all tasks have exited. Multiple deferred functions run in
last-in-first-out order, like Go `defer` statements.

```go
tg.Defer(func(err error) error {
	return metrics.Flush()
})

tg.Defer(func(err error) error {
	return logs.Sync()
})
```

### Goal #4: Return useful errors

`Run` returns the primary shutdown reason:

- the first task error,
- the run context error,
- or `nil` if the first task returned successfully.

Panics from `Interrupt`, and errors or panics from `Defer`, are joined with the
primary error using `errors.Join`. Task errors after shutdown begins are
ignored, but task panics are recovered and joined with the returned error.

## Examples

### Run a server until a signal arrives

#### `stdlib`

```go
func run(ctx context.Context, srv *http.Server) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.ListenAndServe()
	}()

	var err error
	select {
	case err = <-serverErr:
	case sig := <-signals:
		err = fmt.Errorf("signal: %s", sig)
	case <-ctx.Done():
		err = ctx.Err()
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
		return errors.Join(err, shutdownErr)
	}
	return err
}
```

#### `taskgroup`

```go
func run(ctx context.Context, srv *http.Server) error {
	tg := taskgroup.New()

	tg.Add(taskgroup.NewTask(func(context.Context) error {
		return srv.ListenAndServe()
	}).Interrupt(func(error) {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_ = srv.Shutdown(shutdownCtx)
	}))

	tg.Add(taskgroup.SignalTask())

	return tg.Run(ctx)
}
```

### Run several services as one unit

#### `stdlib`

```go
func run(ctx context.Context, api, admin *http.Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 2)

	go func() {
		errs <- api.ListenAndServe()
	}()
	go func() {
		errs <- admin.ListenAndServe()
	}()

	var err error
	select {
	case err = <-errs:
	case <-ctx.Done():
		err = ctx.Err()
	}

	cancel()

	shutdownCtx, stop := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer stop()

	return errors.Join(
		err,
		api.Shutdown(shutdownCtx),
		admin.Shutdown(shutdownCtx),
	)
}
```

#### `taskgroup`

```go
func run(ctx context.Context, api, admin *http.Server) error {
	tg := taskgroup.New()

	tg.Add(taskgroup.NewTask(func(context.Context) error {
		return api.ListenAndServe()
	}).Interrupt(func(error) {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_ = api.Shutdown(shutdownCtx)
	}))

	tg.Add(taskgroup.NewTask(func(context.Context) error {
		return admin.ListenAndServe()
	}).Interrupt(func(error) {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_ = admin.Shutdown(shutdownCtx)
	}))

	return tg.Run(ctx)
}
```

### Cleanup after every task exits

#### `stdlib`

```go
func run(ctx context.Context, db *sql.DB) error {
	err := runServices(ctx, db)

	if closeErr := db.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}
```

#### `taskgroup`

```go
func run(ctx context.Context, db *sql.DB) error {
	tg := taskgroup.New()

	tg.Defer(func(error) error {
		return db.Close()
	})

	tg.AddFunc(func(ctx context.Context) error {
		return runServices(ctx, db)
	})

	return tg.Run(ctx)
}
```

### Recover task panics as errors

#### `stdlib`

```go
func run(ctx context.Context) error {
	done := make(chan error, 1)

	go func() {
		defer func() {
			if pc := recover(); pc != nil {
				done <- fmt.Errorf("panic: %v", pc)
			}
		}()

		done <- doWork(ctx)
	}()

	return <-done
}
```

#### `taskgroup`

```go
func run(ctx context.Context) error {
	tg := taskgroup.New()

	tg.AddFunc(func(ctx context.Context) error {
		return doWork(ctx)
	})

	return tg.Run(ctx)
}
```

## Signals

`taskgroup.SignalTask()` returns a task that waits for a shutdown signal or
context cancellation.

```go
tg.Add(taskgroup.SignalTask())
```

On Unix, the default signals are `os.Interrupt` and `syscall.SIGTERM`. On
Windows, the default signal is `os.Interrupt`.

You can pass custom signals:

```go
tg.Add(taskgroup.SignalTask(syscall.SIGHUP, syscall.SIGTERM))
```

Signal errors can be detected with `IsSignalError`:

```go
err := tg.Run(ctx)
if taskgroup.IsSignalError(err) {
	log.Printf("stopped by signal")
}
```

The concrete signal can be extracted with `SignalFromError`:

```go
err := tg.Run(ctx)
if sig, ok := taskgroup.SignalFromError(err); ok {
	log.Printf("stopped by signal: %s", sig)
}
```

## Error semantics

`Run(ctx)` starts all tasks, even if `ctx` is already canceled. Each task
receives a context derived from `ctx` and decides how to handle it.

The returned error is built from:

- the primary shutdown reason,
- panics from `Interrupt` functions,
- panics from tasks after shutdown begins,
- and errors or panics from `Defer` functions.

Ordinary task errors after shutdown begins are ignored. This keeps expected
shutdown errors, such as `http.ErrServerClosed`, from hiding the reason the
group started shutting down.

## Status

`taskgroup` is small by design. It is meant for application and service
lifecycle orchestration, not for general-purpose worker pools, result
collection, or parallel mapping.
