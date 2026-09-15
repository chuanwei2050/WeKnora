package types

import "testing"

func TestIsDeliberateParseInterrupt(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{ParseInterruptedForRechunkMessage, true},
		{ParseInterruptedForRebuildMessage, true},
		{ParseInterruptedByUserCancelMessage, true},
		{ParseInterruptedByUserStopMessage, true},
		{ParseStaleInterruptedMessage, false},
		{"lease expired", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsDeliberateParseInterrupt(tc.msg); got != tc.want {
			t.Fatalf("IsDeliberateParseInterrupt(%q)=%v want %v", tc.msg, got, tc.want)
		}
	}
}
