package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

const (
	minSearchQueryLength = 2
	maxSearchQueryLength = 64
	searchResultLimit    = 20
)

// Search queries may only contain characters that can appear in a username.
var searchQueryPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type usersHandler struct {
	db *bun.DB
}

type userSearchResult struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func newUsersHandler(db *bun.DB) *usersHandler {
	return &usersHandler{db: db}
}

// ensureUserSearchIndex enables pg_trgm and creates the trigram index used by
// the username search.
func ensureUserSearchIndex(ctx context.Context, db *bun.DB) error {
	if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		return fmt.Errorf("enable pg_trgm extension: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		"CREATE INDEX IF NOT EXISTS users_username_trgm_idx ON users USING gin (username gin_trgm_ops)"); err != nil {
		return fmt.Errorf("create username trigram index: %w", err)
	}
	return nil
}

// GET /api/users/search?q=
func (h *usersHandler) search(c *gin.Context) {
	userId := c.MustGet("user_id").(uuid.UUID)

	query, err := normalizeSearchQuery(c.Query("q"))
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	pattern := "%" + escapeLikePattern(query) + "%"

	var users []User
	err = h.db.NewSelect().Model(&users).
		ModelTableExpr("users AS u").
		ColumnExpr("u.id, u.username").
		Where("u.deleted_at IS NULL").
		Where("u.id != ?", userId).
		Where("(u.username LIKE ? OR ? <% u.username)", pattern, query).
		// Hide users that blocked the searcher or were blocked by them.
		Where("NOT EXISTS (?)", h.db.NewSelect().Model((*Block)(nil)).
			ColumnExpr("1").
			Where("(blocker_id = ? AND blocked_id = u.id) OR (blocker_id = u.id AND blocked_id = ?)", userId, userId)).
		// Exact match first, then prefix matches, then the closest trigram matches.
		OrderExpr("username = ? DESC", query).
		OrderExpr("username LIKE ? DESC", escapeLikePattern(query)+"%").
		OrderExpr("word_similarity(?, username) DESC", query).
		OrderExpr("username ASC").
		Limit(searchResultLimit).
		Scan(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not search users")
		return
	}

	results := make([]userSearchResult, 0, len(users))
	for _, user := range users {
		results = append(results, userSearchResult{
			ID:       user.ID.String(),
			Username: user.Username,
		})
	}

	c.JSON(http.StatusOK, gin.H{"users": results})
}

func normalizeSearchQuery(raw string) (string, error) {
	query := strings.ToLower(strings.TrimSpace(raw))
	if len(query) < minSearchQueryLength || len(query) > maxSearchQueryLength {
		return "", fmt.Errorf("search query must be %d-%d characters", minSearchQueryLength, maxSearchQueryLength)
	}
	if !searchQueryPattern.MatchString(query) {
		return "", errors.New("search query may only contain lowercase letters, digits, or underscores")
	}
	return query, nil
}

// escapeLikePattern escapes LIKE wildcards; underscores are valid in usernames.
func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
