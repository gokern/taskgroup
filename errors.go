package taskgroup

import (
	"errors"
	"fmt"
	"slices"
)

// ErrPanic is the sentinel that wraps every panic recovered by the package.
// Test with errors.Is(err, taskgroup.ErrPanic) to detect a recovered panic.
var ErrPanic = errors.New("panic")

// recoverError runs fn and returns its error, or, if fn panicked, the recovered
// value wrapped so errors.Is(err, ErrPanic) holds.
func recoverError(fn func() error) (err error) {
	defer func() {
		if pc := recover(); pc != nil {
			err = panicToError(pc)
		}
	}()

	return fn()
}

// recoverPanic runs fn and returns the recovered panic, or nil. It is the
// no-result form of recoverError, for a hook that has no error to report and so
// can only ever contribute a panic.
func recoverPanic(fn func()) error {
	return recoverError(func() error {
		fn()

		return nil
	})
}

func panicToError(pc any) error {
	if err, ok := pc.(error); ok {
		return fmt.Errorf("%w: %w", ErrPanic, err)
	}

	return fmt.Errorf("%w: %v", ErrPanic, pc)
}

func joinErrors(primary error, errGroups ...[]error) error {
	var errs []error
	if primary != nil {
		errs = append(errs, primary)
	}

	for _, group := range errGroups {
		errs = append(errs, group...)
	}

	return errors.Join(errs...)
}

// panicErrors keeps the recovered panics among errs, the outcomes that survive
// arriving after the group has stopped. Every recovered panic carries ErrPanic,
// so the wrapper is the only marker needed.
func panicErrors(errs []error) []error {
	panics := make([]error, 0, len(errs))

	for _, err := range errs {
		if errors.Is(err, ErrPanic) {
			panics = append(panics, err)
		}
	}

	return panics
}

// nonNilErrors keeps the errors among errs, dropping the empty slots left by
// tasks that had no interrupt function to run and by interrupts that returned
// without panicking.
func nonNilErrors(errs []error) []error {
	return slices.DeleteFunc(errs, func(err error) bool { return err == nil })
}
