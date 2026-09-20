package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUserQueryRejectsInvalidBoundsBeforeDatabaseAccess(t *testing.T) {
	var overflow int64 = 2147483648
	for _, query := range []UserQuery{
		{StartIndex: -1}, {StartIndex: int(overflow)}, {Limit: -1}, {Limit: MaxUserQueryLimit + 1},
		{NameStartsWithOrGreater: strings.Repeat("a", 129)},
		{NameStartsWithOrGreater: string([]byte{0xff})}, {NameStartsWithOrGreater: "a\x00b"},
	} {
		if _, err := New(nil).QueryUsers(context.Background(), Principal{}, query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid directory query reached storage: %#v, %v", query, err)
		}
	}
}

func TestUserQueryNameFoldingMatchesAccountUniqueness(t *testing.T) {
	for _, names := range [][]string{
		{"Alpha", "ALPHA", "alpha"}, {"\u03a3igma", "\u03c3igma", "\u03c2igma"}, {"Kelvin", "\u212aelvin"},
	} {
		want := foldedUserName(names[0])
		for _, name := range names {
			_, normalized, err := normalizeName(name)
			if err != nil || normalized != want || foldedUserName(name) != want {
				t.Fatalf("query folding differs from username uniqueness for %q: %q, %v", name, normalized, err)
			}
		}
	}
	if foldedUserName(" Bravo%_ ") != " bravo%_ " {
		t.Fatal("query folding changed literal spaces or wildcard characters")
	}
}
