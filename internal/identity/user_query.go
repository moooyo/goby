package identity

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const MaxUserQueryLimit = 1000

// UserQuery selects an administrator-visible directory page. Nil Boolean
// filters include both states; a zero Limit requests only the filtered count.
type UserQuery struct {
	IsHidden                *bool
	IsDisabled              *bool
	NameStartsWithOrGreater string
	Descending              bool
	StartIndex              int
	Limit                   int
}

type UserQueryPage struct {
	Items            []User
	TotalRecordCount int64
}

// ValidateUserQuery also protects callers that bypass an HTTP query parser.
func ValidateUserQuery(query UserQuery) error {
	name := query.NameStartsWithOrGreater
	if !utf8.ValidString(name) || len(name) > 512 || utf8.RuneCountInString(name) > 128 || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
		query.StartIndex < 0 || query.StartIndex > math.MaxInt32 || query.Limit < 0 || query.Limit > MaxUserQueryLimit {
		return ErrInvalidInput
	}
	return nil
}

// QueryUsers rechecks current Emby administrator or application authority in
// the directory transaction. A single SELECT supplies count and page, while
// policy filtering uses the same conservative projection as the User DTO.
func (s *Store) QueryUsers(ctx context.Context, actor Principal, query UserQuery) (UserQueryPage, error) {
	if err := ValidateUserQuery(query); err != nil {
		return UserQueryPage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UserQueryPage{}, fmt.Errorf("begin user query: %w", err)
	}
	defer rollback(tx)
	if err := CheckAdministrator(ctx, tx, actor, AdministratorEmby, true); err != nil {
		return UserQueryPage{}, err
	}
	direction := "ASC"
	if query.Descending {
		direction = "DESC"
	}
	// Fixed C collation makes the lower bound and ordering independent of the
	// database locale. Only these two fixed direction literals enter the SQL.
	rows, err := tx.Query(ctx, "SELECT "+userColumns+` FROM users
		WHERE ($1::boolean IS NULL OR is_disabled = $1)
		AND ($2::text = '' OR normalized_name COLLATE "C" >= $2 COLLATE "C")
		ORDER BY normalized_name COLLATE "C" `+direction+", id "+direction,
		query.IsDisabled, foldedUserName(query.NameStartsWithOrGreater))
	if err != nil {
		return UserQueryPage{}, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()
	page := UserQueryPage{Items: make([]User, 0, query.Limit)}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return UserQueryPage{}, fmt.Errorf("read queried user: %w", err)
		}
		if query.IsHidden != nil && ProjectManagedPolicy(user.Policy).IsHidden != *query.IsHidden {
			continue
		}
		if page.TotalRecordCount >= int64(query.StartIndex) && len(page.Items) < query.Limit {
			page.Items = append(page.Items, user)
		}
		page.TotalRecordCount++
	}
	if err := rows.Err(); err != nil {
		return UserQueryPage{}, fmt.Errorf("read user query: %w", err)
	}
	rows.Close()
	if err := CheckAdministrator(ctx, tx, actor, AdministratorEmby, false); err != nil {
		return UserQueryPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserQueryPage{}, fmt.Errorf("commit user query: %w", err)
	}
	return page, nil
}
