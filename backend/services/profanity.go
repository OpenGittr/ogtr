package services

import "strings"

// explicitWords is a small blocklist used to keep profane custom aliases off
// the shared short domain (see validateAlias). It is deliberately conservative:
// clearly-offensive terms in compound form (asshole, dumbass, dickhead,
// cocksuck) rather than bare short roots (ass, dick, cock), to avoid the
// "Scunthorpe problem" of flagging innocent words that merely contain a root.
// Not meant to be exhaustive — content filters never are — just a sane default.
var explicitWords = []string{
	"fuck", "shit", "bitch", "cunt", "asshole", "dumbass", "jackass",
	"bastard", "dickhead", "pussy", "cocksuck", "nigger", "nigga",
	"faggot", "whore", "nazi", "porn", "dildo", "jizz", "wank",
	"blowjob", "handjob", "boner", "titties",
}

// leetReplacer collapses common leetspeak substitutions so "sh1t" / "f4ggot"
// are caught. Applied after lowercasing and stripping alias separators.
var leetReplacer = strings.NewReplacer(
	"-", "", "_", "",
	"0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "@", "a", "$", "s",
)

// containsExplicit reports whether the alias contains a blocklisted word after
// normalization, so "my-fuck-link", "f_u_c_k" and "sh1t" all match.
func containsExplicit(alias string) bool {
	norm := leetReplacer.Replace(strings.ToLower(alias))

	for _, w := range explicitWords {
		if strings.Contains(norm, w) {
			return true
		}
	}

	return false
}
