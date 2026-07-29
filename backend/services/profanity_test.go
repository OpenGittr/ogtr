package services

import "testing"

func TestContainsExplicit(t *testing.T) {
	blocked := []string{
		"fuck", "my-fuck-link", "f_u_c_k", "sh1t", "SHIT",
		"faggot", "n1gger", "a55hole", "dumbass", "pussycat",
	}
	for _, a := range blocked {
		if !containsExplicit(a) {
			t.Errorf("containsExplicit(%q) = false, want true", a)
		}
	}

	// Innocent aliases that merely contain a short offensive root must pass —
	// the blocklist uses compound forms (asshole, not ass) to avoid these.
	clean := []string{
		"document", "class-notes", "grape-soda", "assistant",
		"cucumber", "peacock-tickets", "bass-guitar",
		"my-brand", "summer-sale", "spring24",
	}
	for _, a := range clean {
		if containsExplicit(a) {
			t.Errorf("containsExplicit(%q) = true, want false (false positive)", a)
		}
	}
}
