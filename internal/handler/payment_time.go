package handler

import (
	"regexp"
	"time"

	"community-waste/internal/domain"
)

// Check the wire format before Go's deliberately permissive timestamp parser.
var paymentTimestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func parsePaymentTimestamp(value string) (time.Time, error) {
	if !paymentTimestampPattern.MatchString(value) {
		return time.Time{}, domain.ErrInvalidFilter
	}
	date, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, domain.ErrInvalidFilter
	}
	return date.UTC(), nil
}
