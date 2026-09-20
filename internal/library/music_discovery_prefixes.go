package library

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NamePrefix uses the SDK NameValuePair shape. Name is the actual initial
// Unicode character, uppercased where Unicode defines a case mapping; Value is
// the decimal count. Han initials remain Han, without inferred pronunciation.
type NamePrefix struct {
	Name  string
	Value string
}

// QueryNamePrefixes aggregates the complete filtered, authorized population.
// Paging and ordering controls do not truncate a navigation prefix inventory.
// family is empty for items or a ListMusicEntities family for artist prefixes.
func (s *Store) QueryNamePrefixes(ctx context.Context, family string, query Query) ([]NamePrefix, error) {
	query.StartIndex, query.Limit, query.SortBy, query.SortOrder = 0, 100, "SortName", "Ascending"
	if query.ParentID == "" {
		query.Recursive = true
	}
	var err error
	query, err = normalizeItemQuery(query)
	if err != nil {
		return nil, err
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	parent, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return nil, err
	}
	var statement string
	var args []any
	if family == "" {
		prefix, filter, parameters := itemQuerySQL(query, access, parent)
		initial := musicNameInitialSQL("i.name")
		statement, args = prefix+`SELECT `+initial+`,count(*) FROM items i WHERE `+filter+` GROUP BY `+initial, parameters
	} else {
		prefix, parameters, err := musicEntityQuerySQL(query, access, parent, family)
		if err != nil {
			return nil, err
		}
		initial := musicNameInitialSQL("name")
		statement, args = prefix+`SELECT `+initial+`,count(*) FROM eligible_entities GROUP BY `+initial, parameters
	}
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query name prefixes: %w", err)
	}
	counts := map[string]int64{}
	for rows.Next() {
		var initial string
		var count int64
		if err := rows.Scan(&initial, &count); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read name prefix: %w", err)
		}
		if normalized := musicNameInitial(initial); normalized != "" {
			counts[normalized] += count
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("finish name prefixes: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete name prefixes: %w", err)
	}
	return orderedMusicPrefixes(counts), nil
}

// Match strings.TrimSpace's Unicode White_Space set before taking the first
// character. PostgreSQL btrim without an explicit set removes only ASCII space.
func musicNameInitialSQL(expression string) string {
	return `left(btrim(` + expression + `,U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'),1)`
}

func musicNameInitial(name string) string {
	initial, width := utf8.DecodeRuneInString(strings.TrimSpace(name))
	if width == 0 || initial == utf8.RuneError && width == 1 || unicode.IsControl(initial) {
		return ""
	}
	return string(unicode.ToUpper(initial))
}

func orderedMusicPrefixes(counts map[string]int64) []NamePrefix {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	// Code-point order is stable across host locales. Latin A-Z, digits, Han,
	// and other scripts keep actual searchable initials rather than a '#' bucket.
	sort.Strings(names)
	result := make([]NamePrefix, 0, len(names))
	for _, name := range names {
		result = append(result, NamePrefix{Name: name, Value: strconv.FormatInt(counts[name], 10)})
	}
	return result
}
