package services

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

const (
	// rescanClickWindow selects which links a re-scan run looks at: those
	// clicked in the last 7 days. Recently clicked links are the ones
	// actively exposing visitors; dormant links get re-checked when traffic
	// returns.
	rescanClickWindow = 7 * 24 * time.Hour

	// rescanBatchSize is the page size of the id-cursor walk.
	rescanBatchSize = 100

	// rescanMaxLinks bounds one run: a huge deployment finishes protecting
	// its hottest links now and picks the rest up next interval, instead of
	// hammering the scanner layers for hours.
	rescanMaxLinks = 10000
)

// RescanService periodically re-scans the destinations of recently clicked
// links (gofr cron, RESCAN_INTERVAL): a destination that was clean at
// creation can turn malicious later — the classic aged-domain switch. A
// flagged link gets status DISABLED_ABUSE and stops redirecting (410).
// Re-enabling after a successful appeal is a deliberate operator DB action
// (documented in DEPLOYMENT.md); the job never flips links back on its own,
// because feeds fluctuate and a flip-flopping link is worse than a
// conservatively disabled one.
type RescanService struct {
	links   LinkStore
	scanner URLScanner
}

// NewRescanService wires a RescanService.
func NewRescanService(links LinkStore, urlScanner URLScanner) *RescanService {
	return &RescanService{links: links, scanner: urlScanner}
}

// Run executes one re-scan pass: batched id-cursor walk over ACTIVE links
// clicked inside the window, bounded per run. Scan errors skip the link
// (fail-open, like creation); store errors abort the run — the next
// interval retries from scratch.
func (s *RescanService) Run(ctx *gofr.Context) {
	since := time.Now().Add(-rescanClickWindow).UTC().Format("2006-01-02 15:04:05")

	var (
		afterID  int64
		scanned  int
		disabled int
	)

	for scanned < rescanMaxLinks {
		batch, err := s.links.ListActiveClickedSince(ctx, since, afterID, rescanBatchSize)
		if err != nil {
			ctx.Logger.Errorf("re-scan: listing links failed (run aborted, next interval retries): %v", err)

			return
		}

		if len(batch) == 0 {
			break
		}

		for i := range batch {
			link := &batch[i]
			afterID = link.ID
			scanned++

			if s.disableIfFlagged(ctx, link) {
				disabled++
			}

			if scanned >= rescanMaxLinks {
				break
			}
		}
	}

	ctx.Logger.Infof("re-scan finished: %d links scanned, %d disabled", scanned, disabled)
}

// disableIfFlagged scans one link's destination and disables the link on a
// flagged verdict; reports whether it disabled.
func (s *RescanService) disableIfFlagged(ctx *gofr.Context, link *models.Link) bool {
	verdict, err := s.scanner.Scan(ctx, link.DestinationURL)
	if err != nil {
		ctx.Logger.Errorf("re-scan: scanning link %d failed (skipped): %v", link.ID, err)

		return false
	}

	if verdict.Allowed {
		return false
	}

	if err := s.links.SetStatusByID(ctx, link.ID, models.LinkStatusDisabledAbuse); err != nil {
		ctx.Logger.Errorf("re-scan: disabling link %d failed: %v", link.ID, err)

		return false
	}

	ctx.Logger.Warnf("re-scan: link %d (%s) in org %d disabled — destination flagged (%s): %s",
		link.ID, link.Code, link.OrgID, verdict.Category, link.DestinationURL)

	return true
}
