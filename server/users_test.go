package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestNormalizeSearchQuery(t *testing.T) {
	valid := map[string]string{
		"  Mario ":  "mario",
		"ab":        "ab",
		"user_name": "user_name",
		"_a":        "_a",
		"abc123":    "abc123",
	}
	for raw, expected := range valid {
		got, err := normalizeSearchQuery(raw)
		if err != nil {
			t.Fatalf("normalizeSearchQuery(%q) returned error: %v", raw, err)
		}
		if got != expected {
			t.Fatalf("normalizeSearchQuery(%q) = %q, want %q", raw, got, expected)
		}
	}

	invalid := []string{
		"",
		"   ",
		"a",
		"bad name",
		"bad%",
		"bad-name",
		"name@example.com",
		string(make([]byte, maxSearchQueryLength+1)),
	}
	for _, raw := range invalid {
		if _, err := normalizeSearchQuery(raw); err == nil {
			t.Fatalf("normalizeSearchQuery(%q) succeeded, want error", raw)
		}
	}
}

func TestEscapeLikePattern(t *testing.T) {
	cases := map[string]string{
		"mario":    "mario",
		"a_b":      `a\_b`,
		"100%":     `100\%`,
		`back\sl`:  `back\\sl`,
		"__init__": `\_\_init\_\_`,
	}
	for input, expected := range cases {
		if got := escapeLikePattern(input); got != expected {
			t.Fatalf("escapeLikePattern(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestSearchRejectsInvalidQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := newUsersHandler(nil)

	for _, query := range []string{"", "a", "bad%25"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/users/search?q="+query, nil)
		c.Set("user_id", uuid.New())

		handler.search(c)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("query %q: status = %d, want %d", query, recorder.Code, http.StatusBadRequest)
		}
	}
}
