package update

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// version is major.minor.patch with optional prerelease identifiers,
// ordered by Semantic Versioning 2.0. Build metadata is not allowed: tags
// and the version in Taskfile.yml never carry it.
type version struct {
	text        string
	core        [3]int
	identifiers []string
}

var versionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func parseVersion(text string) (version, error) {
	m := versionPattern.FindStringSubmatch(text)
	if m == nil {
		return version{}, fmt.Errorf("无效的版本号 %q", text)
	}
	v := version{text: text}
	for i := range v.core {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, fmt.Errorf("无效的版本号 %q", text)
		}
		v.core[i] = n
	}
	if m[4] != "" {
		v.identifiers = strings.Split(m[4], ".")
	}
	return v, nil
}

func (v version) String() string { return v.text }

// prerelease reports whether v is an alpha, beta or release candidate.
func (v version) prerelease() bool { return len(v.identifiers) > 0 }

// compare returns -1, 0 or 1 as v is older than, equal to or newer than w.
func (v version) compare(w version) int {
	for i := range v.core {
		if v.core[i] != w.core[i] {
			return cmp.Compare(v.core[i], w.core[i])
		}
	}
	// A prerelease comes before the release it leads to.
	if v.prerelease() != w.prerelease() {
		if v.prerelease() {
			return -1
		}
		return 1
	}
	for i := range min(len(v.identifiers), len(w.identifiers)) {
		if c := compareIdentifier(v.identifiers[i], w.identifiers[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(v.identifiers), len(w.identifiers))
}

// compareIdentifier orders numeric identifiers by value and before
// alphanumeric ones, which are ordered as text.
func compareIdentifier(a, b string) int {
	x, xerr := strconv.Atoi(a)
	y, yerr := strconv.Atoi(b)
	switch {
	case xerr == nil && yerr == nil:
		return cmp.Compare(x, y)
	case xerr == nil:
		return -1
	case yerr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
