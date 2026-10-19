package handler

import (
	"errors"
	"regexp"
	"time"
)

// Check the wire format before Go's deliberately permissive timestamp parser.
var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func parseTimestamp(value string) (time.Time, error) {
	if !timestampPattern.MatchString(value) {
		return time.Time{}, errors.New("timestamp must use RFC3339")
	}
	date, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("timestamp must use RFC3339")
	}
	date = date.UTC()
	if date.Year() < 0 || date.Year() > 9999 {
		return time.Time{}, errors.New("timestamp must remain within years 0000 through 9999 in UTC")
	}
	return date, nil
}
