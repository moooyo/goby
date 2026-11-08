package updates

import "testing"

func TestSemanticVersionOrderingUsesProductVersionPrecedence(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "999999999999999999999999999.0.0"}
	for index := 1; index < len(ordered); index++ {
		previous, previousOK := parseVersion(ordered[index-1])
		next, nextOK := parseVersion(ordered[index])
		if !previousOK || !nextOK || compareVersions(previous, next) >= 0 || compareVersions(next, previous) <= 0 {
			t.Fatalf("semantic precedence was not preserved between %s and %s", ordered[index-1], ordered[index])
		}
	}
	left, leftOK := parseVersion("v0.2.0+build.8")
	right, rightOK := parseVersion("0.2.0+different.9")
	if !leftOK || !rightOK || compareVersions(left, right) != 0 {
		t.Fatal("an optional v prefix or build metadata changed version precedence")
	}
	for _, value := range []string{"", "4.9.5.0", "integration-version", "0.1", "01.0.0", "1.01.0", "1.0.00", "1.0.0-01", "1.0.0-", "1.0.0+", "1.0.0-a..b", "1.0.0+a+b", " 1.0.0", "1.0.0\n", "1.0.0-alpha_1", "vv1.0.0"} {
		if _, ok := parseVersion(value); ok {
			t.Errorf("invalid product version %q was accepted", value)
		}
	}
}
