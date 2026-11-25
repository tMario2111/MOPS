package main

import (
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bytemare/ksf"
	"github.com/bytemare/opaque"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// testOpaqueConfiguration mirrors the suite configured in newAuthHandler.
func testOpaqueConfiguration() *opaque.Configuration {
	return &opaque.Configuration{
		OPRF: opaque.P256Sha256,
		AKE:  opaque.P256Sha256,
		KSF:  ksf.Argon2id,
		KDF:  crypto.SHA256,
		MAC:  crypto.SHA256,
		Hash: crypto.SHA256,
	}
}

// newTestAuthHandler builds a handler through newAuthHandler with freshly
// generated, test-only OPAQUE server key material and an in-memory Badger store.
func newTestAuthHandler(t *testing.T) *authHandler {
	t.Helper()
	config := testOpaqueConfiguration()
	privateKey, publicKey := config.KeyGen()
	material := &opaque.ServerKeyMaterial{
		PrivateKey:     privateKey,
		PublicKeyBytes: publicKey.Encode(),
		OPRFGlobalSeed: config.GenerateOPRFSeed(),
		// DecodeServerKeyMaterial rejects an empty identity vector.
		Identity: []byte("mops-test-server"),
	}
	t.Setenv("OPAQUE_SERVER_KEY_MATERIAL", hex.EncodeToString(material.Encode()))
	t.Setenv("TOTP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, totpEncryptionKeySize)))

	handler, err := newAuthHandler(nil, newTestBadgerStore(t))
	if err != nil {
		t.Fatalf("create auth handler: %v", err)
	}
	return handler
}

func newOpaqueRegistrationRequest(t *testing.T) string {
	t.Helper()
	client, err := testOpaqueConfiguration().Client()
	if err != nil {
		t.Fatalf("create OPAQUE client: %v", err)
	}
	request, err := client.RegistrationInit([]byte("correct horse battery staple"))
	if err != nil {
		t.Fatalf("create OPAQUE registration request: %v", err)
	}
	return encodeMessage(request.Serialize())
}

func callJSONHandler(t *testing.T, handle gin.HandlerFunc, path, body string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")

	handle(context)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return recorder.Code, response
}

func registrationStartBody(username, email, opaqueRequest string) string {
	body, _ := json.Marshal(registrationStartRequest{
		Username:      username,
		Email:         email,
		OpaqueRequest: opaqueRequest,
	})
	return string(body)
}

func TestNewAuthHandlerRejectsInvalidKeyMaterial(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "not hex", value: "not-hex"},
		{name: "wrong length", value: "abcd"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OPAQUE_SERVER_KEY_MATERIAL", test.value)
			if _, err := newAuthHandler(nil, nil); err == nil {
				t.Fatal("invalid OPAQUE server key material unexpectedly accepted")
			}
		})
	}
}

func TestRegisterStartStoresUnverifiedRegistration(t *testing.T) {
	handler := newTestAuthHandler(t)

	status, response := callJSONHandler(t, handler.registerStart, "/api/auth/register/start",
		registrationStartBody("  test_user ", " Test@Example.COM ", newOpaqueRegistrationRequest(t)))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (response %v)", status, http.StatusOK, response)
	}
	if response["opaque_response"] == "" || response["opaque_response"] == nil {
		t.Fatal("missing opaque_response")
	}
	registrationID, err := uuid.Parse(response["registration_id"].(string))
	if err != nil {
		t.Fatalf("invalid registration_id: %v", err)
	}
	if _, exists := response["code"]; exists {
		t.Fatal("verification code must not be returned to the client")
	}

	pending, err := handler.state.getRegistration(registrationID)
	if err != nil {
		t.Fatalf("get registration state: %v", err)
	}
	if pending.Username != "test_user" || pending.Email != "test@example.com" {
		t.Fatalf("identity was not normalized: %q %q", pending.Username, pending.Email)
	}
	if pending.EmailVerified {
		t.Fatal("new registration must not be marked as verified")
	}
	if len(pending.VerificationCodeHash) != 64 {
		t.Fatalf("verification code hash length = %d, want 64", len(pending.VerificationCodeHash))
	}
}

func TestRegisterStartRejectsPendingIdentity(t *testing.T) {
	handler := newTestAuthHandler(t)
	status, _ := callJSONHandler(t, handler.registerStart, "/api/auth/register/start",
		registrationStartBody("test_user", "test@example.com", newOpaqueRegistrationRequest(t)))
	if status != http.StatusOK {
		t.Fatalf("first registration status = %d, want %d", status, http.StatusOK)
	}

	tests := []struct {
		name        string
		username    string
		email       string
		wantMessage string
	}{
		{
			name:        "pending username",
			username:    "test_user",
			email:       "other@example.com",
			wantMessage: "username has a pending registration",
		},
		{
			name:        "pending email",
			username:    "other_user",
			email:       "TEST@example.com",
			wantMessage: "email has a pending registration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, response := callJSONHandler(t, handler.registerStart, "/api/auth/register/start",
				registrationStartBody(test.username, test.email, newOpaqueRegistrationRequest(t)))
			if status != http.StatusConflict {
				t.Fatalf("status = %d, want %d", status, http.StatusConflict)
			}
			if response["error"] != test.wantMessage {
				t.Fatalf("error = %q, want %q", response["error"], test.wantMessage)
			}
		})
	}
}

func TestRegisterStartValidation(t *testing.T) {
	handler := newTestAuthHandler(t)
	validRequest := newOpaqueRegistrationRequest(t)
	tests := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{name: "invalid request body", body: `{`, wantMessage: "invalid request body"},
		{
			name:        "invalid username",
			body:        registrationStartBody("_bad", "test@example.com", validRequest),
			wantMessage: "username must be 4-64 lowercase letters, digits, or underscores; underscores cannot lead, trail, or repeat",
		},
		{
			name:        "invalid email",
			body:        registrationStartBody("test_user", "invalid", validRequest),
			wantMessage: "email is invalid",
		},
		{
			name:        "invalid opaque request encoding",
			body:        registrationStartBody("test_user", "test@example.com", "not base64"),
			wantMessage: "invalid opaque_request",
		},
		{
			name:        "invalid OPAQUE registration request",
			body:        registrationStartBody("test_user", "test@example.com", encodeMessage([]byte("garbage"))),
			wantMessage: "invalid OPAQUE registration request",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, response := callJSONHandler(t, handler.registerStart, "/api/auth/register/start", test.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
			}
			if response["error"] != test.wantMessage {
				t.Fatalf("error = %q, want %q", response["error"], test.wantMessage)
			}
		})
	}
}

func TestRegisterFinishValidation(t *testing.T) {
	handler := newTestAuthHandler(t)
	verifiedID := uuid.New()
	if err := handler.state.putRegistration(verifiedID, pendingRegistrationState{
		Username:      "test_user",
		Email:         "test@example.com",
		EmailVerified: true,
		Created:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	finishBody := func(registrationID string, mutate func(*registrationFinishRequest)) string {
		request := registrationFinishRequest{
			RegistrationID:           registrationID,
			OpaqueRecord:             encodeMessage([]byte("record")),
			PublicKey:                "public",
			SigningPublicKey:         "signing-public",
			WrappedX25519PrivateKey:  "wrapped-x25519",
			WrappedEd25519PrivateKey: "wrapped-ed25519",
			KeyWrapNonce:             "nonce",
			KeyWrapSalt:              "salt",
			KeyWrapParams:            map[string]any{"algorithm": "argon2id"},
		}
		if mutate != nil {
			mutate(&request)
		}
		body, _ := json.Marshal(request)
		return string(body)
	}

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
			body:        finishBody("not-a-uuid", nil),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid registration_id",
		},
		{
			name:        "missing registration",
			body:        finishBody(uuid.New().String(), nil),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "registration is missing or expired",
		},
		{
			name: "missing wrapped private key",
			body: finishBody(verifiedID.String(), func(request *registrationFinishRequest) {
				request.WrappedX25519PrivateKey = ""
			}),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "wrapped_x25519_private_key is missing or too large",
		},
		{
			name: "oversized public key",
			body: finishBody(verifiedID.String(), func(request *registrationFinishRequest) {
				request.PublicKey = strings.Repeat("a", maxKeyMaterial+1)
			}),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "public_key is missing or too large",
		},
		{
			name: "missing key wrap params",
			body: finishBody(verifiedID.String(), func(request *registrationFinishRequest) {
				request.KeyWrapParams = nil
			}),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "key_wrap_params is required",
		},
		{
			name: "invalid opaque record encoding",
			body: finishBody(verifiedID.String(), func(request *registrationFinishRequest) {
				request.OpaqueRecord = "not base64"
			}),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid opaque_record",
		},
		{
			name:        "invalid OPAQUE registration record",
			body:        finishBody(verifiedID.String(), nil),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid OPAQUE registration record",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, response := callJSONHandler(t, handler.registerFinish, "/api/auth/register/finish", test.body)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
			if response["error"] != test.wantMessage {
				t.Fatalf("error = %q, want %q", response["error"], test.wantMessage)
			}
		})
	}

	if _, err := handler.state.getRegistration(verifiedID); err != nil {
		t.Fatalf("rejected finish requests must keep the verified registration: %v", err)
	}
}

func TestRegisterVerifyHandlerInternalError(t *testing.T) {
	store := newTestBadgerStore(t)
	registrationID := uuid.New()
	if err := store.putRegistration(registrationID, pendingRegistrationState{
		VerificationCodeHash: "not-a-hash",
		Created:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	status, response := callRegisterVerify(t, &authHandler{state: store}, registrationID, "AB12CD34")
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
	if response["error"] != "could not verify email" {
		t.Fatalf("error = %q, want %q", response["error"], "could not verify email")
	}
}

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

func TestLoginTOTPRejectsInvalidAndMissingChallenges(t *testing.T) {
	handler := &authHandler{
		state:             newTestBadgerStore(t),
		totpEncryptionKey: make([]byte, totpEncryptionKeySize),
	}
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid body",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "invalid challenge ID",
			body:       `{"challenge_id":"nope","code":"123456"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid challenge_id",
		},
		{
			name:       "missing challenge",
			body:       `{"challenge_id":"` + uuid.New().String() + `","code":"123456"}`,
			wantStatus: http.StatusGone,
			wantError:  "two-factor challenge expired; sign in again",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, response := callJSONHandler(t, handler.loginTOTP, "/api/auth/login/totp", test.body)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
			if response["error"] != test.wantError {
				t.Fatalf("error = %q, want %q", response["error"], test.wantError)
			}
		})
	}
}
