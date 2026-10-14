package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version: 1.2.3 or 1.2.3-beta.1.
type Version struct {
	Nums [3]int
	Pre  []string
}

// ParseVersion reads a version, with or without a leading v.
func ParseVersion(s string) (Version, error) {
	var v Version
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	main, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("update: bad version %q", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, fmt.Errorf("update: bad version %q", s)
		}
		v.Nums[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, fmt.Errorf("update: bad version %q", s)
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, nil
}

// Compare orders versions as semantic versioning does: -1, 0 or 1.
func Compare(a, b Version) int {
	for i := range 3 {
		if a.Nums[i] != b.Nums[i] {
			if a.Nums[i] < b.Nums[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.Pre) == 0 && len(b.Pre) == 0:
		return 0
	case len(a.Pre) == 0:
		return 1
	case len(b.Pre) == 0:
		return -1
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		x, y := a.Pre[i], b.Pre[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a.Pre) < len(b.Pre):
		return -1
	case len(a.Pre) > len(b.Pre):
		return 1
	}
	return 0
}

// Newer reports whether candidate is a later version than current.
func Newer(candidate, current string) bool {
	a, err1 := ParseVersion(candidate)
	b, err2 := ParseVersion(current)
	return err1 == nil && err2 == nil && Compare(a, b) > 0
}

// IsPrerelease reports whether v has a pre-release part.
func IsPrerelease(v string) bool {
	p, err := ParseVersion(v)
	return err == nil && len(p.Pre) > 0
}
