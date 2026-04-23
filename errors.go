package taskgroup

import (
	"errors"
	"fmt"
)

type taskResult struct {
	err   error
	panic bool
}

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
		return fmt.Errorf("panic: %w", err)
	}

	return fmt.Errorf("panic: %v", pc)
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
	result := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			result = append(result, err)
		}
	}

	return result
}
