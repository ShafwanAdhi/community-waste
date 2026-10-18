package domain

import (
	"encoding/json"
	"testing"
)

func TestMoneyParseAndJSON(t *testing.T) {
	for _, tc := range []struct {
		input, canonical string
		value            Money
	}{
		{"50000", "50000.00", 5000000}, {"50000.0", "50000.00", 5000000}, {"100000.00", "100000.00", 10000000}, {"0.01", "0.01", 1}, {"9999999999.99", "9999999999.99", 999999999999},
	} {
		got, err := ParseMoney(tc.input)
		if err != nil || got != tc.value || got.String() != tc.canonical {
			t.Fatalf("%q: %v %v", tc.input, got, err)
		}
		data, err := json.Marshal(got)
		if err != nil || string(data) != `"`+tc.canonical+`"` {
			t.Fatalf("JSON: %s %v", data, err)
		}
	}
	for _, input := range []string{"", "0", "0.00", "-1.00", "+50000", " 50000.00", "50000.00 ", "5e4", "NaN", "Infinity", "50000.001", ".50", "1.", "10000000000.00", "1,00", "1\x00", "one"} {
		if _, err := ParseMoney(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
