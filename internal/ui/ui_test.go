package ui

import (
	"strings"
	"testing"
)

func withPlain(t *testing.T, fn func()) {
	t.Helper()
	SetNoColor(true)
	defer SetNoColor(false)
	fn()
}

func TestBannerContainsVersion(t *testing.T) {
	withPlain(t, func() {
		if got := Banner("9.9.9"); !strings.Contains(got, "9.9.9") {
			t.Errorf("Banner missing version: %q", got)
		}
	})
}

func TestStatusPlainFields(t *testing.T) {
	withPlain(t, func() {
		got := Status(7222, "lsb", 25, 500, 12000, true)
		for _, want := range []string{"7222", "lsb", "25", "500", "12000", "live"} {
			if !strings.Contains(got, want) {
				t.Errorf("Status missing %q: %q", want, got)
			}
		}
		got = Status(7222, "lsb", 25, 500, 12000, false)
		if !strings.Contains(got, "connecting") {
			t.Errorf("disconnected Status should mention connecting: %q", got)
		}
	})
}

func TestSignalBar(t *testing.T) {
	withPlain(t, func() {
		empty := SignalBar(0)
		full := SignalBar(2000)
		if empty == full {
			t.Errorf("empty and full bars identical: %q", empty)
		}
		if !strings.HasPrefix(empty, "[") {
			t.Errorf("plain bar should be bracketed: %q", empty)
		}
	})
}

func TestScanRow(t *testing.T) {
	withPlain(t, func() {
		got := ScanRow(7100, 600)
		if !strings.Contains(got, "7100") || !strings.Contains(got, "600") {
			t.Errorf("ScanRow missing fields: %q", got)
		}
	})
}

func TestSignalDBM(t *testing.T) {
	if got := SignalDBM(0); got != -127 {
		t.Errorf("SignalDBM(0) = %v, want -127", got)
	}
}
