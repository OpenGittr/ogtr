package geo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleCSV = `geoname_id,locale_code,continent_code,continent_name,country_iso_code,country_name,subdivision_1_iso_code,subdivision_1_name,subdivision_2_iso_code,subdivision_2_name,city_name,metro_code,time_zone
1,en,AS,Asia,IN,India,KA,Karnataka,,,Bengaluru,,Asia/Kolkata
2,en,AS,Asia,IN,India,DL,Delhi,,,Delhi,,Asia/Kolkata
3,en,AS,Asia,IN,India,DL,Delhi,,,New Delhi,,Asia/Kolkata
4,en,EU,Europe,NL,Netherlands,ZH,South Holland,,,Delft,,Europe/Amsterdam
5,en,AS,Asia,IN,India,DL,Delhi,,,Delhi,,Asia/Kolkata
6,en,AF,Africa,RW,Rwanda,,,,,,,Africa/Kigali
`

func writeCSV(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cities.csv")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestLoadCities_DedupesAndSorts(t *testing.T) {
	cities, err := LoadCities(writeCSV(t, sampleCSV))

	require.NoError(t, err)
	assert.True(t, cities.Enabled())
	// Duplicate "Delhi" collapses; the empty city_name row is skipped.
	assert.Equal(t, []string{"Bengaluru", "Delft", "Delhi", "New Delhi"}, cities.names)
}

func TestCities_Search(t *testing.T) {
	cities, err := LoadCities(writeCSV(t, sampleCSV))
	require.NoError(t, err)

	tests := []struct {
		desc   string
		prefix string
		limit  int
		want   []string
	}{
		{"case-insensitive prefix", "del", 20, []string{"Delft", "Delhi"}},
		{"upper-case prefix", "DELH", 20, []string{"Delhi"}},
		{"limit caps results", "del", 1, []string{"Delft"}},
		{"prefix with surrounding space", " new ", 20, []string{"New Delhi"}},
		{"no match", "zzz", 20, []string{}},
		{"empty prefix returns nothing", "", 20, nil},
		{"zero limit returns nothing", "del", 0, []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := cities.Search(tc.prefix, tc.limit)

			if len(tc.want) == 0 {
				assert.Empty(t, got)

				return
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLoadCities_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadCities(filepath.Join(t.TempDir(), "absent.csv"))
		require.Error(t, err)
	})

	t.Run("missing city_name column", func(t *testing.T) {
		_, err := LoadCities(writeCSV(t, "a,b,c\n1,2,3\n"))
		require.Error(t, err)
	})
}

func TestDisabledCities(t *testing.T) {
	cities := DisabledCities()

	assert.False(t, cities.Enabled())
	assert.Empty(t, cities.Search("del", 20))
}

func TestDisabledLocator(t *testing.T) {
	assert.Empty(t, DisabledLocator().CityForIP("8.8.8.8"))
	assert.Equal(t, Location{}, DisabledLocator().LocationForIP("8.8.8.8"))
}

func TestOpenLocator_MissingFile(t *testing.T) {
	_, err := OpenLocator(filepath.Join(t.TempDir(), "absent.mmdb"))
	require.Error(t, err)
}

func TestLocator_CityForIP_BadInput(t *testing.T) {
	// A disabled locator must tolerate any input without panicking.
	l := DisabledLocator()

	assert.Empty(t, l.CityForIP("not-an-ip"))
	assert.Empty(t, l.CityForIP(""))
	assert.Equal(t, Location{}, l.LocationForIP("not-an-ip"))
	assert.Equal(t, Location{}, l.LocationForIP(""))
}
