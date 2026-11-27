package execsvc

import (
	"testing"
	"time"
)

// HumanDuration writes a duration as the `.mh` literal that would declare it.
func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * 24 * time.Hour, "30d"},
		{36 * time.Hour, "36h"},
		{3 * time.Minute, "3m"},
		{90 * time.Second, "90s"},
		{2500 * time.Millisecond, "2500ms"},
		{200 * time.Millisecond, "200ms"},
		{0, "0s"},
		{1500 * time.Microsecond, "1.5ms"}, // no literal for it: Go's form
	}
	for _, tc := range cases {
		if got := HumanDuration(tc.in); got != tc.want {
			t.Errorf("HumanDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
