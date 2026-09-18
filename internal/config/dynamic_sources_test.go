package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestDynamicSourceConfigurationBoundaries(t *testing.T) {
	valid := `[{"itemId":"0123456789abcdef0123456789abcdef","url":"https://media.example/stream","headers":{"Authorization":"Bearer private-value"},"infinite":true,"maxReconnects":2}]`
	definitions, err := parseDynamicSources([]byte(valid))
	if err != nil || len(definitions) != 1 || !definitions[0].Infinite {
		t.Fatalf("valid registered source: %v", err)
	}
	for _, invalid := range []string{
		`null`, `{}`, valid + ` []`,
		strings.Replace(valid, `"infinite":true`, `"unknown":true`, 1),
		strings.Replace(valid, `https://media.example/stream`, `file:///etc/passwd`, 1),
		strings.Replace(valid, `https://media.example/stream`, `https://user:private-value@media.example/stream`, 1),
		strings.Replace(valid, `"Authorization"`, `"Host"`, 1),
		strings.Replace(valid, `"maxReconnects":2`, `"maxReconnects":6`, 1),
		strings.Replace(valid, `"0123456789abcdef0123456789abcdef"`, `"unregistered"`, 1),
		"[" + strings.Trim(valid, "[]") + "," + strings.Trim(valid, "[]") + "]",
	} {
		if _, err := parseDynamicSources([]byte(invalid)); err == nil {
			t.Fatal("invalid dynamic-source configuration accepted")
		}
	}
	for _, output := range []string{fmt.Sprint(definitions), fmt.Sprintf("%+v", definitions), fmt.Sprintf("%#v", definitions)} {
		if strings.Contains(output, "private-value") || strings.Contains(output, "media.example") {
			t.Fatal("private source configuration escaped through formatting")
		}
	}
}
