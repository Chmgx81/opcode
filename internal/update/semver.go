package update

import (
	"regexp"
	"strconv"
	"strings"
)

// semverRE is the semver.org grammar with a mandatory "v" prefix and
// without build metadata: the release pipeline only publishes tags of
// this shape (see .github/workflows/release.yml), so anything else —
// "(devel)", a "+dirty" stamp, "latest" — is deliberately rejected.
var semverRE = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)` +
		`(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?$`)

// pseudoRE matches the tail of a Go pseudo-version prerelease
// ("...-0.20250101000000-abcdef123456"). Go 1.24+ stamps those into
// builds made from an untagged commit; they are not releases.
var pseudoRE = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}$`)

type semver struct {
	major, minor, patch uint64
	pre                 []string
}

// parseRelease parses a release tag. Go pseudo-versions parse as
// invalid: a build from an untagged commit is a development build.
func parseRelease(s string) (semver, bool) {
	m := semverRE.FindStringSubmatch(s)
	if m == nil {
		return semver{}, false
	}
	var v semver
	for i, dst := range []*uint64{&v.major, &v.minor, &v.patch} {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return semver{}, false // overflow
		}
		*dst = n
	}
	if m[4] != "" {
		if pseudoRE.MatchString(m[4]) {
			return semver{}, false
		}
		v.pre = strings.Split(m[4], ".")
	}
	return v, true
}

// compare returns -1, 0 or 1 by semver precedence (semver.org §11).
func compare(a, b semver) int {
	for _, p := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if c := cmpUint(p[0], p[1]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1 // a release outranks its own prereleases
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := cmpIdent(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(a.pre)), uint64(len(b.pre)))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmpIdent orders prerelease identifiers: numeric ones numerically and
// below alphanumeric ones, which compare in ASCII order.
func cmpIdent(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		return cmpUint(an, bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
