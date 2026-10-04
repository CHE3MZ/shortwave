package kiwi

import "testing"

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"example.com":                "example.com:8073",
		"example.com:8074":           "example.com:8074",
		"ws://example.com:8073":      "example.com:8073",
		"https://example.com:8073/":  "example.com:8073",
		"  kiwisdr.ucsd.edu:8073  ":  "kiwisdr.ucsd.edu:8073",
		"wss://example.com:8073":     "example.com:8073",
		"http://example.com":         "example.com:8073",
	}
	for in, want := range cases {
		if got := NormalizeServer(in); got != want {
			t.Errorf("NormalizeServer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsMode(t *testing.T) {
	for _, m := range ValidModes {
		if !IsMode(m) {
			t.Errorf("IsMode(%q) = false, want true", m)
		}
	}
	if IsMode("ssb") {
		t.Error("IsMode(ssb) = true, want false")
	}
	if IsMode("") {
		t.Error("IsMode(\"\") = true, want false")
	}
}

func TestPassband(t *testing.T) {
	if lo, hi := Passband("usb"); lo != 300 || hi != 3000 {
		t.Errorf("usb passband = %d/%d, want 300/3000", lo, hi)
	}
	if lo, hi := Passband("lsb"); lo != -3000 || hi != -300 {
		t.Errorf("lsb passband = %d/%d, want -3000/-300", lo, hi)
	}
	if lo, hi := Passband("am"); lo != -4000 || hi != 4000 {
		t.Errorf("am passband = %d/%d, want -4000/4000", lo, hi)
	}
	if lo, hi := Passband("bogus"); lo != -4000 || hi != 4000 {
		t.Errorf("unknown mode passband = %d/%d, want am default", lo, hi)
	}
}

func TestDecodeAudioCompressedZero(t *testing.T) {
	c := &Client{}
	got := c.DecodeAudio([]byte{0x00}, FlagCompressed)
	if len(got) != 2 || got[0] != 0 || got[1] != 0 {
		t.Fatalf("compressed zero byte = %v, want [0 0]", got)
	}
}

func TestDecodeAudioUncompressed(t *testing.T) {
	c := &Client{}
	// 0x4000 LE = 16384 -> 0.5
	got := c.DecodeAudio([]byte{0x00, 0x40}, FlagLittleEndian)
	if len(got) != 1 || got[0] != 0.5 {
		t.Fatalf("LE decode = %v, want [0.5]", got)
	}
	// Same bytes as BE = 0x0040 = 64 -> 64/32768
	got = c.DecodeAudio([]byte{0x00, 0x40}, 0)
	if len(got) != 1 || got[0] != float64(64)/32768.0 {
		t.Fatalf("BE decode = %v, want [%v]", got, float64(64)/32768.0)
	}
}

func TestDecoderReset(t *testing.T) {
	var d Decoder
	var out [2]int16
	d.DecodeByte(0xFF, out[:])
	d.Reset()
	if d.index != 0 || d.prev != 0 {
		t.Fatalf("Reset left state index=%d prev=%d", d.index, d.prev)
	}
}
