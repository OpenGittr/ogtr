package stores

import (
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// ClickStore writes the clicks table.
type ClickStore struct{}

// NewClickStore builds a ClickStore.
func NewClickStore() *ClickStore { return &ClickStore{} }

// insertClick is a fixed, explicit column list: click attributes can never
// choose their own column names (the SQL allowlist is structural —
// FEATURES.md INV-2). ts defaults to CURRENT_TIMESTAMP in the schema.
const insertClick = `INSERT INTO clicks (org_id, link_id, utm_source, utm_medium, utm_campaign,
	device_type, mobile_os, browser, referrer, ip, city, country, region,
	is_deeplink, target_matched, custom_tag_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert records one click. Empty string attributes are stored as NULL.
func (*ClickStore) Insert(ctx *gofr.Context, c *models.Click) error {
	_, err := ctx.SQL.ExecContext(ctx, insertClick,
		c.OrgID, c.LinkID,
		nullable(c.UTMSource), nullable(c.UTMMedium), nullable(c.UTMCampaign),
		nullable(c.DeviceType), nullable(c.MobileOS), nullable(c.Browser),
		nullable(c.Referrer), nullable(c.IP), nullable(c.City),
		nullable(c.Country), nullable(c.Region),
		c.IsDeeplink, c.TargetMatched, nullable(c.CustomTagID))

	return err
}

// nullable maps "" to SQL NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}

	return s
}
