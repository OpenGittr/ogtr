// Package scanner is the URL-scanning seam that keeps abusive destinations
// out of the deployment (mirroring the auth-provider and limits seams:
// a small interface, composable implementations, wired once in main.go).
//
// A destination URL is scanned on link creation and on destination edits,
// and periodically re-scanned for recently clicked links. The pipeline
// composes independent layers:
//
//   - Syntactic guards (always on): IP-literal/private hosts, non-http(s)
//     schemes, userinfo@ tricks, mixed-script (homograph) labels, and
//     shortener chaining.
//   - Feed-based blocklists (BLOCKLIST_FEED_URLS): plaintext host/URL feeds
//     such as URLhaus or OpenPhish, refreshed on an interval, last-good kept
//     on fetch failure.
//   - Google Web Risk (WEBRISK_API_KEY): an external lookup with a hard
//     timeout that FAILS OPEN — the local layers are the floor, the external
//     service only adds coverage.
//
// A flagged verdict carries only a coarse category (malware | phishing |
// abuse | policy); which list or rule matched is never revealed to clients.
package scanner

import "context"

// Coarse verdict categories. Clients only ever see these — never the
// specific rule, list or feed that matched.
const (
	CategoryMalware  = "malware"
	CategoryPhishing = "phishing"
	CategoryAbuse    = "abuse"
	CategoryPolicy   = "policy"
)

// Verdict is the outcome of scanning one URL.
type Verdict struct {
	// Allowed is true when no layer flagged the URL.
	Allowed bool
	// Category is the coarse reason when flagged; empty when allowed.
	Category string
}

// Allow is the clean verdict.
func Allow() Verdict { return Verdict{Allowed: true} }

// Flag builds a flagged verdict with a coarse category.
func Flag(category string) Verdict { return Verdict{Category: category} }

// Scanner decides whether a destination URL may be shortened.
type Scanner interface {
	// Scan checks one destination URL. A non-nil error means this layer
	// could not decide (the pipeline treats that as allow for the layer —
	// fail-open per layer, with the syntactic floor always deciding).
	Scan(ctx context.Context, rawURL string) (Verdict, error)
}

// Logger is the minimal logging surface the scanners need; gofr's
// logging.Logger satisfies it.
type Logger interface {
	Errorf(format string, args ...any)
	Warnf(format string, args ...any)
	Infof(format string, args ...any)
}

// Pipeline composes scanners in order; the first flagged verdict wins. A
// layer returning an error is logged and skipped (that layer fails open),
// so an unreachable external service can never block link creation outright
// — the syntactic layer never errors and is the guaranteed floor.
type Pipeline struct {
	scanners []Scanner
	log      Logger
}

// NewPipeline builds a Pipeline over the given layers.
func NewPipeline(log Logger, scanners ...Scanner) *Pipeline {
	return &Pipeline{scanners: scanners, log: log}
}

// Scan implements Scanner.
func (p *Pipeline) Scan(ctx context.Context, rawURL string) (Verdict, error) {
	for _, s := range p.scanners {
		verdict, err := s.Scan(ctx, rawURL)
		if err != nil {
			if p.log != nil {
				p.log.Errorf("scanner layer %T failed (layer skipped, fail-open): %v", s, err)
			}

			continue
		}

		if !verdict.Allowed {
			return verdict, nil
		}
	}

	return Allow(), nil
}
