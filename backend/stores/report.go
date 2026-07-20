package stores

import (
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// AbuseReportStore writes the abuse_reports table (public link reporting).
// Insert-only from the app: triage/deletion is an operator concern.
type AbuseReportStore struct{}

// NewAbuseReportStore builds an AbuseReportStore.
func NewAbuseReportStore() *AbuseReportStore { return &AbuseReportStore{} }

const insertAbuseReport = "INSERT INTO abuse_reports (org_id, link_id, code, reason, reporter_contact) " +
	"VALUES (?, ?, ?, ?, ?)"

// Insert writes one report row. org_id/link_id come from the reported
// link's row (derived server-side), never from the reporter.
func (*AbuseReportStore) Insert(ctx *gofr.Context, r *models.AbuseReport) error {
	res, err := ctx.SQL.ExecContext(ctx, insertAbuseReport,
		r.OrgID, r.LinkID, r.Code, r.Reason, r.ReporterContact)
	if err != nil {
		return err
	}

	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}

	return nil
}
