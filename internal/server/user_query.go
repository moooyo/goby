package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/identity"
)

func parseEmbyUserQuery(r *http.Request) (identity.UserQuery, error) {
	query := identity.UserQuery{Limit: 100}
	values, err := embyBusinessQuery(r)
	if err != nil {
		return query, identity.ErrInvalidInput
	}
	seen := make(map[string]bool, len(values))
	for name, entries := range values {
		field := strings.ToLower(name)
		if len(entries) != 1 || seen[field] {
			return query, identity.ErrInvalidInput
		}
		seen[field] = true
		value := entries[0]
		switch field {
		case "ishidden", "isdisabled":
			if !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
				return query, identity.ErrInvalidInput
			}
			flag := strings.EqualFold(value, "true")
			if field == "ishidden" {
				query.IsHidden = &flag
			} else {
				query.IsDisabled = &flag
			}
		case "startindex", "limit":
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 {
				return query, identity.ErrInvalidInput
			}
			if field == "startindex" {
				query.StartIndex = int(number)
			} else {
				query.Limit = min(int(number), identity.MaxUserQueryLimit)
			}
		case "namestartswithorgreater":
			query.NameStartsWithOrGreater = value
		case "sortorder":
			if !strings.EqualFold(value, "Ascending") && !strings.EqualFold(value, "Descending") {
				return query, identity.ErrInvalidInput
			}
			query.Descending = strings.EqualFold(value, "Descending")
		default:
			return query, identity.ErrInvalidInput
		}
	}
	return query, identity.ValidateUserQuery(query)
}
