package stores

import (
	"context"
	"testing"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

// newTestCtx builds a gofr context backed by the framework's sqlmock-based
// mock container.
func newTestCtx(t *testing.T) (*gofr.Context, *container.Mocks) {
	t.Helper()

	mockContainer, mocks := container.NewMockContainer(t)

	return &gofr.Context{Context: context.Background(), Container: mockContainer}, mocks
}

// ptr64 builds a *int64 literal (nullable columns like links.user_id).
func ptr64(v int64) *int64 { return &v }
