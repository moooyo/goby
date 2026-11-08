package updates

import "strings"

type semanticVersion struct {
	core [3]string
	pre  []string
}

func parseVersion(value string) (semanticVersion, bool) {
	var result semanticVersion
	if len(value) == 0 || len(value) > 128 {
		return result, false
	}
	value = strings.TrimPrefix(value, "v")
	base, build, hasBuild := strings.Cut(value, "+")
	if hasBuild && !versionIdentifiers(build, false) {
		return result, false
	}
	core, prerelease, hasPrerelease := strings.Cut(base, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return result, false
	}
	for index, part := range parts {
		if !numericIdentifier(part) || len(part) > 1 && part[0] == '0' {
			return result, false
		}
		result.core[index] = part
	}
	if hasPrerelease {
		if !versionIdentifiers(prerelease, true) {
			return semanticVersion{}, false
		}
		result.pre = strings.Split(prerelease, ".")
	}
	return result, true
}

func versionIdentifiers(value string, prerelease bool) bool {
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for _, character := range part {
			if !(character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character == '-') {
				return false
			}
		}
		if prerelease && numericIdentifier(part) && len(part) > 1 && part[0] == '0' {
			return false
		}
	}
	return true
}

func numericIdentifier(value string) bool {
	return value != "" && strings.Trim(value, "0123456789") == ""
}

func compareNumeric(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func compareVersions(left, right semanticVersion) int {
	for index := range left.core {
		if order := compareNumeric(left.core[index], right.core[index]); order != 0 {
			return order
		}
	}
	if len(left.pre) == 0 && len(right.pre) > 0 {
		return 1
	}
	if len(left.pre) > 0 && len(right.pre) == 0 {
		return -1
	}
	for index := 0; index < min(len(left.pre), len(right.pre)); index++ {
		leftNumeric, rightNumeric := numericIdentifier(left.pre[index]), numericIdentifier(right.pre[index])
		switch {
		case leftNumeric && !rightNumeric:
			return -1
		case !leftNumeric && rightNumeric:
			return 1
		case leftNumeric:
			if order := compareNumeric(left.pre[index], right.pre[index]); order != 0 {
				return order
			}
		default:
			if order := strings.Compare(left.pre[index], right.pre[index]); order != 0 {
				return order
			}
		}
	}
	if len(left.pre) < len(right.pre) {
		return -1
	}
	if len(left.pre) > len(right.pre) {
		return 1
	}
	return 0
}
