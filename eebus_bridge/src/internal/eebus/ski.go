package eebus

import "strings"

func normaliseSKI(ski string) string {
	ski = strings.ToLower(strings.TrimSpace(ski))
	return strings.NewReplacer(" ", "", ":", "", "-", "").Replace(ski)
}

func validSKI(ski string) bool {
	if len(ski) != 40 {
		return false
	}
	for _, r := range ski {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
