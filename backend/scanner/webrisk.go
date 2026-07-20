package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultWebRiskBaseURL is Google Web Risk's production endpoint; tests
// substitute an httptest server.
const DefaultWebRiskBaseURL = "https://webrisk.googleapis.com"

// webRiskTimeout is the hard budget for one lookup: link creation must not
// hang on an external service. Past it the layer fails open.
const webRiskTimeout = 2 * time.Second

// WebRisk is the optional Google Web Risk lookup layer (WEBRISK_API_KEY).
// It FAILS OPEN: an unreachable, slow or erroring API allows the URL and
// logs — the syntactic and feed layers are the local floor, Web Risk only
// adds coverage on top.
type WebRisk struct {
	apiKey  string
	baseURL string
	client  *http.Client
	log     Logger
}

// NewWebRisk builds the layer. baseURL "" means the production endpoint;
// client nil means a default client (the per-lookup timeout applies either
// way).
func NewWebRisk(apiKey, baseURL string, client *http.Client, log Logger) *WebRisk {
	if baseURL == "" {
		baseURL = DefaultWebRiskBaseURL
	}

	if client == nil {
		client = &http.Client{}
	}

	return &WebRisk{apiKey: apiKey, baseURL: baseURL, client: client, log: log}
}

// webRiskResponse is the uris:search response shape: an empty object for a
// clean URL, or a threat with its matched types.
type webRiskResponse struct {
	Threat struct {
		ThreatTypes []string `json:"threatTypes"`
	} `json:"threat"`
}

// Scan implements Scanner. It never returns an error — every failure mode
// logs and allows (documented fail-open).
func (w *WebRisk) Scan(ctx context.Context, rawURL string) (Verdict, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, webRiskTimeout)
	defer cancel()

	q := url.Values{}
	q.Set("key", w.apiKey)
	q.Set("uri", rawURL)
	q["threatTypes"] = []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE"}

	endpoint := w.baseURL + "/v1/uris:search?" + q.Encode()

	req, err := http.NewRequestWithContext(lookupCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		w.failOpen(fmt.Errorf("building request: %w", err))

		return Allow(), nil
	}

	resp, err := w.client.Do(req)
	if err != nil {
		w.failOpen(err)

		return Allow(), nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		w.failOpen(fmt.Errorf("API answered HTTP %d", resp.StatusCode))

		return Allow(), nil
	}

	var parsed webRiskResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		w.failOpen(err)

		return Allow(), nil
	}

	if len(parsed.Threat.ThreatTypes) == 0 {
		return Allow(), nil
	}

	return Flag(webRiskCategory(parsed.Threat.ThreatTypes)), nil
}

// webRiskCategory maps Web Risk threat types onto the coarse categories.
func webRiskCategory(threatTypes []string) string {
	category := CategoryAbuse // UNWANTED_SOFTWARE and anything unrecognized

	for _, t := range threatTypes {
		switch t {
		case "MALWARE":
			return CategoryMalware // strongest signal wins immediately
		case "SOCIAL_ENGINEERING":
			category = CategoryPhishing
		}
	}

	return category
}

// failOpen logs an unavailable-service allow. Deliberately not an error
// return: the pipeline would only log it the same way, and callers must
// never treat Web Risk unavailability as a scan failure.
func (w *WebRisk) failOpen(err error) {
	if w.log != nil {
		w.log.Errorf("web risk lookup failed (fail-open, URL allowed by this layer): %v", err)
	}
}
