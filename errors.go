package taskgroup

import (
	"errors"
	"fmt"
	"slices"
)

// ErrPanic is the sentinel that wraps every panic recovered by the package.
// Test with errors.Is(err, taskgroup.ErrPanic) to detect a recovered panic.
var ErrPanic = errors.New("panic")

type taskResult struct {
	err   error
	panic bool
}

// Named return lets the deferred recover set result on panic.
//
//nolint:nonamedreturns
func recoverTask(fn func() error) (result taskResult) {
	defer func() {
		if pc := recover(); pc != nil {
			result.err = panicToError(pc)
			result.panic = true
		}
	}()

	result.err = fn()

	return result
}

func recoverError(fn func() error) (err error) {
	defer func() {
		if pc := recover(); pc != nil {
			err = panicToError(pc)
		}
	}()

	return fn()
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

func compactErrors(errs []error) []error {
	return slices.DeleteFunc(errs, func(err error) bool { return err == nil })
}
