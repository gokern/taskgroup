# `taskgroup`: scoped lifecycles for long-running Go tasks

[![CI](https://github.com/gokern/taskgroup/actions/workflows/ci.yml/badge.svg)](https://github.com/gokern/taskgroup/actions/workflows/ci.yml)
[![Lint](https://github.com/gokern/taskgroup/actions/workflows/lint.yml/badge.svg)](https://github.com/gokern/taskgroup/actions/workflows/lint.yml)
[![CodeQL](https://github.com/gokern/taskgroup/actions/workflows/codeql.yml/badge.svg)](https://github.com/gokern/taskgroup/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gokern/taskgroup.svg)](https://pkg.go.dev/github.com/gokern/taskgroup)
[![Go Report Card](https://goreportcard.com/badge/github.com/gokern/taskgroup)](https://goreportcard.com/report/github.com/gokern/taskgroup)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gokern/taskgroup)](go.mod)
[![Release](https://img.shields.io/github/v/release/gokern/taskgroup?include_prereleases&sort=semver)](https://github.com/gokern/taskgroup/releases)
[![License](https://img.shields.io/github/license/gokern/taskgroup)](LICENSE)

<p align="center">
  <img src="img/preview.png" alt="taskgroup: scoped task lifecycles for Go" width="900">
</p>

Starting goroutines in Go is easy. Getting them to stop together, in order, with cleanup that actually runs? That's the part where `main.go` quietly goes from neat to tangled. `taskgroup` handles that lifecycle for you.

## Install

```sh
go get github.com/gokern/taskgroup
```

Requires Go 1.26+.

## Example

An HTTP server that stops on Ctrl-C, shuts down gracefully, and closes a database on the way out.

With `taskgroup`:

```go
func run(ctx context.Context, srv *http.Server, db *sql.DB) error {
	tg := taskgroup.New()

	tg.Add(taskgroup.NewTask(func(context.Context) error {
		return srv.ListenAndServe()
	}).Interrupt(func(error) {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}))

	tg.Add(taskgroup.SignalTask())
	tg.Defer(func(error) error { return db.Close() })

	return tg.Run(ctx)
}
```

Same thing hand-rolled:

```go
func run(ctx context.Context, srv *http.Server, db *sql.DB) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()

	var err error
	select {
	case err = <-serverErr:
	case sig := <-signals:
		err = fmt.Errorf("signal: %s", sig)
	case <-ctx.Done():
		err = ctx.Err()
	}

	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return errors.Join(err, srv.Shutdown(sctx), db.Close())
}
```

Each block in the first version handles one thing: the server, the signal listener, the cleanup. The second merges all of it into one select. Adding a fourth concern to that version means rewriting the select.

## API

- `taskgroup.New()` creates a group.
- `tg.AddFunc(fn)` adds a task.
- `tg.Add(taskgroup.NewTask(fn).Interrupt(stop))` adds a task with an explicit stop hook.
- `tg.Add(taskgroup.SignalTask())` stops the group on standard shutdown signals.
- `tg.Defer(fn)` runs cleanup after every task exits, LIFO like Go `defer`.
- `tg.Run(ctx)` starts everything and returns the first error.

Runnable examples are in `example_test.go`. Everything else is in the [godoc](https://pkg.go.dev/github.com/gokern/taskgroup).

## Signals

`SignalTask()` listens for `os.Interrupt` on Windows and `os.Interrupt + SIGTERM` on Unix. Pass your own to override:

```go
tg.Add(taskgroup.SignalTask(syscall.SIGHUP, syscall.SIGTERM))
```

Detect a signal shutdown with `IsSignalError`, pull the actual signal out with `SignalFromError`.

## Errors

`Run` returns the first task error, the run context error, or nil if the first task finished cleanly. Errors from interrupts and defers are joined via `errors.Join`. Ordinary task errors that arrive after shutdown starts are dropped, so `http.ErrServerClosed` doesn't hide the real reason you stopped. Panics are recovered, wrapped with `ErrPanic`, and joined in.

## Scope

`taskgroup` is for application lifecycle: glue together servers, workers, signal handlers, and cleanup in one place. It's not a worker pool or a replacement for `errgroup` when you want to fan out a batch of jobs and collect their results.
