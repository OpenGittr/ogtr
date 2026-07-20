package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/scanner"
)

func newRescanService(t *testing.T) (*RescanService, *MockLinkStore, *MockURLScanner, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	links := NewMockLinkStore(ctrl)
	urlScanner := NewMockURLScanner(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewRescanService(links, urlScanner), links, urlScanner, ctx
}

func TestRescanService_Run_DisablesFlaggedLinks(t *testing.T) {
	svc, links, urlScanner, ctx := newRescanService(t)

	batch := []models.Link{
		*publicLink(1, "aaaaaaa", "https://clean.example/1"),
		*publicLink(2, "bbbbbbb", "https://turned-evil.example/x"),
		*publicLink(3, "ccccccc", "https://clean.example/3"),
	}

	// One batch, then the empty page ends the walk. The cursor advances to
	// the last id of the batch.
	links.EXPECT().ListActiveClickedSince(gomock.Any(), gomock.Any(), int64(0), rescanBatchSize).
		Return(batch, nil)
	links.EXPECT().ListActiveClickedSince(gomock.Any(), gomock.Any(), int64(3), rescanBatchSize).
		Return([]models.Link{}, nil)

	urlScanner.EXPECT().Scan(gomock.Any(), "https://clean.example/1").Return(scanner.Allow(), nil)
	urlScanner.EXPECT().Scan(gomock.Any(), "https://turned-evil.example/x").
		Return(scanner.Flag(scanner.CategoryMalware), nil)
	urlScanner.EXPECT().Scan(gomock.Any(), "https://clean.example/3").Return(scanner.Allow(), nil)

	// Only the flagged link is disabled.
	links.EXPECT().SetStatusByID(gomock.Any(), int64(2), models.LinkStatusDisabledAbuse).Return(nil)

	svc.Run(ctx)
}

func TestRescanService_Run_ScanErrorSkipsLink(t *testing.T) {
	svc, links, urlScanner, ctx := newRescanService(t)

	batch := []models.Link{*publicLink(1, "aaaaaaa", "https://x.example")}

	links.EXPECT().ListActiveClickedSince(gomock.Any(), gomock.Any(), int64(0), rescanBatchSize).
		Return(batch, nil)
	links.EXPECT().ListActiveClickedSince(gomock.Any(), gomock.Any(), int64(1), rescanBatchSize).
		Return([]models.Link{}, nil)

	// Scan failure: fail-open, never disable.
	urlScanner.EXPECT().Scan(gomock.Any(), gomock.Any()).Return(scanner.Verdict{}, assert.AnError)

	svc.Run(ctx)
}

func TestRescanService_Run_ListErrorAbortsRun(t *testing.T) {
	svc, links, _, ctx := newRescanService(t)

	links.EXPECT().ListActiveClickedSince(gomock.Any(), gomock.Any(), int64(0), rescanBatchSize).
		Return(nil, assert.AnError)

	svc.Run(ctx) // no scans, no status writes — unstubbed calls would fail
}
