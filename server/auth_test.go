package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRegisterFinishRequiresEmailVerification(t *testing.T) {
	store := newTestBadgerStore(t)
	registrationID := uuid.New()
	if err := store.putRegistration(registrationID, pendingRegistrationState{
		Username: "test_user",
		Email:    "test@example.com",
		Created:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	handler := &authHandler{state: store}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register/finish", strings.NewReader(
		`{"registration_id":"`+registrationID.String()+`"}`,
	))
	context.Request.Header.Set("Content-Type", "application/json")

	handler.registerFinish(context)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unverified registration status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}
