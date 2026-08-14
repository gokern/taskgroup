package taskgroup_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gokern/taskgroup"
)

func TestNewTask(t *testing.T) {
	t.Parallel()

	t.Run("nil execute function", func(t *testing.T) {
		t.Parallel()

		require.PanicsWithValue(t,
			"taskgroup: nil execute function",
			func() { taskgroup.NewTask(nil) },
		)
	})
}

func TestTask_Interrupt(t *testing.T) {
	t.Parallel()

	t.Run("nil interrupt function", func(t *testing.T) {
		t.Parallel()

		task := taskgroup.NewTask(func(context.Context) error { return nil })

		require.PanicsWithValue(t,
			"taskgroup: nil interrupt function",
			func() { task.Interrupt(nil) },
		)
	})
}
