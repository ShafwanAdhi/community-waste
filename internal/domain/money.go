package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Money stores hundredths of a rupiah, matching NUMERIC(12,2) without floats.
type Money int64

func ParseMoney(value string) (Money, error) {
	whole, fraction, hasFraction := strings.Cut(value, ".")
	if len(whole) == 0 || len(whole) > 10 || (hasFraction && (len(fraction) < 1 || len(fraction) > 2)) {
		return 0, errors.New("amount must be a positive decimal with at most two fractional digits")
	}
	for _, part := range []string{whole, fraction} {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, errors.New("amount must contain decimal digits")
			}
		}
	}
	units, _ := strconv.ParseInt(whole, 10, 64)
	cents := int64(0)
	if hasFraction {
		if len(fraction) == 1 {
			fraction += "0"
		}
		cents, _ = strconv.ParseInt(fraction, 10, 64)
	}
	amount := Money(units*100 + cents)
	if amount <= 0 {
		return 0, errors.New("amount must be positive")
	}
	return amount, nil
}

func (m Money) String() string {
	if m < 0 {
		return fmt.Sprintf("-%d.%02d", -(m / 100), -(m % 100))
	}
	return fmt.Sprintf("%d.%02d", m/100, m%100)
}

func (m Money) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, m.String()), nil
}
