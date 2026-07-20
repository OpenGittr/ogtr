// Package geo provides optional GeoIP support (ARCHITECTURE.md §6): visitor
// location (city/region/country) resolution from a MaxMind GeoLite2-City
// .mmdb, and city-name prefix search over the GeoLite2 locations CSV for the
// rule-builder autocomplete.
//
// Both datasets are optional and independently configured (GEOIP_DB_PATH /
// GEOIP_CITIES_CSV). When absent the deployment still works: clicks carry no
// location, location rule conditions never match, and /api/v1/cities returns
// 501.
package geo

import (
	"net"

	"github.com/oschwald/geoip2-golang"
)

// Location is the result of one GeoIP lookup: the visitor's city, region
// (first subdivision, e.g. state) and country, all English names, plus the
// ISO 3166-1 country code. Unknown fields are "".
type Location struct {
	City        string
	Region      string
	Country     string
	CountryCode string
}

// Locator resolves visitor IPs to locations. The zero/disabled Locator is
// valid and resolves everything to the zero Location.
type Locator struct {
	db *geoip2.Reader
}

// DisabledLocator is the no-op locator used when GEOIP_DB_PATH is unset.
func DisabledLocator() *Locator { return &Locator{} }

// OpenLocator opens the .mmdb ONCE; the handle is shared for the process
// lifetime and never reopened per lookup (FEATURES.md §9). It is
// deliberately never closed: it lives exactly as long as the process.
func OpenLocator(path string) (*Locator, error) {
	db, err := geoip2.Open(path)
	if err != nil {
		return nil, err
	}

	return &Locator{db: db}, nil
}

// LocationForIP resolves an IP to its city/region/country in ONE database
// lookup, or the zero Location when the locator is disabled, the IP is
// unparseable, or the database has no record for it. It never fails: click
// recording must not block on GeoIP (ARCHITECTURE §4).
func (l *Locator) LocationForIP(ip string) Location {
	if l == nil || l.db == nil {
		return Location{}
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return Location{}
	}

	record, err := l.db.City(parsed)
	if err != nil {
		return Location{}
	}

	loc := Location{
		City:        record.City.Names["en"],
		Country:     record.Country.Names["en"],
		CountryCode: record.Country.IsoCode,
	}
	if len(record.Subdivisions) > 0 {
		loc.Region = record.Subdivisions[0].Names["en"]
	}

	return loc
}

// CityForIP returns the English city name for an IP; see LocationForIP.
func (l *Locator) CityForIP(ip string) string {
	return l.LocationForIP(ip).City
}
