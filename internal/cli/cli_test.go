package cli

import (
	"reflect"
	"testing"
)

func TestNormalizeArgs(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"--example"}, []string{"--example=1"}},
		{[]string{"--example", "2"}, []string{"--example", "2"}},
		{[]string{"--example", "foo"}, []string{"--example=1", "foo"}},
		{[]string{"--random", "--example"}, []string{"--random", "--example=1"}},
		{[]string{"host:8073", "-f", "9650"}, []string{"host:8073", "-f", "9650"}},
	}
	for _, c := range cases {
		if got := NormalizeArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizeArgs(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestExamplePreset(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		f, m, v, ok := examplePreset(n)
		if !ok || f <= 0 || m == "" || v <= 0 {
			t.Errorf("examplePreset(%d) = %v %q %v %v, want valid preset", n, f, m, v, ok)
		}
	}
	if _, _, _, ok := examplePreset(99); ok {
		t.Error("examplePreset(99) ok = true, want false")
	}
}

func TestClampVolume(t *testing.T) {
	if clampVolume(-1) != 0 || clampVolume(101) != 100 || clampVolume(50) != 50 {
		t.Error("clampVolume failed to clamp 0-100")
	}
}
