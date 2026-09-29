package update

import "testing"

func TestParseReleaseAccepts(t *testing.T) {
	for _, s := range []string{
		"v0.0.1", "v1.2.3", "v10.20.30", "v1.0.0-rc.1", "v1.0.0-alpha", "v1.0.0-0.3.7", "v1.0.0-x-y.z",
	} {
		if _, ok := parseRelease(s); !ok {
			t.Errorf("parseRelease(%q) rejected a valid tag", s)
		}
	}
}

func TestParseReleaseRejects(t *testing.T) {
	for _, s := range []string{
		"", "(devel)", "dev", "latest", "1.0.0", "v1.0", "v1.0.0.0", "v1.0.0+dirty", "v1.0.0+build.5",
		"v01.0.0", "v1.0.0-", "v1.0.0-01", "v1.0.0-rc..1", " v1.0.0", "v1.0.0\n",
		"v1.0.0/../x", "v99999999999999999999.0.0",
		// Go 1.24+ stamps these into builds from an untagged commit.
		"v0.2.1-0.20250101000000-abcdef123456",
		"v0.2.1-0.20250101000000-abcdef123456+dirty",
	} {
		if _, ok := parseRelease(s); ok {
			t.Errorf("parseRelease(%q) accepted a non-release version", s)
		}
	}
}

func TestCompareOrdering(t *testing.T) {
	// Each list is strictly ascending by semver precedence.
	ascending := []string{
		"v1.0.0-1", "v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta",
		"v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0",
		"v1.0.1", "v1.9.0", "v1.10.0", "v2.0.0",
	}
	for i := range ascending {
		a, _ := parseRelease(ascending[i])
		if compare(a, a) != 0 {
			t.Errorf("compare(%s, itself) != 0", ascending[i])
		}
		for j := i + 1; j < len(ascending); j++ {
			b, _ := parseRelease(ascending[j])
			if compare(a, b) != -1 || compare(b, a) != 1 {
				t.Errorf("want %s < %s", ascending[i], ascending[j])
			}
		}
	}
}
