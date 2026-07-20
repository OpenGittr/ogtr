package services

import (
	"context"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

const (
	// txtRecordPrefix is where the ownership-proving TXT record lives:
	// _ogtr-verify.<hostname>. Its value must equal the domain's
	// verification token exactly.
	txtRecordPrefix = "_ogtr-verify."

	// verificationTokenLength is the number of random base62 characters in a
	// verification token (~190 bits — unguessable).
	verificationTokenLength = 32

	// dnsLookupTimeout bounds the TXT lookup so a slow resolver can never
	// hang a verify request.
	dnsLookupTimeout = 5 * time.Second

	maxHostnameLength = 253
	maxLabelLength    = 63
)

// hostLabelRe is one DNS label: letters/digits/hyphens, no edge hyphens.
var hostLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// DomainService implements per-org custom short domains (FEATURES.md §1.6):
// register a hostname, prove control via a DNS TXT record, then serve that
// org's short links on it and display short URLs under the org's primary
// verified domain.
type DomainService struct {
	domains   DomainStore
	members   MemberStore
	dns       DNSResolver
	shortHost string // SHORT_DOMAIN with any port stripped
}

// NewDomainService wires a DomainService. dns is the TXT-record resolver
// (net.DefaultResolver in production); shortDomain is the deployment's
// SHORT_DOMAIN (a port, as in local dev, is ignored for comparisons).
func NewDomainService(domains DomainStore, members MemberStore, dns DNSResolver, shortDomain string) *DomainService {
	return &DomainService{
		domains:   domains,
		members:   members,
		dns:       dns,
		shortHost: NormalizeHost(shortDomain),
	}
}

// Create registers a hostname for the org (OWNER only) as PENDING and
// returns it with the TXT record to publish. Hostnames are globally unique —
// a hostname owned by any org (verified or not) is a 409.
func (s *DomainService) Create(ctx *gofr.Context, orgID, actorID int64, hostname string) (*models.Domain, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	normalized, err := s.normalizeHostname(hostname)
	if err != nil {
		return nil, err
	}

	existing, err := s.domains.GetByHostname(ctx, normalized)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		// Same message whether this or another org holds it — hostnames are
		// one global namespace (the unique index backstops races).
		return nil, apierrors.Conflict("this domain is already registered")
	}

	token, err := randomCode(verificationTokenLength)
	if err != nil {
		return nil, err
	}

	domain, err := s.domains.Create(ctx, orgID, normalized, token)
	if err != nil {
		return nil, err
	}

	ctx.Logger.Infof("domain %d (%s) registered in org %d by user %d (pending verification)",
		domain.ID, domain.Hostname, orgID, actorID)

	return withTXTRecord(domain), nil
}

// List returns the org's domains (any member may look).
func (s *DomainService) List(ctx *gofr.Context, orgID int64) ([]models.Domain, error) {
	domains, err := s.domains.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}

	for i := range domains {
		withTXTRecord(&domains[i])
	}

	return domains, nil
}

// Verify runs the DNS ownership check (OWNER only): the TXT record at
// _ogtr-verify.<hostname> must equal the domain's verification token. On
// success the domain flips to VERIFIED (verified_at stamped) and is returned;
// an already-VERIFIED domain returns unchanged (idempotent).
//
// Failure semantics (documented in ARCHITECTURE.md): any state where DNS did
// not prove ownership — record missing, value mismatch, lookup error or
// timeout — is a 409 with a human-readable reason; the client retries after
// fixing DNS or waiting out propagation. There is no test-only bypass: tests
// substitute the DNSResolver dependency and exercise this exact code.
func (s *DomainService) Verify(ctx *gofr.Context, orgID, actorID, id int64) (*models.Domain, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	domain, err := s.getDomain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	switch domain.Status {
	case models.DomainStatusVerified:
		return withTXTRecord(domain), nil
	case models.DomainStatusDisabled:
		return nil, apierrors.Conflict("this domain is disabled and cannot be verified")
	}

	// Re-checked at verify time (not only at create): SHORT_DOMAIN can change
	// between deployments, and the deployment's own domain must never become
	// an org-scoped custom domain.
	if err := s.rejectDeploymentHost(domain.Hostname); err != nil {
		return nil, err
	}

	if err := s.checkTXTRecord(ctx, domain); err != nil {
		return nil, err
	}

	if err := s.domains.SetVerified(ctx, orgID, id); err != nil {
		return nil, err
	}

	ctx.Logger.Infof("domain %d (%s) verified in org %d by user %d",
		domain.ID, domain.Hostname, orgID, actorID)

	return s.getWithTXT(ctx, orgID, id)
}

// checkTXTRecord performs the bounded DNS lookup and compares every returned
// TXT value against the token.
func (s *DomainService) checkTXTRecord(ctx *gofr.Context, domain *models.Domain) error {
	lookupCtx, cancel := context.WithTimeout(ctx, dnsLookupTimeout)
	defer cancel()

	recordName := txtRecordPrefix + domain.Hostname

	values, err := s.dns.LookupTXT(lookupCtx, recordName)
	if err != nil {
		ctx.Logger.Infof("TXT lookup for %s failed: %v", recordName, err)

		return apierrors.Conflict("the verification TXT record for " + recordName +
			" could not be found yet — check the record and allow time for DNS propagation")
	}

	for _, v := range values {
		if strings.TrimSpace(v) == domain.VerificationToken {
			return nil
		}
	}

	return apierrors.Conflict("a TXT record exists at " + recordName +
		" but none of its values match the verification token")
}

// SetPrimary marks the domain as the org's primary (OWNER only). Exactly one
// primary per org: the swap is transactional in the store, and only a
// VERIFIED domain can become primary (422 otherwise).
func (s *DomainService) SetPrimary(ctx *gofr.Context, orgID, actorID, id int64) (*models.Domain, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	domain, err := s.getDomain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if domain.Status != models.DomainStatusVerified {
		return nil, apierrors.Unprocessable("only a verified domain can be made primary")
	}

	swapped, err := s.domains.SetPrimary(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if !swapped {
		// The domain lost VERIFIED between our read and the swap; the
		// transaction rolled back and the previous primary survives.
		return nil, apierrors.Unprocessable("only a verified domain can be made primary")
	}

	ctx.Logger.Infof("domain %d (%s) set as primary in org %d by user %d",
		domain.ID, domain.Hostname, orgID, actorID)

	return s.getWithTXT(ctx, orgID, id)
}

// Delete removes the domain (OWNER only). Short URLs in API responses revert
// to the deployment's SHORT_DOMAIN if this was the primary; the links
// themselves are untouched (their codes keep resolving on SHORT_DOMAIN).
func (s *DomainService) Delete(ctx *gofr.Context, orgID, actorID, id int64) error {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return err
	}

	deleted, err := s.domains.Delete(ctx, orgID, id)
	if err != nil {
		return err
	}

	if !deleted {
		return apierrors.NotFound("domain not found")
	}

	ctx.Logger.Infof("domain %d removed from org %d by user %d", id, orgID, actorID)

	return nil
}

// getDomain fetches org-scoped; a cross-org or unknown id is 404 (existence
// stays hidden).
func (s *DomainService) getDomain(ctx *gofr.Context, orgID, id int64) (*models.Domain, error) {
	domain, err := s.domains.GetByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if domain == nil {
		return nil, apierrors.NotFound("domain not found")
	}

	return domain, nil
}

func (s *DomainService) getWithTXT(ctx *gofr.Context, orgID, id int64) (*models.Domain, error) {
	domain, err := s.getDomain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	return withTXTRecord(domain), nil
}

// requireOwner checks the actor's role in the database (not the possibly
// stale token claim) — same rule as org management.
func (s *DomainService) requireOwner(ctx *gofr.Context, orgID, actorID int64) error {
	role, err := s.members.GetRole(ctx, orgID, actorID)
	if err != nil {
		return err
	}

	if role != models.RoleOwner {
		return apierrors.Forbidden("only an org owner can manage custom domains")
	}

	return nil
}

// normalizeHostname validates and canonicalizes a submitted hostname:
// lowercase, punycode (IDNA) normalized, no scheme/path/port/IP-literal, a
// registrable-looking DNS name (at least two labels), and never the
// deployment's own short domain or a subdomain of it.
func (s *DomainService) normalizeHostname(raw string) (string, error) {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")

	switch {
	case h == "":
		return "", apierrors.Unprocessable("hostname must not be empty")
	case strings.Contains(h, "://") || strings.Contains(h, "/"):
		return "", apierrors.Unprocessable("hostname must be a bare DNS name without a scheme or path")
	case strings.Contains(h, ":"):
		return "", apierrors.Unprocessable("hostname must not include a port")
	case strings.ContainsAny(h, " \t@?#&"):
		return "", apierrors.Unprocessable("hostname must be a bare DNS name like links.example.com")
	case net.ParseIP(h) != nil:
		return "", apierrors.Unprocessable("hostname must be a DNS name, not an IP address")
	}

	// Punycode-normalize (IDN labels become xn--…) with strict DNS rules:
	// LDH characters only, no leading/trailing hyphens, label and name
	// length limits.
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil || ascii == "" || len(ascii) > maxHostnameLength {
		return "", apierrors.Unprocessable("hostname is not a valid DNS name")
	}

	if !strings.Contains(ascii, ".") {
		return "", apierrors.Unprocessable("hostname must have at least two labels, like links.example.com")
	}

	// Belt-and-suspenders label check (idna tolerates some shapes, e.g.
	// empty labels): every label must be LDH with no edge hyphens.
	for label := range strings.SplitSeq(ascii, ".") {
		if len(label) > maxLabelLength || !hostLabelRe.MatchString(label) {
			return "", apierrors.Unprocessable("hostname is not a valid DNS name")
		}
	}

	if err := s.rejectDeploymentHost(ascii); err != nil {
		return "", err
	}

	return ascii, nil
}

// rejectDeploymentHost refuses the deployment's own SHORT_DOMAIN and any
// subdomain of it — those hosts already serve the global namespace and must
// never be claimed by one org.
func (s *DomainService) rejectDeploymentHost(hostname string) error {
	if hostname == s.shortHost || strings.HasSuffix(hostname, "."+s.shortHost) {
		return apierrors.Unprocessable("this hostname is the deployment's own short domain (or a subdomain of it)")
	}

	return nil
}

// withTXTRecord fills the computed TXT-record instruction fields.
func withTXTRecord(d *models.Domain) *models.Domain {
	d.TXTRecordName = txtRecordPrefix + d.Hostname
	d.TXTRecordValue = d.VerificationToken

	return d
}

// NormalizeHost canonicalizes a request Host header for comparisons:
// lowercased, port stripped (local dev sends localhost:5810), trailing dot
// removed.
func NormalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	return strings.TrimSuffix(host, ".")
}

// IsDeploymentHost reports whether a (raw or normalized) request Host is the
// deployment's own short domain or a local-dev loopback variant — requests
// there keep the global-namespace behavior. An empty host (HTTP/1.0 clients,
// unit tests) is treated as the deployment host rather than an unknown
// domain.
func IsDeploymentHost(host, shortDomain string) bool {
	h := NormalizeHost(host)

	return h == "" || h == NormalizeHost(shortDomain) ||
		h == "localhost" || h == "127.0.0.1" || h == "::1"
}
