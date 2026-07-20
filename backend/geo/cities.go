package geo

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// cityNameColumn is the city_name column of the GeoLite2-City locations CSV.
const cityNameColumn = "city_name"

// Cities is the in-memory city-name index for the rule-builder autocomplete
// (GET /api/v1/cities?q=). The CSV is read ONCE at startup into a sorted,
// deduplicated slice (~100k unique names, a few MB) — never re-read per
// request. The zero/disabled Cities is valid and reports Enabled() == false.
type Cities struct {
	names   []string // sorted, unique
	lowered []string // names[i] lowercased, for case-insensitive prefix match
}

// DisabledCities is the no-op index used when GEOIP_CITIES_CSV is unset.
func DisabledCities() *Cities { return &Cities{} }

// LoadCities reads the GeoLite2 locations CSV (header row locates the
// city_name column), deduplicates the non-empty city names and sorts them
// for binary-searched prefix lookups.
func LoadCities(path string) (*Cities, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // tolerate ragged rows; we only need one column

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading csv header: %w", err)
	}

	col := -1

	for i, name := range header {
		if strings.EqualFold(strings.TrimSpace(name), cityNameColumn) {
			col = i

			break
		}
	}

	if col == -1 {
		return nil, fmt.Errorf("csv %s has no %q column", path, cityNameColumn)
	}

	seen := map[string]struct{}{}

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("reading csv row: %w", err)
		}

		if col < len(row) {
			if name := strings.TrimSpace(row[col]); name != "" {
				seen[name] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}

	sort.Strings(names)

	lowered := make([]string, len(names))
	for i, name := range names {
		lowered[i] = strings.ToLower(name)
	}

	return &Cities{names: names, lowered: lowered}, nil
}

// Enabled reports whether a city dataset is loaded.
func (c *Cities) Enabled() bool { return c != nil && len(c.names) > 0 }

// Search returns up to limit city names starting with prefix,
// case-insensitively, in alphabetical order. An empty prefix returns nothing
// (the autocomplete only fires on typed input).
func (c *Cities) Search(prefix string, limit int) []string {
	results := []string{}

	if !c.Enabled() || limit <= 0 {
		return results
	}

	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return results
	}

	// The index is sorted case-sensitively, so a case-insensitive prefix scan
	// can't binary-search; a linear scan over ~100k pre-lowered strings is
	// still well under a millisecond.
	for i, low := range c.lowered {
		if strings.HasPrefix(low, prefix) {
			results = append(results, c.names[i])

			if len(results) == limit {
				break
			}
		}
	}

	return results
}
