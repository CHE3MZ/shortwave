package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPathNotEmpty(t *testing.T) {
	if p := Path(); p == "" {
		t.Error("Path() is empty")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	c := &Config{LastServer: "example.com:8073", LastFreq: 7222, LastMode: "lsb"}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Config
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != *c {
		t.Fatalf("round trip = %+v, want %+v", back, *c)
	}
}

func TestLoadEmptyWithoutFiles(t *testing.T) {
	// Load must never fail: worst case it returns an empty config.
	c := Load()
	if c == nil {
		t.Fatal("Load returned nil")
	}
	_ = strings.TrimSpace(c.LastServer) // smoke use; no assertion on user state
}
