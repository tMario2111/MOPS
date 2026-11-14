package main

import (
	"encoding/json"
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

func TestRegisterVerifyHandler(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "invalid request body",
			body:        `{`,
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid request body",
		},
		{
			name:        "invalid registration id",
			body:        `{"registration_id":"not-a-uuid","code":"AB12CD34"}`,
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid registration_id",
		},
		{
			name:        "missing registration",
			body:        `{"registration_id":"` + uuid.New().String() + `","code":"AB12CD34"}`,
			wantStatus:  http.StatusGone,
			wantMessage: "registration is missing or expired; restart registration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &authHandler{state: newTestBadgerStore(t)}
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(
				http.MethodPost,
				"/api/auth/register/verify",
				strings.NewReader(test.body),
			)
			context.Request.Header.Set("Content-Type", "application/json")

			handler.registerVerify(context)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response["error"] != test.wantMessage {
				t.Fatalf("error = %q, want %q", response["error"], test.wantMessage)
			}
		})
	}
}

func TestRegisterVerifyHandlerSuccessAndInvalidCode(t *testing.T) {
	store := newTestBadgerStore(t)
	registrationID := uuid.New()
	if err := store.putRegistration(registrationID, pendingRegistrationState{
		Username:             "test_user",
		Email:                "test@example.com",
		VerificationCodeHash: hashVerificationCode("AB12CD34"),
		Created:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}
	handler := &authHandler{state: store}

	status, response := callRegisterVerify(t, handler, registrationID, "WRONG123")
	if status != http.StatusBadRequest {
		t.Fatalf("invalid code status = %d, want %d", status, http.StatusBadRequest)
	}
	if response["attempts_remaining"] != float64(2) {
		t.Fatalf("attempts_remaining = %v, want 2", response["attempts_remaining"])
	}

	status, response = callRegisterVerify(t, handler, registrationID, "AB12CD34")
	if status != http.StatusOK {
		t.Fatalf("valid code status = %d, want %d", status, http.StatusOK)
	}
	if response["verified"] != true {
		t.Fatalf("verified = %v, want true", response["verified"])
	}
}

func TestRegisterVerifyHandlerExhaustsAttempts(t *testing.T) {
	store := newTestBadgerStore(t)
	registrationID := uuid.New()
	if err := store.putRegistration(registrationID, pendingRegistrationState{
		VerificationCodeHash: hashVerificationCode("AB12CD34"),
		Created:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}
	handler := &authHandler{state: store}

	for attempt := 1; attempt <= verificationMaxAttempts; attempt++ {
		status, _ := callRegisterVerify(t, handler, registrationID, "WRONG123")
		if attempt < verificationMaxAttempts && status != http.StatusBadRequest {
			t.Fatalf("attempt %d status = %d, want %d", attempt, status, http.StatusBadRequest)
		}
		if attempt == verificationMaxAttempts && status != http.StatusGone {
			t.Fatalf("final attempt status = %d, want %d", status, http.StatusGone)
		}
	}
}

func callRegisterVerify(
	t *testing.T,
	handler *authHandler,
	registrationID uuid.UUID,
	code string,
) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/auth/register/verify",
		strings.NewReader(`{"registration_id":"`+registrationID.String()+`","code":"`+code+`"}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")
	handler.registerVerify(context)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return recorder.Code, response
}

func TestRegistrationValidationHelpers(t *testing.T) {
	if err := validateRegistrationIdentity("valid_user", "user@example.com"); err != nil {
		t.Fatalf("valid registration identity rejected: %v", err)
	}
	for _, username := range []string{"abc", "_valid", "valid_", "two__words", "UPPER"} {
		if err := validateUsername(username); err == nil {
			t.Fatalf("username %q unexpectedly accepted", username)
		}
	}
	for _, email := range []string{"", "x", "invalid-email", strings.Repeat("a", 321)} {
		if err := validateRegistrationIdentity("valid_user", email); err == nil {
			t.Fatalf("email %q unexpectedly accepted", email)
		}
	}
}

func TestDecodeMessageValidation(t *testing.T) {
	if _, err := decodeMessage("", maxOpaqueMessage); err == nil {
		t.Fatal("empty message unexpectedly accepted")
	}
	if _, err := decodeMessage("not base64", maxOpaqueMessage); err == nil {
		t.Fatal("invalid base64 unexpectedly accepted")
	}
	if _, err := decodeMessage(encodeMessage([]byte("valid")), maxOpaqueMessage); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
}
