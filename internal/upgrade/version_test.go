package upgrade

import (
	"testing"
	"time"
)

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.107.79", "v0.107.70", true},
		{"v0.107.70", "v0.107.79", false},
		{"v0.108.0", "v0.107.99", true},
		{"v0.108.0", "v0.108.0-b.5", true},
		{"v0.108.0-b.5", "v0.108.0", false},
		{"0.107.79", "v0.107.79", false},
		{"garbage", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if !SameVersion("0.107.79", "v0.107.79") {
		t.Error("SameVersion should ignore the v prefix")
	}
}

func TestWindow(t *testing.T) {
	loc := time.UTC
	sun := time.Date(2026, 11, 1, 3, 30, 0, 0, loc) // a Sunday
	w := Window{Days: []int{0}, Start: "03:00", End: "05:00"}
	if !w.Contains(sun) {
		t.Error("Sunday 03:30 should be inside")
	}
	if w.Contains(sun.Add(2 * time.Hour)) {
		t.Error("05:30 should be outside")
	}
	if w.Contains(sun.Add(24 * time.Hour)) {
		t.Error("Monday should be outside")
	}
	wrap := Window{Days: []int{6}, Start: "23:00", End: "02:00"} // Saturday night
	if !wrap.Contains(time.Date(2026, 10, 31, 23, 30, 0, 0, loc)) {
		t.Error("Saturday 23:30 should be inside")
	}
	if !wrap.Contains(time.Date(2026, 11, 1, 1, 0, 0, 0, loc)) {
		t.Error("Sunday 01:00 continues Saturday's window")
	}
	if wrap.Contains(time.Date(2026, 11, 1, 23, 30, 0, 0, loc)) {
		t.Error("Sunday 23:30 is not a window day")
	}
	if (Window{Days: []int{1}, Start: "3:00", End: "3:00"}).Validate() == nil {
		t.Error("zero-length window should be invalid")
	}
}
