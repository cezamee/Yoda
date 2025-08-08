// Utility functions shared across services
package services

import "strings"

func hasWildcards(paths []string) bool {
	for _, path := range paths {
		if strings.ContainsAny(path, "*?[]") {
			return true
		}
	}
	return false
}
