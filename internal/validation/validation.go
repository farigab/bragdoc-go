// Package validation provides small helpers to validate inputs and parse dates.
package validation

import (
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode"
)

// ParseISODate parses a date in YYYY-MM-DD format.
func ParseISODate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// ValidateDateRange parses start and end and checks that start <= end when both provided.
func ValidateDateRange(start, end string) (time.Time, time.Time, error) {
	s, err := ParseISODate(start)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	e, err := ParseISODate(end)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	// When users provide a date without a time (YYYY-MM-DD) they expect the
	// entire day to be included. Treat the parsed end date as the end of the
	// day (23:59:59.999999999) so range queries are inclusive of that day.
	if !e.IsZero() {
		e = e.Add(24*time.Hour - time.Nanosecond)
	}

	if !s.IsZero() && !e.IsZero() && s.After(e) {
		return time.Time{}, time.Time{}, errors.New("startDate must be before or equal endDate")
	}
	return s, e, nil
}

// ValidateRepositories ensures the repositories slice has no empty values.
func ValidateRepositories(repos []string) error {
	if len(repos) == 0 {
		return nil // allow empty — caller decides if required
	}
	if slices.Contains(repos, "") {
		return errors.New("repositories cannot contain empty values")
	}
	return nil
}

// ValidateGitHubToken ensures the token only contains characters expected in a
// GitHub personal access token (alphanumeric, underscore, hyphen) and is within
// a reasonable length. This is a defence-in-depth check before the value reaches
// a raw SQL string — the driver does not expose prepared statements.
func ValidateGitHubToken(token string) error {
	if token == "" {
		return errors.New("token must not be empty")
	}
	const maxLen = 512
	if len(token) > maxLen {
		return fmt.Errorf("token exceeds maximum length of %d characters", maxLen)
	}
	for i, c := range token {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '-' {
			return fmt.Errorf("token contains invalid character %q at position %d", c, i)
		}
	}
	return nil
}
