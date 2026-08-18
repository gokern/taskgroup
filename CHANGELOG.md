# Changelog

Notable changes to `taskgroup`. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.3.0 — 2026-08-18

Panic recovery moves to [`github.com/gokern/panics`](https://github.com/gokern/panics),
so a recovered panic carries the frames between the panic site and the point where
the group contained it, instead of just a sentinel and a message. That module owns the panic vocabulary, and this one
stops exporting its own.

> **Nothing stops compiling.** `taskgroup.ErrPanic` is deprecated but retained,
> and it matches exactly what it matched in 1.2.0. The behaviour changes below
> are the ones to read.

### Deprecated

- **`taskgroup.ErrPanic` is now an alias of `panics.ErrPanic`. Use
  `panics.Is(err)`.** Panic recovery goes through `github.com/gokern/panics`,
  and that module owns the whole vocabulary: the sentinel, the `*panics.Panic`
  type, `Is` and `As`. One concept, one name, in the module that owns it.

  The alias is exact rather than approximate — the two spellings are one value,
  so every `errors.Is(err, taskgroup.ErrPanic)` keeps matching precisely the
  errors it matched before, and `panics.Is(err)` is a drop-in for it. Removing
  the name outright would have been a v1 compatibility break to save one line,
  which is not a trade this package makes. The alias stays for the life of the
  1.x line — there is no deadline attached to the deprecation, and no 2.x being
  cut to carry out the removal.

  The reason to migrate anyway is that the name reads narrower than it behaves.
  `taskgroup.ErrPanic` matches a panic contained anywhere in the error —
  including one a task recovered itself and one a nested group contained — not
  only a panic this package recovered. That was already true of the 1.2.0
  sentinel, so the alias inherits the mismatch rather than introducing it, but
  `panics.Is` is the spelling that says what it does.

### Changed

- **Recovered panics now carry the stack they came from.** Reach it with
  `panics.As(err)`, which returns the outermost panic in the error; a run can
  join several, so hand the joined error to a crash reporter rather than an
  extracted panic. Panic recovery is implemented on top of
  `github.com/gokern/panics`, taskgroup's first and only dependency.
- **A task error carrying a panic contained anywhere now survives shutdown.**
  The filter that keeps panics arriving after the group has started stopping
  matches a contained panic from anywhere — a task that returns an error
  carrying a panic contained through `panics`, whether it called
  `panics.Catch` itself or got that error from a library that did — where
  before only taskgroup's own panics matched. This extends the existing rule
  that the wrapper marks a panic rather than how the task ended; it only ever
  reports more, never less. It is also what keeps a nested group's panic visible
  in the group that ran it, so the two cannot be separated.
- **A non-string, non-error panic value renders via `%#v` instead of `%v`.**
  `panic(myStruct{1})` was reported as `panic: {1}` and is now
  `panic: main.myStruct{A:1}`. Field names and the concrete type survive into
  the message, which is what makes an unfamiliar panic value identifiable at
  all; anything matching on the old rendering will notice.

  This is a trade, not a straight gain, and the losing side is any value that
  already rendered well. `%#v` ignores `String()`, so `panic(5*time.Second)`
  reads as `panic: 5000000000` where it used to read `panic: 5s`, and
  `panic(net.IPv4(1,2,3,4))` as `panic: net.IP{0x0, …}` instead of
  `panic: 1.2.3.4` — the same applies to UUIDs, enums, and most domain types
  with a compact `Stringer`. Pointer fields also render as live heap addresses,
  so two processes hitting one bug emit different strings, and a value holding a
  large `[]byte` renders every byte into a single-line message.
- **A `panic(nil)` in a task is no longer swallowed.** Only observable under
  `GODEBUG=panicnil=1`, or in a main module whose `go` directive predates 1.21,
  where `recover()` still yields `nil` for a panic that really happened: the old
  recovery saw that `nil` and returned no error at all, so the panic vanished and
  the run looked clean. It is now reported as `panic: <nil>`, which also means
  `Interrupt` and `Defer` functions receive a non-nil cause where they used to
  get none. Everywhere else Go 1.21's `*runtime.PanicNilError` already made this
  visible.

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
