package bands

import "testing"

func TestPickRandomFreqValid(t *testing.T) {
	for i := 0; i < 2000; i++ {
		f, m := PickRandomFreq(30000)
		if f <= 0 || f >= 30000 {
			t.Fatalf("freq %v out of range for 30 MHz receiver", f)
		}
		if m == "" {
			t.Fatalf("pickRandomFreq returned empty mode")
		}
	}
}

func TestPickRandomFreqClamped(t *testing.T) {
	// A receiver that only tunes to 7 MHz must never return a freq above that.
	for i := 0; i < 2000; i++ {
		f, _ := PickRandomFreq(7000)
		if f <= 0 || f >= 7000 {
			t.Fatalf("freq %v out of range for 7 MHz receiver", f)
		}
	}
}

func TestPickRandomFreqZeroMax(t *testing.T) {
	// Unknown range falls back to a safe default; still valid.
	f, m := PickRandomFreq(0)
	if f <= 0 || f >= 30000 {
		t.Fatalf("freq %v out of range with unknown max", f)
	}
	if m == "" {
		t.Fatalf("empty mode")
	}
}

func TestPickVoiceFreqClamped(t *testing.T) {
	for i := 0; i < 500; i++ {
		f, m := PickVoiceFreq(7300)
		if f <= 0 || f >= 7300 {
			t.Fatalf("freq %v out of range for 7.3 MHz receiver", f)
		}
		if m == "" {
			t.Fatalf("empty mode")
		}
	}
}
