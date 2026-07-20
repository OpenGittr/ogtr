package services

import (
	"strings"
	"unicode/utf8"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/ratelimit"
)

// maxReportReasonLength bounds the free-text reason (spec: 140 chars).
const maxReportReasonLength = 140

// maxReporterContactLength matches the reporter_contact column.
const maxReporterContactLength = 255

// ReportInput is the POST /api/v1/report payload after binding.
type ReportInput struct {
	Code            string `json:"code"`
	Reason          string `json:"reason"`
	ReporterContact string `json:"reporter_contact"`
}

// ReportService implements public abuse reporting: anyone who received a
// short link can report it without an account. Reports only ever create a
// row for operator triage — they never auto-disable a link (that would make
// the report endpoint itself a takedown weapon).
type ReportService struct {
	links   LinkStore
	reports AbuseReportStore
	limiter *ratelimit.SlidingWindow // per reporter IP; nil = unlimited
}

// NewReportService wires a ReportService.
func NewReportService(links LinkStore, reports AbuseReportStore, limiter *ratelimit.SlidingWindow) *ReportService {
	return &ReportService{links: links, reports: reports, limiter: limiter}
}

// Create validates and stores one abuse report. An unknown code is an
// honest 404 — the report page needs to say "no such link", and the
// preview/report flow only ever starts from a link that resolved anyway, so
// no enumeration surface is added beyond resolution itself (which carries
// the guess throttle). reporterIP keys the rate limit (5/min per IP,
// per-instance) and is never stored.
func (s *ReportService) Create(ctx *gofr.Context, in ReportInput, reporterIP string) (*models.AbuseReport, error) {
	code := strings.TrimSpace(in.Code)
	reason := strings.TrimSpace(in.Reason)
	contact := strings.TrimSpace(in.ReporterContact)

	switch {
	case code == "":
		return nil, apierrors.Unprocessable("code is required")
	case reason == "":
		return nil, apierrors.Unprocessable("reason is required")
	case utf8.RuneCountInString(reason) > maxReportReasonLength:
		return nil, apierrors.Unprocessable("reason must be at most 140 characters")
	case utf8.RuneCountInString(contact) > maxReporterContactLength:
		return nil, apierrors.Unprocessable("reporter_contact is too long")
	}

	if s.limiter != nil && !s.limiter.Allow(reporterIP) {
		return nil, apierrors.TooManyRequests("too many reports from this address — try again in a minute")
	}

	link, err := s.links.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	if link == nil {
		return nil, apierrors.NotFound("no short link exists for this code")
	}

	report := &models.AbuseReport{
		OrgID:  link.OrgID,
		LinkID: link.ID,
		Code:   code,
		Reason: reason,
	}
	if contact != "" {
		report.ReporterContact = &contact
	}

	if err := s.reports.Insert(ctx, report); err != nil {
		return nil, err
	}

	ctx.Logger.Warnf("abuse report %d filed against link %d (%s) in org %d",
		report.ID, link.ID, code, link.OrgID)

	return report, nil
}
