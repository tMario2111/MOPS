package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func newTestTOTPHandler(t *testing.T) *authHandler {
	t.Helper()
	return &authHandler{
		state:             newTestBadgerStore(t),
		totpEncryptionKey: make([]byte, totpEncryptionKeySize),
	}
}

// callAuthenticatedJSONHandler mirrors callJSONHandler but sets the user_id
// context value that requireSession would normally provide.
func callAuthenticatedJSONHandler(t *testing.T, handle gin.HandlerFunc, userID uuid.UUID, body string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/auth/totp", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user_id", userID)

	handle(context)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return recorder.Code, response
}

func putRawState(t *testing.T, store *badgerStore, key, value string) {
	t.Helper()
	if err := store.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), []byte(value))
	}); err != nil {
		t.Fatalf("put raw state %q: %v", key, err)
	}
}

func putTestTOTPSetup(t *testing.T, handler *authHandler, userID uuid.UUID, secret string, created time.Time) {
	t.Helper()
	encrypted, err := encryptTOTPVault(handler.totpEncryptionKey, totpVault{Secret: secret})
	if err != nil {
		t.Fatalf("encrypt vault: %v", err)
	}
	if err := handler.state.putTOTPSetup(userID, pendingTOTPSetupState{
		EncryptedVault: encrypted,
		Created:        created,
	}); err != nil {
		t.Fatalf("put TOTP setup: %v", err)
	}
}

func wrongTOTPCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	for _, candidate := range []string{"000000", "111111", "222222", "333333"} {
		if _, valid, err := verifyTOTPCode(secret, candidate, now, -1); err == nil && !valid {
			return candidate
		}
	}
	t.Fatal("could not find an invalid TOTP code")
	return ""
}

func assertErrorResponse(t *testing.T, status int, response map[string]any, wantStatus int, wantError string) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("status = %d, want %d (response %v)", status, wantStatus, response)
	}
	if response["error"] != wantError {
		t.Fatalf("error = %q, want %q", response["error"], wantError)
	}
}

func TestNewAuthHandlerRejectsInvalidTOTPEncryptionKey(t *testing.T) {
	// Populate valid OPAQUE material first, then override the TOTP key.
	newTestAuthHandler(t)
	t.Setenv("TOTP_ENCRYPTION_KEY", "")
	if _, err := newAuthHandler(nil, newTestBadgerStore(t)); err == nil {
		t.Fatal("missing TOTP encryption key unexpectedly accepted")
	}
}

func TestTOTPHandlersRequireEncryptionKey(t *testing.T) {
	handler := &authHandler{state: newTestBadgerStore(t)}
	handlers := map[string]gin.HandlerFunc{
		"setup":   handler.totpSetup,
		"enable":  handler.totpEnable,
		"disable": handler.totpDisable,
		"login":   handler.loginTOTP,
	}
	for name, handle := range handlers {
		t.Run(name, func(t *testing.T) {
			status, response := callAuthenticatedJSONHandler(t, handle, uuid.New(), `{"code":"123456"}`)
			assertErrorResponse(t, status, response, http.StatusServiceUnavailable, "two-factor authentication is not configured")
		})
	}
}

func TestTOTPEnableRejectsInvalidRequests(t *testing.T) {
	handler := newTestTOTPHandler(t)

	t.Run("invalid body", func(t *testing.T) {
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, uuid.New(), `{`)
		assertErrorResponse(t, status, response, http.StatusBadRequest, "invalid request body")
	})

	t.Run("missing setup", func(t *testing.T) {
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, uuid.New(), `{"code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusGone, "authenticator setup expired; start setup again")
	})

	t.Run("corrupt setup state", func(t *testing.T) {
		userID := uuid.New()
		putRawState(t, handler.state, badgerTOTPSetupPrefix+userID.String(), "not json")
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, `{"code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not load authenticator setup")
	})

	t.Run("undecryptable vault", func(t *testing.T) {
		userID := uuid.New()
		if err := handler.state.putTOTPSetup(userID, pendingTOTPSetupState{
			EncryptedVault: "not-a-vault",
			Created:        time.Now().UTC(),
		}); err != nil {
			t.Fatalf("put TOTP setup: %v", err)
		}
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, `{"code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not read authenticator setup")
	})

	t.Run("invalid secret in vault", func(t *testing.T) {
		userID := uuid.New()
		putTestTOTPSetup(t, handler, userID, "not base32!", time.Now().UTC())
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, `{"code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not verify authenticator code")
	})

	t.Run("setup expired between load and failure", func(t *testing.T) {
		userID := uuid.New()
		secret, err := generateTOTPSecret()
		if err != nil {
			t.Fatalf("generate secret: %v", err)
		}
		putTestTOTPSetup(t, handler, userID, secret, time.Now().UTC().Add(-totpSetupStateTTL-time.Second))
		body := `{"code":"` + wrongTOTPCode(t, secret, time.Now()) + `"}`
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, body)
		assertErrorResponse(t, status, response, http.StatusGone, "authenticator setup expired; start setup again")
	})
}

func TestTOTPEnableCountsIncorrectCodes(t *testing.T) {
	handler := newTestTOTPHandler(t)
	userID := uuid.New()
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	putTestTOTPSetup(t, handler, userID, secret, time.Now().UTC())
	body := `{"code":" ` + wrongTOTPCode(t, secret, time.Now()) + ` "}`

	for attempt := 1; attempt < totpMaxAttempts; attempt++ {
		status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, body)
		assertErrorResponse(t, status, response, http.StatusBadRequest, "authenticator code is incorrect")
		if response["attempts_remaining"] != float64(totpMaxAttempts-attempt) {
			t.Fatalf("attempt %d remaining = %v", attempt, response["attempts_remaining"])
		}
	}
	status, response := callAuthenticatedJSONHandler(t, handler.totpEnable, userID, body)
	assertErrorResponse(t, status, response, http.StatusGone, "too many incorrect codes; start authenticator setup again")
	if _, err := handler.state.getTOTPSetup(userID); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("setup state survived exhausted attempts: %v", err)
	}
}

func TestTOTPDisableRejectsBeforeLoadingAccount(t *testing.T) {
	handler := newTestTOTPHandler(t)

	t.Run("locked after failed attempts", func(t *testing.T) {
		userID := uuid.New()
		for attempt := 0; attempt < totpDisableMaxAttempts; attempt++ {
			_, _ = handler.state.failTOTPDisable(userID)
		}
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, `{"code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusTooManyRequests, "too many incorrect codes; try again later")
	})

	t.Run("corrupt attempt state", func(t *testing.T) {
		userID := uuid.New()
		putRawState(t, handler.state, badgerTOTPDisablePrefix+userID.String(), "not json")
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, userID, `{"code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not check authenticator attempts")
	})

	t.Run("invalid body", func(t *testing.T) {
		status, response := callAuthenticatedJSONHandler(t, handler.totpDisable, uuid.New(), `{`)
		assertErrorResponse(t, status, response, http.StatusBadRequest, "invalid request body")
	})
}

func TestLoginTOTPRejectsLockedAndCorruptState(t *testing.T) {
	handler := newTestTOTPHandler(t)

	t.Run("corrupt challenge", func(t *testing.T) {
		challengeID := uuid.New()
		putRawState(t, handler.state, badgerTOTPLoginPrefix+challengeID.String(), "not json")
		status, response := callJSONHandler(t, handler.loginTOTP, "/api/auth/login/totp",
			`{"challenge_id":"`+challengeID.String()+`","code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not load two-factor challenge")
	})

	t.Run("corrupt attempt state", func(t *testing.T) {
		userID, challengeID := uuid.New(), uuid.New()
		if err := handler.state.putTOTPLogin(challengeID, pendingTOTPLoginState{UserID: userID, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("put challenge: %v", err)
		}
		putRawState(t, handler.state, badgerTOTPLoginAttemptsPrefix+userID.String(), "not json")
		status, response := callJSONHandler(t, handler.loginTOTP, "/api/auth/login/totp",
			`{"challenge_id":"`+challengeID.String()+`","code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusInternalServerError, "could not check two-factor attempts")
	})

	t.Run("locked account consumes challenge", func(t *testing.T) {
		userID, challengeID := uuid.New(), uuid.New()
		if err := handler.state.putTOTPLogin(challengeID, pendingTOTPLoginState{UserID: userID, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("put challenge: %v", err)
		}
		if err := handler.state.putJSON(badgerTOTPLoginAttemptsPrefix+userID.String(),
			totpDisableAttemptState{Attempts: totpLoginMaxAttempts, CreatedAt: time.Now().UTC()}, totpLoginLockTTL); err != nil {
			t.Fatalf("lock account: %v", err)
		}
		status, response := callJSONHandler(t, handler.loginTOTP, "/api/auth/login/totp",
			`{"challenge_id":"`+challengeID.String()+`","code":"123456"}`)
		assertErrorResponse(t, status, response, http.StatusTooManyRequests, "too many incorrect codes; try again in 15 minutes")
		if _, err := handler.state.getTOTPLogin(challengeID); !errors.Is(err, badger.ErrKeyNotFound) {
			t.Fatalf("locked challenge was not consumed: %v", err)
		}
	})
}

func TestVerifyTOTPFactor(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	codes, hashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	vault := totpVault{Secret: secret, RecoveryCodeHashes: hashes, LastAcceptedStep: -1}

	code, err := generateTOTPCode(secret, now)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	step, recovery, valid, err := verifyTOTPFactor(&vault, code, now)
	if err != nil || !valid || recovery || step != now.Unix()/totpPeriodSeconds {
		t.Fatalf("TOTP factor = (%d, %v, %v, %v), want current step", step, recovery, valid, err)
	}

	step, recovery, valid, err = verifyTOTPFactor(&vault, codes[0], now)
	if err != nil || !valid || !recovery || step != 0 {
		t.Fatalf("recovery factor = (%d, %v, %v, %v), want recovery", step, recovery, valid, err)
	}
	if len(vault.RecoveryCodeHashes) != totpRecoveryCodeCount-1 {
		t.Fatalf("recovery code was not consumed: %d left", len(vault.RecoveryCodeHashes))
	}

	if _, _, valid, err := verifyTOTPFactor(&vault, codes[0], now); err != nil || valid {
		t.Fatalf("reused recovery code = (%v, %v), want rejected", valid, err)
	}

	broken := totpVault{Secret: "not base32!"}
	if _, _, _, err := verifyTOTPFactor(&broken, "123456", now); err == nil {
		t.Fatal("invalid secret did not return an error")
	}
}

func TestTOTPProvisioningURI(t *testing.T) {
	uri := totpProvisioningURI("alice_01", "JBSWY3DPEHPK3PXP")
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse provisioning URI: %v", err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Fatalf("unexpected URI prefix: %q", uri)
	}
	if parsed.Path != "/"+totpIssuer+":alice_01" {
		t.Fatalf("label = %q", parsed.Path)
	}
	query := parsed.Query()
	want := map[string]string{
		"secret":    "JBSWY3DPEHPK3PXP",
		"issuer":    totpIssuer,
		"algorithm": "SHA1",
		"digits":    "6",
		"period":    "30",
	}
	for key, value := range want {
		if query.Get(key) != value {
			t.Fatalf("%s = %q, want %q", key, query.Get(key), value)
		}
	}
}

func TestTOTPVaultRejectsInvalidInput(t *testing.T) {
	key := make([]byte, totpEncryptionKeySize)

	if _, err := encryptTOTPVault(key[:16], totpVault{}); err == nil {
		t.Fatal("encrypt accepted short key")
	}
	if _, err := decryptTOTPVault(key[:16], ""); err == nil {
		t.Fatal("decrypt accepted short key")
	}
	if _, err := decryptTOTPVault(key, "%%%"); err == nil {
		t.Fatal("decrypt accepted invalid base64")
	}
	if _, err := decryptTOTPVault(key, base64.RawURLEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("decrypt accepted truncated vault")
	}

	encrypted, err := encryptTOTPVault(key, totpVault{Secret: "JBSWY3DPEHPK3PXP"})
	if err != nil {
		t.Fatalf("encrypt vault: %v", err)
	}
	otherKey := make([]byte, totpEncryptionKeySize)
	otherKey[0] = 1
	if _, err := decryptTOTPVault(otherKey, encrypted); err == nil {
		t.Fatal("decrypt accepted vault encrypted with another key")
	}

	// A correctly authenticated ciphertext whose plaintext is not JSON.
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("create GCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	sealed := gcm.Seal(nil, nonce, []byte("not json"), []byte(totpSecretAAD))
	if _, err := decryptTOTPVault(key, base64.RawURLEncoding.EncodeToString(append(nonce, sealed...))); err == nil {
		t.Fatal("decrypt accepted non-JSON plaintext")
	}
}

func TestTOTPCodeRejectsInvalidSecretAndWrongLength(t *testing.T) {
	if _, err := generateTOTPCode("not base32!", time.Now()); err == nil {
		t.Fatal("generate accepted invalid secret")
	}
	if _, valid, err := verifyTOTPCode("not base32!", "123456", time.Now(), -1); err == nil || valid {
		t.Fatalf("verify with invalid secret = (%v, %v), want error", valid, err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	if _, valid, err := verifyTOTPCode(secret, "12345", time.Now(), -1); err != nil || valid {
		t.Fatalf("short code = (%v, %v), want rejected", valid, err)
	}
	// Accepts codes from the previous step (clock skew) but not two steps back.
	now := time.Unix(1_700_000_010, 0)
	previous, _ := generateTOTPCode(secret, now.Add(-totpPeriodSeconds*time.Second))
	if _, valid, _ := verifyTOTPCode(secret, previous, now, -1); !valid {
		t.Fatal("previous-step code was rejected")
	}
	stale, _ := generateTOTPCode(secret, now.Add(-2*totpPeriodSeconds*time.Second))
	if _, valid, _ := verifyTOTPCode(secret, stale, now, -1); valid {
		t.Fatal("code from two steps back was accepted")
	}
}

func TestDecodeTOTPEncryptionKeyRejectsInvalidBase64(t *testing.T) {
	if _, err := decodeTOTPEncryptionKey("not base64!"); err == nil {
		t.Fatal("invalid base64 key unexpectedly accepted")
	}
}

func TestTOTPStateCleanupAndMissingState(t *testing.T) {
	store := newTestBadgerStore(t)
	userID, challengeID := uuid.New(), uuid.New()

	if err := store.putTOTPSetup(userID, pendingTOTPSetupState{Created: time.Now().UTC()}); err != nil {
		t.Fatalf("put setup: %v", err)
	}
	if err := store.deleteTOTPSetup(userID); err != nil {
		t.Fatalf("delete setup: %v", err)
	}
	if _, err := store.getTOTPSetup(userID); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("setup survived delete: %v", err)
	}
	if _, err := store.failTOTPSetup(userID); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("fail missing setup = %v, want not found", err)
	}

	if err := store.putTOTPLogin(challengeID, pendingTOTPLoginState{UserID: userID, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("put challenge: %v", err)
	}
	if err := store.consumeTOTPLogin(challengeID); err != nil {
		t.Fatalf("consume challenge: %v", err)
	}
	if _, err := store.failTOTPLogin(challengeID); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("fail consumed challenge = %v, want not found", err)
	}

	expiredID := uuid.New()
	if err := store.putTOTPLogin(expiredID, pendingTOTPLoginState{
		UserID:    uuid.New(),
		CreatedAt: time.Now().UTC().Add(-totpLoginChallengeTTL - time.Second),
	}); err != nil {
		t.Fatalf("put expired challenge: %v", err)
	}
	if _, err := store.failTOTPLogin(expiredID); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("fail expired challenge = %v, want not found", err)
	}

	if _, err := store.failTOTPDisable(userID); err != nil {
		t.Fatalf("fail disable: %v", err)
	}
	if err := store.clearTOTPDisableAttempts(userID); err != nil {
		t.Fatalf("clear disable attempts: %v", err)
	}
	if _, err := store.getTOTPDisableAttempts(userID); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("disable attempts survived clear: %v", err)
	}

	putRawState(t, store, badgerTOTPLoginAttemptsPrefix+userID.String(), "not json")
	if _, err := store.isTOTPLoginLocked(userID); err == nil {
		t.Fatal("corrupt login attempts state was accepted")
	}
}
