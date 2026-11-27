package main

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dgraph-io/badger/v4"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

const (
	selectUserQuery = `SELECT .* FROM "users"`
	updateUserQuery = `UPDATE "users"`
	testPassword    = "correct horse battery staple"
	testServerID    = "mops-test-server"
)

// attachSQLMock gives the handler a Bun database backed by go-sqlmock. Bun
// formats arguments into the SQL text, so expectations match on query shape.
func attachSQLMock(t *testing.T, handler *authHandler) sqlmock.Sqlmock {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	handler.db = bun.NewDB(sqlDB, pgdialect.New())
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectations: %v", err)
		}
		_ = handler.db.Close()
	})
	return mock
}

func newSQLMockTOTPHandler(t *testing.T) (*authHandler, sqlmock.Sqlmock) {
	t.Helper()
	handler := newTestTOTPHandler(t)
	return handler, attachSQLMock(t, handler)
}

type testUser struct {
	id           uuid.UUID
	username     string
	encrypted    *string
	opaqueRecord string
}

func userRows(user testUser) *sqlmock.Rows {
	var encrypted driver.Value
	if user.encrypted != nil {
		encrypted = *user.encrypted
	}
	return sqlmock.NewRows([]string{"id", "username", "email", "totp_secret_encrypted", "opaque_record"}).
		AddRow(user.id.String(), user.username, user.username+"@example.com", encrypted, user.opaqueRecord)
}

func emptyUserRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "username"})
}

func encryptedTestVault(t *testing.T, handler *authHandler, vault totpVault) *string {
	t.Helper()
	encrypted, err := encryptTOTPVault(handler.totpEncryptionKey, vault)
	if err != nil {
		t.Fatalf("encrypt vault: %v", err)
	}
	return &encrypted
}

func newTOTPVaultWithRecovery(t *testing.T) (totpVault, []string) {
	t.Helper()
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	codes, hashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	return totpVault{Secret: secret, RecoveryCodeHashes: hashes, LastAcceptedStep: -1}, codes
}

func currentTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := generateTOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// performJSON runs a handler and returns the recorder, so callers can inspect
// empty bodies (204) and cookies. userID is set only when non-nil.
func performJSON(t *testing.T, handle gin.HandlerFunc, userID *uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	if userID != nil {
		context.Set("user_id", *userID)
	}
	handle(context)
	// gin's engine flushes a header-only response (c.Status) after the handler.
	context.Writer.WriteHeaderNow()
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return response
}

func codeBody(code string) string {
	body, _ := json.Marshal(totpCodeRequest{Code: code})
	return string(body)
}

func loginTOTPBody(challengeID uuid.UUID, code string) string {
	body, _ := json.Marshal(totpLoginRequest{ChallengeID: challengeID.String(), Code: code})
	return string(body)
}

func TestTOTPSetupWithDatabase(t *testing.T) {
	t.Run("account not found", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		mock.ExpectQuery(selectUserQuery).WillReturnRows(emptyUserRows())
		status, response := callAuthenticatedJSONHandler(t, handler.totpSetup, uuid.New(), `{}`)
		assertErrorResponse(t, status, response, http.StatusNotFound, "account not found")
	})

	t.Run("already enabled", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		vault, _ := newTOTPVaultWithRecovery(t)
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{
			id: userID, username: "test_user", encrypted: encryptedTestVault(t, handler, vault),
		}))
		status, response := callAuthenticatedJSONHandler(t, handler.totpSetup, userID, `{}`)
		assertErrorResponse(t, status, response, http.StatusConflict, "two-factor authentication is already enabled")
	})

	t.Run("creates an encrypted pending setup", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{id: userID, username: "test_user"}))

		status, response := callAuthenticatedJSONHandler(t, handler.totpSetup, userID, `{}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, response %v", status, response)
		}
		secret, _ := response["secret"].(string)
		uri, _ := response["otpauth_url"].(string)
		codes, _ := response["recovery_codes"].([]any)
		if secret == "" || !strings.Contains(uri, "Still:test_user") || !strings.Contains(uri, "secret="+secret) {
			t.Fatalf("unexpected setup response: %v", response)
		}
		if len(codes) != totpRecoveryCodeCount {
			t.Fatalf("recovery codes = %d, want %d", len(codes), totpRecoveryCodeCount)
		}

		setup, err := handler.state.getTOTPSetup(userID)
		if err != nil {
			t.Fatalf("pending setup not stored: %v", err)
		}
		if strings.Contains(setup.EncryptedVault, secret) {
			t.Fatal("pending setup stores the plaintext secret")
		}
		vault, err := decryptTOTPVault(handler.totpEncryptionKey, setup.EncryptedVault)
		if err != nil {
			t.Fatalf("decrypt pending setup: %v", err)
		}
		if vault.Secret != secret || len(vault.RecoveryCodeHashes) != totpRecoveryCodeCount {
			t.Fatalf("pending vault does not match the response: %#v", vault)
		}
		if vault.RecoveryCodeHashes[0] != hashTOTPRecoveryCode(codes[0].(string)) {
			t.Fatal("stored recovery hash does not match the returned code")
		}
	})
}

func TestTOTPEnableWithDatabase(t *testing.T) {
	prepare := func(t *testing.T) (*authHandler, sqlmock.Sqlmock, uuid.UUID, string) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		secret, err := generateTOTPSecret()
		if err != nil {
			t.Fatalf("generate secret: %v", err)
		}
		putTestTOTPSetup(t, handler, userID, secret, time.Now().UTC())
		return handler, mock, userID, currentTOTPCode(t, secret)
	}

	t.Run("enables with a valid code", func(t *testing.T) {
		handler, mock, userID, code := prepare(t)
		mock.ExpectExec(updateUserQuery + `.*totp_secret_encrypted IS NULL`).WillReturnResult(sqlmock.NewResult(0, 1))
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, codeBody(code))
		if status != http.StatusOK || response["enabled"] != true {
			t.Fatalf("enable = %d %v", status, response)
		}
		if _, err := handler.state.getTOTPSetup(userID); !errors.Is(err, badger.ErrKeyNotFound) {
			t.Fatalf("pending setup not deleted: %v", err)
		}
	})

	tests := []struct {
		name       string
		result     driver.Result
		execErr    error
		wantStatus int
		wantError  string
	}{
		{"update fails", nil, errors.New("db down"), http.StatusInternalServerError, "could not enable two-factor authentication"},
		{"rows affected fails", sqlmock.NewErrorResult(errors.New("driver")), nil, http.StatusInternalServerError, "could not confirm two-factor authentication"},
		{"enabled concurrently", sqlmock.NewResult(0, 0), nil, http.StatusConflict, "two-factor authentication is already enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, mock, userID, code := prepare(t)
			expectation := mock.ExpectExec(updateUserQuery)
			if test.execErr != nil {
				expectation.WillReturnError(test.execErr)
			} else {
				expectation.WillReturnResult(test.result)
			}
			status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, codeBody(code))
			assertErrorResponse(t, status, response, test.wantStatus, test.wantError)
		})
	}
}

func TestTOTPDisableWithDatabase(t *testing.T) {
	prepare := func(t *testing.T) (*authHandler, sqlmock.Sqlmock, uuid.UUID, totpVault, []string) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		vault, codes := newTOTPVaultWithRecovery(t)
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{
			id: userID, username: "test_user", encrypted: encryptedTestVault(t, handler, vault),
		}))
		return handler, mock, userID, vault, codes
	}

	t.Run("account not found", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		mock.ExpectQuery(selectUserQuery).WillReturnRows(emptyUserRows())
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, uuid.New(), codeBody("123456"))
		assertErrorResponse(t, status, response, http.StatusNotFound, "account not found")
	})

	t.Run("not enabled", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{id: userID, username: "test_user"}))
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, codeBody("123456"))
		assertErrorResponse(t, status, response, http.StatusConflict, "two-factor authentication is not enabled")
	})

	t.Run("undecryptable secret", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		broken := "not-a-vault"
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{id: userID, username: "test_user", encrypted: &broken}))
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, codeBody("123456"))
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not read authenticator secret")
	})

	t.Run("invalid secret in vault", func(t *testing.T) {
		handler, mock := newSQLMockTOTPHandler(t)
		userID := uuid.New()
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{
			id: userID, username: "test_user", encrypted: encryptedTestVault(t, handler, totpVault{Secret: "not base32!"}),
		}))
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, codeBody("123456"))
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not verify authenticator code")
	})

	t.Run("incorrect code counts an attempt", func(t *testing.T) {
		handler, _, userID, vault, _ := prepare(t)
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, codeBody(wrongTOTPCode(t, vault.Secret, time.Now())))
		assertErrorResponse(t, status, response, http.StatusUnauthorized, "authenticator or recovery code is incorrect")
		if response["attempts_remaining"] != float64(totpDisableMaxAttempts-1) {
			t.Fatalf("attempts_remaining = %v", response["attempts_remaining"])
		}
	})

	t.Run("last incorrect code locks", func(t *testing.T) {
		handler, _, userID, vault, _ := prepare(t)
		for attempt := 1; attempt < totpDisableMaxAttempts; attempt++ {
			if _, err := handler.state.failTOTPDisable(userID); err != nil {
				t.Fatalf("seed attempt %d: %v", attempt, err)
			}
		}
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, codeBody(wrongTOTPCode(t, vault.Secret, time.Now())))
		assertErrorResponse(t, status, response, http.StatusTooManyRequests, "too many incorrect codes; try again later")
	})

	t.Run("disables with an authenticator code", func(t *testing.T) {
		handler, mock, userID, vault, _ := prepare(t)
		if _, err := handler.state.failTOTPDisable(userID); err != nil {
			t.Fatalf("seed attempt: %v", err)
		}
		mock.ExpectExec(updateUserQuery + `.*totp_secret_encrypted = NULL`).WillReturnResult(sqlmock.NewResult(0, 1))
		recorder := performJSON(t, handler.totpDisable, &userID, codeBody(currentTOTPCode(t, vault.Secret)))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
		}
		if _, err := handler.state.getTOTPDisableAttempts(userID); !errors.Is(err, badger.ErrKeyNotFound) {
			t.Fatalf("disable attempts not cleared: %v", err)
		}
	})

	t.Run("disables with a recovery code", func(t *testing.T) {
		handler, mock, userID, _, codes := prepare(t)
		mock.ExpectExec(updateUserQuery).WillReturnResult(sqlmock.NewResult(0, 1))
		recorder := performJSON(t, handler.totpDisable, &userID, codeBody(" "+strings.ToLower(codes[3])+" "))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
		}
	})

	tests := []struct {
		name       string
		result     driver.Result
		execErr    error
		wantStatus int
		wantError  string
	}{
		{"update fails", nil, errors.New("db down"), http.StatusInternalServerError, "could not disable two-factor authentication"},
		{"rows affected fails", sqlmock.NewErrorResult(errors.New("driver")), nil, http.StatusInternalServerError, "could not confirm two-factor settings"},
		{"changed concurrently", sqlmock.NewResult(0, 0), nil, http.StatusConflict, "two-factor settings changed; try again"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, mock, userID, vault, _ := prepare(t)
			expectation := mock.ExpectExec(updateUserQuery)
			if test.execErr != nil {
				expectation.WillReturnError(test.execErr)
			} else {
				expectation.WillReturnResult(test.result)
			}
			status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, codeBody(currentTOTPCode(t, vault.Secret)))
			assertErrorResponse(t, status, response, test.wantStatus, test.wantError)
		})
	}
}

func TestLoginTOTPWithDatabase(t *testing.T) {
	type fixture struct {
		handler     *authHandler
		mock        sqlmock.Sqlmock
		userID      uuid.UUID
		challengeID uuid.UUID
		vault       totpVault
		codes       []string
	}
	prepare := func(t *testing.T, challengeCreated time.Time) fixture {
		handler, mock := newSQLMockTOTPHandler(t)
		f := fixture{handler: handler, mock: mock, userID: uuid.New(), challengeID: uuid.New()}
		f.vault, f.codes = newTOTPVaultWithRecovery(t)
		if err := handler.state.putTOTPLogin(f.challengeID, pendingTOTPLoginState{UserID: f.userID, CreatedAt: challengeCreated}); err != nil {
			t.Fatalf("put challenge: %v", err)
		}
		return f
	}
	expectUser := func(f fixture, encrypted *string) {
		f.mock.ExpectQuery(selectUserQuery + `.*deleted_at IS NULL`).
			WillReturnRows(userRows(testUser{id: f.userID, username: "test_user", encrypted: encrypted}))
	}

	t.Run("deleted or missing account", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		f.mock.ExpectQuery(selectUserQuery).WillReturnRows(emptyUserRows())
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp", loginTOTPBody(f.challengeID, "123456"))
		assertErrorResponse(t, status, response, http.StatusUnauthorized, "invalid two-factor challenge")
	})

	t.Run("account without two-factor authentication", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		expectUser(f, nil)
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp", loginTOTPBody(f.challengeID, "123456"))
		assertErrorResponse(t, status, response, http.StatusUnauthorized, "invalid two-factor challenge")
	})

	t.Run("undecryptable secret", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		broken := "not-a-vault"
		expectUser(f, &broken)
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp", loginTOTPBody(f.challengeID, "123456"))
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not read authenticator secret")
	})

	t.Run("invalid secret in vault", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		expectUser(f, encryptedTestVault(t, f.handler, totpVault{Secret: "not base32!"}))
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp", loginTOTPBody(f.challengeID, "123456"))
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not verify authenticator code")
	})

	t.Run("incorrect code counts an attempt", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		expectUser(f, encryptedTestVault(t, f.handler, f.vault))
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp",
			loginTOTPBody(f.challengeID, wrongTOTPCode(t, f.vault.Secret, time.Now())))
		assertErrorResponse(t, status, response, http.StatusUnauthorized, "authenticator or recovery code is incorrect")
		if response["attempts_remaining"] != float64(totpLoginMaxAttempts-1) {
			t.Fatalf("attempts_remaining = %v", response["attempts_remaining"])
		}
	})

	t.Run("last incorrect code locks the account", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		if err := f.handler.state.putJSON(badgerTOTPLoginAttemptsPrefix+f.userID.String(),
			totpDisableAttemptState{Attempts: totpLoginMaxAttempts - 1, CreatedAt: time.Now().UTC()}, totpLoginLockTTL); err != nil {
			t.Fatalf("seed attempts: %v", err)
		}
		expectUser(f, encryptedTestVault(t, f.handler, f.vault))
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp",
			loginTOTPBody(f.challengeID, wrongTOTPCode(t, f.vault.Secret, time.Now())))
		assertErrorResponse(t, status, response, http.StatusTooManyRequests, "too many incorrect codes; try again in 15 minutes")
		if _, err := f.handler.state.getTOTPLogin(f.challengeID); !errors.Is(err, badger.ErrKeyNotFound) {
			t.Fatalf("challenge survived the lock: %v", err)
		}
	})

	t.Run("challenge expires before the failure is recorded", func(t *testing.T) {
		f := prepare(t, time.Now().UTC().Add(-totpLoginChallengeTTL-time.Second))
		expectUser(f, encryptedTestVault(t, f.handler, f.vault))
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp",
			loginTOTPBody(f.challengeID, wrongTOTPCode(t, f.vault.Secret, time.Now())))
		assertErrorResponse(t, status, response, http.StatusGone, "two-factor challenge expired; sign in again")
	})

	assertSession := func(t *testing.T, f fixture, recorder *httptest.ResponseRecorder) {
		t.Helper()
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
		}
		if decodeBody(t, recorder)["user_id"] != f.userID.String() {
			t.Fatalf("unexpected body %s", recorder.Body.String())
		}
		cookie := recorder.Header().Get("Set-Cookie")
		if !strings.Contains(cookie, sessionCookieName+"=") || !strings.Contains(cookie, "HttpOnly") {
			t.Fatalf("session cookie not set: %q", cookie)
		}
		if _, err := f.handler.state.getTOTPLogin(f.challengeID); !errors.Is(err, badger.ErrKeyNotFound) {
			t.Fatalf("challenge not consumed: %v", err)
		}
		if locked, err := f.handler.state.isTOTPLoginLocked(f.userID); err != nil || locked {
			t.Fatalf("login attempts not cleared: locked=%v err=%v", locked, err)
		}
	}

	t.Run("valid authenticator code creates a session", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		if err := f.handler.state.putJSON(badgerTOTPLoginAttemptsPrefix+f.userID.String(),
			totpDisableAttemptState{Attempts: 2, CreatedAt: time.Now().UTC()}, totpLoginLockTTL); err != nil {
			t.Fatalf("seed attempts: %v", err)
		}
		expectUser(f, encryptedTestVault(t, f.handler, f.vault))
		f.mock.ExpectExec(updateUserQuery + `.*totp_secret_encrypted = '`).WillReturnResult(sqlmock.NewResult(0, 1))
		recorder := performJSON(t, f.handler.loginTOTP, nil, loginTOTPBody(f.challengeID, currentTOTPCode(t, f.vault.Secret)))
		assertSession(t, f, recorder)
	})

	t.Run("valid recovery code creates a session", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		expectUser(f, encryptedTestVault(t, f.handler, f.vault))
		f.mock.ExpectExec(updateUserQuery).WillReturnResult(sqlmock.NewResult(0, 1))
		recorder := performJSON(t, f.handler.loginTOTP, nil, loginTOTPBody(f.challengeID, f.codes[0]))
		assertSession(t, f, recorder)
	})

	t.Run("lost concurrent update counts as a failure", func(t *testing.T) {
		f := prepare(t, time.Now().UTC())
		expectUser(f, encryptedTestVault(t, f.handler, f.vault))
		f.mock.ExpectExec(updateUserQuery).WillReturnResult(sqlmock.NewResult(0, 0))
		status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp",
			loginTOTPBody(f.challengeID, currentTOTPCode(t, f.vault.Secret)))
		assertErrorResponse(t, status, response, http.StatusUnauthorized, "authenticator or recovery code is incorrect")
	})

	for name, configure := range map[string]func(*sqlmock.ExpectedExec){
		"update fails":        func(e *sqlmock.ExpectedExec) { e.WillReturnError(errors.New("db down")) },
		"rows affected fails": func(e *sqlmock.ExpectedExec) { e.WillReturnResult(sqlmock.NewErrorResult(errors.New("driver"))) },
	} {
		t.Run(name, func(t *testing.T) {
			f := prepare(t, time.Now().UTC())
			expectUser(f, encryptedTestVault(t, f.handler, f.vault))
			configure(f.mock.ExpectExec(updateUserQuery))
			status, response := callJSONHandler(t, f.handler.loginTOTP, "/api/auth/login/totp",
				loginTOTPBody(f.challengeID, currentTOTPCode(t, f.vault.Secret)))
			assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not update two-factor state")
		})
	}
}

// opaqueLoginFinishBody registers testPassword with the handler's OPAQUE
// server, runs loginStart against a mocked user row, and returns the
// loginFinish request body that a real client would send.
func opaqueLoginFinishBody(t *testing.T, handler *authHandler, mock sqlmock.Sqlmock, userID uuid.UUID, username string) (string, uuid.UUID) {
	t.Helper()
	config := testOpaqueConfiguration()

	registrationClient, err := config.Client()
	if err != nil {
		t.Fatalf("create OPAQUE client: %v", err)
	}
	registrationRequest, err := registrationClient.RegistrationInit([]byte(testPassword))
	if err != nil {
		t.Fatalf("registration init: %v", err)
	}
	registrationResponse, err := handler.opaqueServer.RegistrationResponse(registrationRequest, []byte(username), nil)
	if err != nil {
		t.Fatalf("registration response: %v", err)
	}
	record, _, err := registrationClient.RegistrationFinalize(registrationResponse, nil, []byte(testServerID))
	if err != nil {
		t.Fatalf("registration finalize: %v", err)
	}

	loginClient, err := config.Client()
	if err != nil {
		t.Fatalf("create OPAQUE client: %v", err)
	}
	ke1, err := loginClient.GenerateKE1([]byte(testPassword))
	if err != nil {
		t.Fatalf("generate KE1: %v", err)
	}
	mock.ExpectQuery(selectUserQuery + `.*username = '` + username + `'`).WillReturnRows(userRows(testUser{
		id: userID, username: username, opaqueRecord: encodeMessage(record.Serialize()),
	}))
	startBody, _ := json.Marshal(loginStartRequest{Username: username, OpaqueRequest: encodeMessage(ke1.Serialize())})
	status, start := callJSONHandler(t, handler.loginStart, "/api/auth/login/start", string(startBody))
	if status != http.StatusOK {
		t.Fatalf("login start = %d %v", status, start)
	}
	ke2Bytes, err := decodeMessage(start["opaque_response"].(string), maxOpaqueMessage)
	if err != nil {
		t.Fatalf("decode KE2: %v", err)
	}
	ke2, err := loginClient.Deserialize.KE2(ke2Bytes)
	if err != nil {
		t.Fatalf("deserialize KE2: %v", err)
	}
	ke3, _, _, err := loginClient.GenerateKE3(ke2, nil, []byte(testServerID))
	if err != nil {
		t.Fatalf("generate KE3: %v", err)
	}
	loginID := uuid.MustParse(start["login_id"].(string))
	finishBody, _ := json.Marshal(loginFinishRequest{LoginID: loginID.String(), OpaqueFinish: encodeMessage(ke3.Serialize())})
	return string(finishBody), loginID
}

func TestLoginFinishTwoFactorBranch(t *testing.T) {
	setup := func(t *testing.T) (*authHandler, sqlmock.Sqlmock, uuid.UUID, string, uuid.UUID) {
		handler := newTestAuthHandler(t)
		mock := attachSQLMock(t, handler)
		userID := uuid.New()
		body, loginID := opaqueLoginFinishBody(t, handler, mock, userID, "test_user")
		return handler, mock, userID, body, loginID
	}

	t.Run("issues a challenge instead of a session", func(t *testing.T) {
		handler, mock, userID, body, loginID := setup(t)
		vault, _ := newTOTPVaultWithRecovery(t)
		mock.ExpectQuery(selectUserQuery + `.*deleted_at IS NULL`).WillReturnRows(userRows(testUser{
			id: userID, username: "test_user", encrypted: encryptedTestVault(t, handler, vault),
		}))
		recorder := performJSON(t, handler.loginFinish, nil, body)
		response := decodeBody(t, recorder)
		if recorder.Code != http.StatusOK || response["requires_totp"] != true {
			t.Fatalf("login finish = %d %v", recorder.Code, response)
		}
		if cookie := recorder.Header().Get("Set-Cookie"); cookie != "" {
			t.Fatalf("session cookie issued before second factor: %q", cookie)
		}
		challengeID := uuid.MustParse(response["challenge_id"].(string))
		challenge, err := handler.state.getTOTPLogin(challengeID)
		if err != nil || challenge.UserID != userID {
			t.Fatalf("challenge = %#v, err %v", challenge, err)
		}
		if _, err := handler.state.getLogin(loginID); !errors.Is(err, badger.ErrKeyNotFound) {
			t.Fatalf("OPAQUE login state not deleted: %v", err)
		}
	})

	t.Run("refuses a locked account", func(t *testing.T) {
		handler, mock, userID, body, _ := setup(t)
		if err := handler.state.putJSON(badgerTOTPLoginAttemptsPrefix+userID.String(),
			totpDisableAttemptState{Attempts: totpLoginMaxAttempts, CreatedAt: time.Now().UTC()}, totpLoginLockTTL); err != nil {
			t.Fatalf("lock account: %v", err)
		}
		vault, _ := newTOTPVaultWithRecovery(t)
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{
			id: userID, username: "test_user", encrypted: encryptedTestVault(t, handler, vault),
		}))
		status, response := callJSONHandler(t, handler.loginFinish, "/api/auth/login/finish", body)
		assertErrorResponse(t, status, response, http.StatusTooManyRequests, "too many incorrect codes; try again in 15 minutes")
	})

	t.Run("creates a session without two-factor authentication", func(t *testing.T) {
		handler, mock, userID, body, _ := setup(t)
		mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{id: userID, username: "test_user"}))
		recorder := performJSON(t, handler.loginFinish, nil, body)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Set-Cookie"), sessionCookieName+"=") {
			t.Fatalf("login finish = %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("reports an account lookup failure", func(t *testing.T) {
		handler, mock, _, body, _ := setup(t)
		mock.ExpectQuery(selectUserQuery).WillReturnError(errors.New("db down"))
		status, response := callJSONHandler(t, handler.loginFinish, "/api/auth/login/finish", body)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not load account")
	})
}

func TestCurrentUserReportsTOTPStatus(t *testing.T) {
	handler, mock := newSQLMockTOTPHandler(t)
	userID := uuid.New()
	vault, _ := newTOTPVaultWithRecovery(t)

	mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{
		id: userID, username: "test_user", encrypted: encryptedTestVault(t, handler, vault),
	}))
	status, response := callAuthenticatedJSONHandler(t, handler.currentUser, userID, ``)
	if status != http.StatusOK || response["totp_enabled"] != true || response["username"] != "test_user" {
		t.Fatalf("current user = %d %v", status, response)
	}
	if _, leaked := response["totp_secret_encrypted"]; leaked {
		t.Fatal("current user response exposes the encrypted TOTP vault")
	}

	mock.ExpectQuery(selectUserQuery).WillReturnRows(userRows(testUser{id: userID, username: "test_user"}))
	if _, response := callAuthenticatedJSONHandler(t, handler.currentUser, userID, ``); response["totp_enabled"] != false {
		t.Fatalf("totp_enabled = %v, want false", response["totp_enabled"])
	}

	mock.ExpectQuery(selectUserQuery).WillReturnRows(emptyUserRows())
	status, response = callAuthenticatedJSONHandler(t, handler.currentUser, userID, ``)
	assertErrorResponse(t, status, response, http.StatusNotFound, "user not found")
}
