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
	var page UserQueryPage
	if query.IsHidden == nil {
		page, err = queryUserDirectoryPage(ctx, tx, query)
	} else {
		page, err = queryUserDirectoryByVisibility(ctx, tx, query)
	}
	if err != nil {
		return UserQueryPage{}, err
	}
	if err := CheckAdministrator(ctx, tx, actor, AdministratorEmby, false); err != nil {
		return UserQueryPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserQueryPage{}, fmt.Errorf("commit user query: %w", err)
	}
	return page, nil
}

const userDirectoryPredicate = `WHERE ($1::boolean IS NULL OR is_disabled = $1)
	AND ($2::text = '' OR normalized_name COLLATE "C" >= $2 COLLATE "C")`

func userDirectoryDirection(query UserQuery) string {
	direction := "ASC"
	if query.Descending {
		direction = "DESC"
	}
	return direction
}

func queryUserDirectoryPage(ctx context.Context, tx pgx.Tx, query UserQuery) (UserQueryPage, error) {
	page := UserQueryPage{Items: make([]User, 0, query.Limit)}
	name := foldedUserName(query.NameStartsWithOrGreater)
	if query.Limit == 0 {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM users "+userDirectoryPredicate,
			query.IsDisabled, name).Scan(&page.TotalRecordCount); err != nil {
			return UserQueryPage{}, fmt.Errorf("count queried users: %w", err)
		}
		return page, nil
	}
	direction := userDirectoryDirection(query)
	// Keep only ordering keys for all matches. Count and page share one statement
	// snapshot, and full policy/configuration records are returned only for the page.
	rows, err := tx.Query(ctx, `WITH filtered AS MATERIALIZED (
		SELECT id, normalized_name FROM users `+userDirectoryPredicate+`
	), page_ids AS (
		SELECT id, normalized_name FROM filtered
		ORDER BY normalized_name COLLATE "C" `+direction+`, id `+direction+` LIMIT $3 OFFSET $4
	)
	SELECT `+userColumns+`, totals.total
	FROM (SELECT count(*) AS total FROM filtered) totals
	LEFT JOIN page_ids ON true
	LEFT JOIN users USING (id)
	ORDER BY page_ids.normalized_name COLLATE "C" `+direction+`, page_ids.id `+direction,
		query.IsDisabled, name, query.Limit, query.StartIndex)
	if err != nil {
		return UserQueryPage{}, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if rows.RawValues()[0] == nil {
			// An empty page still returns its count through the outer join. Skip
			// its null account fields instead of manufacturing an empty User.
			columns := make([]any, len(rows.RawValues()))
			columns[len(columns)-1] = &page.TotalRecordCount
			if err := rows.Scan(columns...); err != nil {
				return UserQueryPage{}, fmt.Errorf("read empty user query page: %w", err)
			}
			continue
		}
		user, err := scanUser(rows, &page.TotalRecordCount)
		if err != nil {
			return UserQueryPage{}, fmt.Errorf("read queried user: %w", err)
		}
		page.Items = append(page.Items, user)
	}
	if err := rows.Err(); err != nil {
		return UserQueryPage{}, fmt.Errorf("read user query: %w", err)
	}
	return page, nil
}

func queryUserDirectoryByVisibility(ctx context.Context, tx pgx.Tx, query UserQuery) (UserQueryPage, error) {
	direction := userDirectoryDirection(query)
	// Fixed C collation makes the lower bound and ordering independent of the
	// database locale. Only these two fixed direction literals enter the SQL.
	// Visibility retains the conservative Go projection for malformed policies.
	rows, err := tx.Query(ctx, "SELECT "+userColumns+" FROM users "+userDirectoryPredicate+`
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
		if ProjectManagedPolicy(user.Policy).IsHidden != *query.IsHidden {
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
	return page, nil
}
