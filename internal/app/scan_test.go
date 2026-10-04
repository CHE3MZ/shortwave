package app

import (
	"testing"

	"github.com/CHE3MZ/shortwave/internal/bands"
)

func TestScanSegmentsBand(t *testing.T) {
	segs, step, err := ScanSegments("7100-7300")
	if err != nil {
		t.Fatalf("ScanSegments: %v", err)
	}
	if len(segs) != 1 || segs[0].Lo != 7100 || segs[0].Hi != 7300 {
		t.Fatalf("unexpected segments: %+v", segs)
	}
	if step != ScanStepBand {
		t.Fatalf("step = %v, want %v", step, ScanStepBand)
	}
}

func TestScanSegmentsDefault(t *testing.T) {
	segs, step, err := ScanSegments("")
	if err != nil {
		t.Fatalf("ScanSegments: %v", err)
	}
	if len(segs) == 0 {
		t.Fatal("default scan should cover at least one band")
	}
	if step != ScanStepDefault {
		t.Fatalf("step = %v, want %v", step, ScanStepDefault)
	}
}

func TestScanSegmentsBad(t *testing.T) {
	for _, bad := range []string{"abc", "7300-7100", "7100", "-5-3", "7100-7100"} {
		if _, _, err := ScanSegments(bad); err == nil {
			t.Errorf("ScanSegments(%q) = nil error, want error", bad)
		}
	}
}

func TestExpectedSteps(t *testing.T) {
	segs := []bands.Band{{Lo: 7100, Hi: 7200, Mode: "am"}}
	// (7200-7100)/2.5 = 40
	if n := ExpectedSteps(segs, ScanStepBand); n != 40 {
		t.Fatalf("ExpectedSteps = %d, want 40", n)
	}
}

func TestAppTuneClamps(t *testing.T) {
	a := New(10000, "am", 80)
	a.Tune(-5)
	if f, _, _, _, _, _ := a.Snapshot(); f != 0 {
		t.Fatalf("negative tune = %v, want 0", f)
	}
}
