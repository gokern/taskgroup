# Changelog

Notable changes to `taskgroup`. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.2.0 — 2026-08-14

> **Read this before upgrading.** Nothing in the API changed, so your code will
> still compile, but `Run` reports differently: a canceled run that used to
> return `context.Canceled` now returns `nil`. The compiler will not flag it.
> If you branch on `Run`'s error, read the migration notes below.

Reworks what `Run` reports. A run now produces two values instead of one. The
*cause* is why the group is stopping and goes to every `Interrupt` and `Defer`
function. The *result* is what `Run` returns: the cause minus a bare
`context.Canceled`.

### Changed

- **`Run` returns `nil` when the run context is canceled**, instead of
  `context.Canceled`. Cancellation is an action the caller took, so reporting it
  says nothing the caller does not already know, and manufacturing it raced a
  real task error for the same slot.
- **A reason attached with `context.WithCancelCause` is reported.** `Run`
  returns that reason on its own; the bare sentinel does not travel with it, so
  `errors.Is(err, context.Canceled)` stays a reliable "stopped cleanly" test.
  `cancel(nil)` sets the cause to `context.Canceled` and gets `nil` back.
- **A reason attached with `context.WithTimeoutCause` is reported**, joined with
  `context.DeadlineExceeded`. An expired deadline on its own is reported as
  before: a deadline is a condition the caller set without learning whether it
  fired.
- **`Interrupt` and `Defer` functions receive the cause, not the result.** On a
  canceled run they now see `context.Canceled` while `Run` returns `nil`. Where
  the caller attached a reason, they see it joined with `ctx.Err()`, so
  `errors.Is` finds either.

### Fixed

- **A task result and a canceled context arriving together no longer decide the
  outcome at random.** Whether the run context ended is read from the context
  rather than from whichever `select` case won, so a group handed a context that
  is already over answers the same way on every run. The previous behaviour was
  documented as "either one may become the primary error".
- **Ordinary task errors arriving after shutdown are dropped consistently.**
  They were dropped when a task ended the run but could surface when the context
  did, which let `http.ErrServerClosed` become the result of a clean stop.

### Documentation

- `Run` carries the full outcome table, and the README explains why a
  cancellation is silent while a deadline is not.
- The flagship `main.go` example no longer calls `log.Fatal` on a clean Ctrl-C;
  it tests with `IsSignalError` first.
- Stated that `Run` always returns a joined error, so `errors.Is` or `errors.As`
  is required and `==` will not match a task's error value.

## Migration

Every signature is unchanged, so nothing to do to keep compiling. What follows
is about behaviour.

Callers that treated a canceled run as an error need no change: `Run` returns
`nil`, so `if err != nil` simply stops firing.

Callers that detected a clean shutdown with `errors.Is(err, context.Canceled)`
must invert the test. A canceled run is now the `nil` case:

```go
// before
if err := tasks.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
	log.Fatal(err)
}

// after
if err := tasks.Run(ctx); err != nil {
	log.Fatal(err)
}
```

Callers that want a canceled run to carry a reason should attach one:

```go
ctx, cancel := context.WithCancelCause(context.Background())
cancel(fmt.Errorf("config reload failed: %w", err))
// Run returns that error; Interrupt and Defer see it joined with context.Canceled.
```

`Interrupt` and `Defer` functions that branched on their argument to tell a
clean stop from a failure keep working, and now also see a caller-supplied
reason. Ones that assumed the argument equals what `Run` returns no longer hold
on a canceled run.

## 1.1.2 and earlier

Not recorded here; see the commit history.
