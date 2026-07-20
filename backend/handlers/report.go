package handlers

import (
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/visitor"
)

// ReportHandler serves POST /api/v1/report — public abuse reporting (no
// auth: the whole point is that a recipient of a bad link, who has no
// account here, can flag it). Rate-limited per IP in the service.
type ReportHandler struct {
	reports ReportService
}

// NewReportHandler wires a ReportHandler.
func NewReportHandler(reports ReportService) *ReportHandler {
	return &ReportHandler{reports: reports}
}

// reportReceipt is the minimal success body: the report id proves receipt
// without echoing anything the reporter wrote.
type reportReceipt struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// Create handles POST /api/v1/report {code, reason, reporter_contact?}.
// Unknown codes answer an honest 404 (the report page must be able to say
// "no such link"); validation problems are 422; the 6th report in a minute
// from one IP is 429. The reporter's IP comes from the visitor middleware
// and keys the rate limit only — it is never stored.
func (h *ReportHandler) Create(ctx *gofr.Context) (any, error) {
	var in services.ReportInput
	if err := ctx.Bind(&in); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	report, err := h.reports.Create(ctx, in, visitor.FromContext(ctx).IP)
	if err != nil {
		return nil, err
	}

	return reportReceipt{ID: report.ID, Status: "received"}, nil
}
