package library

import (
	"fmt"
	"strings"
)

// Explicit predicates always constrain the final mixed population. A false
// missing/placeholder selector therefore overrides a saved display preference.
// Unknown premiere dates are not evidence of an unaired episode.
func includesExpectedEpisodes(query Query) bool {
	if query.IsMissing != nil && !*query.IsMissing || query.IsPlaceHolder != nil && !*query.IsPlaceHolder {
		return false
	}
	if query.DisplayMissingEpisodes {
		return true
	}
	for _, value := range []*bool{query.IsMissing, query.IsVirtualUnaired, query.IsPlaceHolder, query.IsUnaired} {
		if value != nil && *value {
			return true
		}
	}
	for _, id := range query.Ids {
		if IsExpectedEpisodeID(id) {
			return true
		}
	}
	return false
}

func appendItemQueryCTE(prefix, expression string) string {
	if prefix == "" {
		return "WITH RECURSIVE " + expression + " "
	}
	return strings.TrimSpace(prefix) + ", " + expression + " "
}

func addExpectedEpisodeConditions(query Query, conditions []string, args []any) ([]string, []any) {
	missing := "false"
	if query.expectedEpisodePopulation {
		missing = "(i.expected_episode IS NOT NULL)"
	}
	unaired := "(i.type='Episode' AND COALESCE(" + navigationPremiereSQL() + ">CURRENT_TIMESTAMP,false))"
	for _, selector := range []struct {
		value *bool
		sql   string
	}{
		{query.IsMissing, missing}, {query.IsPlaceHolder, missing},
		{query.IsVirtualUnaired, "(" + missing + " AND " + unaired + ")"},
		{query.IsUnaired, unaired},
	} {
		if selector.value != nil {
			args = append(args, *selector.value)
			conditions = append(conditions, fmt.Sprintf("%s=$%d::boolean", selector.sql, len(args)))
		}
	}
	return conditions, args
}
