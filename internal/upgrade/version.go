// Package upgrade keeps AdGuard Home itself up to date across the fleet:
// it tracks releases, talks to each instance's upgrade driver (AdGuard Home's
// built-in updater, the Docker API or SSH) and runs rolling upgrades with
// health gates and automatic rollback.
package upgrade

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Version is a parsed AdGuard Home version such as v0.107.79 or v0.108.0-b.5.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

// ParseVersion parses "v0.107.79" (the leading v is optional).
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, false
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2], pre}, true
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Compare returns -1, 0 or 1. A pre-release sorts before its release.
func (v Version) Compare(o Version) int {
	for _, d := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	default:
		return 1
	}
}

// Newer reports whether a is a strictly newer version than b. Unparseable
// versions are never newer.
func Newer(a, b string) bool {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	return ok1 && ok2 && va.Compare(vb) > 0
}

// SameVersion compares versions ignoring a leading v.
func SameVersion(a, b string) bool {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	if !ok1 || !ok2 {
		return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
	}
	return va.Compare(vb) == 0
}

// Window is a weekly maintenance window in the server's local time. End
// before Start wraps past midnight (e.g. 23:00–02:00); the day is the day the
// window starts.
type Window struct {
	Days  []int  `json:"days"`  // 0 = Sunday
	Start string `json:"start"` // HH:MM
	End   string `json:"end"`   // HH:MM
}

func parseClock(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	return hh*60 + mm, nil
}

// Validate checks the window.
func (w Window) Validate() error {
	if len(w.Days) == 0 {
		return fmt.Errorf("pick at least one day")
	}
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			return fmt.Errorf("day %d out of range", d)
		}
	}
	a, err := parseClock(w.Start)
	if err != nil {
		return err
	}
	b, err := parseClock(w.End)
	if err != nil {
		return err
	}
	if a == b {
		return fmt.Errorf("the window must not start and end at the same time")
	}
	return nil
}

// Contains reports whether t falls inside the window.
func (w Window) Contains(t time.Time) bool {
	a, err1 := parseClock(w.Start)
	b, err2 := parseClock(w.End)
	if err1 != nil || err2 != nil {
		return false
	}
	now := t.Hour()*60 + t.Minute()
	day := int(t.Weekday())
	has := func(d int) bool {
		for _, x := range w.Days {
			if x == d {
				return true
			}
		}
		return false
	}
	if a < b {
		return has(day) && now >= a && now < b
	}
	// Wraps midnight: today's evening part, or yesterday's window continuing.
	return (has(day) && now >= a) || (has((day+6)%7) && now < b)
}
