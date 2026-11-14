package main

import (
	"errors"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"
)

func newTestBadgerStore(t *testing.T) *badgerStore {
	t.Helper()
	db, err := badger.Open(badger.DefaultOptions("").WithInMemory(true).WithLogger(nil))
	if err != nil {
		t.Fatalf("open in-memory Badger: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close in-memory Badger: %v", err)
		}
	})
	return &badgerStore{db: db}
}

func TestSessionTokenIsHashedAndRevocable(t *testing.T) {
	store := newTestBadgerStore(t)
	userID := uuid.New()

	rawToken, state, err := newSession(store, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if len(rawToken) != 43 {
		t.Fatalf("expected 43-character base64url token, got %d", len(rawToken))
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(badgerSessionPrefix + rawToken))
		if err != badger.ErrKeyNotFound {
			t.Fatalf("raw token was stored as a key: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect session keys: %v", err)
	}

	session, err := store.getSession(rawToken)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.State.ID != state.ID || session.State.UserID != userID {
		t.Fatal("stored session did not round-trip")
	}

	if err := store.revokeSession(rawToken); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := store.getSession(rawToken); err != badger.ErrKeyNotFound {
		t.Fatalf("expected revoked session to be unavailable, got %v", err)
	}
}

func TestTemporaryStateRoundTrip(t *testing.T) {
	store := newTestBadgerStore(t)
	id := uuid.New()
	expected := pendingRegistrationState{
		Username: "test-user",
		Email:    "test@example.com",
	}

	if err := store.putRegistration(id, expected); err != nil {
		t.Fatalf("put registration state: %v", err)
	}
	actual, err := store.getRegistration(id)
	if err != nil {
		t.Fatalf("get registration state: %v", err)
	}
	if actual.Username != expected.Username || actual.Email != expected.Email {
		t.Fatal("registration state did not round-trip")
	}
	if err := store.deleteRegistration(id); err != nil {
		t.Fatalf("delete registration state: %v", err)
	}
	if _, err := store.getRegistration(id); err != badger.ErrKeyNotFound {
		t.Fatalf("expected deleted registration state to be unavailable, got %v", err)
	}
}

func TestRegistrationReservationsAreExclusiveAndReleased(t *testing.T) {
	store := newTestBadgerStore(t)
	firstID := uuid.New()
	first := pendingRegistrationState{
		Username: "test_user",
		Email:    "test@example.com",
		Created:  time.Now().UTC(),
	}
	if err := store.reserveRegistration(firstID, first); err != nil {
		t.Fatalf("reserve first registration: %v", err)
	}

	if err := store.reserveRegistration(uuid.New(), pendingRegistrationState{
		Username: "test_user",
		Email:    "other@example.com",
		Created:  time.Now().UTC(),
	}); !errors.Is(err, errUsernameRegistrationPending) {
		t.Fatalf("duplicate username reservation error = %v", err)
	}
	if err := store.reserveRegistration(uuid.New(), pendingRegistrationState{
		Username: "other_user",
		Email:    "test@example.com",
		Created:  time.Now().UTC(),
	}); !errors.Is(err, errEmailRegistrationPending) {
		t.Fatalf("duplicate email reservation error = %v", err)
	}

	if err := store.deleteRegistration(firstID); err != nil {
		t.Fatalf("delete first registration: %v", err)
	}
	if err := store.reserveRegistration(uuid.New(), pendingRegistrationState{
		Username: "test_user",
		Email:    "test@example.com",
		Created:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
}

func TestRegistrationVerificationAttempts(t *testing.T) {
	store := newTestBadgerStore(t)
	id := uuid.New()
	code := "AB12CD34"
	if err := store.putRegistration(id, pendingRegistrationState{
		Email:                "test@example.com",
		VerificationCodeHash: hashVerificationCode(code),
		Created:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	for attempt := 1; attempt <= verificationMaxAttempts; attempt++ {
		result, err := store.verifyRegistration(id, "WRONG123")
		if attempt == verificationMaxAttempts {
			if !errors.Is(err, errVerificationAttemptsExceeded) {
				t.Fatalf("final invalid verification attempt error = %v, want attempts exceeded", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("invalid verification attempt %d: %v", attempt, err)
		}
		if result.Verified {
			t.Fatalf("invalid verification attempt %d succeeded", attempt)
		}
	}
	if _, err := store.getRegistration(id); err != badger.ErrKeyNotFound {
		t.Fatalf("expected registration to be invalidated after %d attempts, got %v", verificationMaxAttempts, err)
	}
}

func TestRegistrationVerificationSucceeds(t *testing.T) {
	store := newTestBadgerStore(t)
	id := uuid.New()
	code := "AB12CD34"
	if err := store.putRegistration(id, pendingRegistrationState{
		VerificationCodeHash: hashVerificationCode(code),
		Created:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	result, err := store.verifyRegistration(id, code)
	if err != nil {
		t.Fatalf("verify registration: %v", err)
	}
	if !result.Verified {
		t.Fatal("expected verification to succeed")
	}
	state, err := store.getRegistration(id)
	if err != nil {
		t.Fatalf("get verified registration: %v", err)
	}
	if !state.EmailVerified {
		t.Fatal("registration was not marked as verified")
	}

	result, err = store.verifyRegistration(id, "WRONG123")
	if err != nil {
		t.Fatalf("re-verification of verified registration: %v", err)
	}
	if !result.Verified {
		t.Fatal("verified registration was not idempotent")
	}
}

func TestRegistrationVerificationRejectsMalformedHash(t *testing.T) {
	store := newTestBadgerStore(t)
	id := uuid.New()
	if err := store.putRegistration(id, pendingRegistrationState{
		VerificationCodeHash: "not-a-hash",
		Created:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	if _, err := store.verifyRegistration(id, "AB12CD34"); err == nil {
		t.Fatal("malformed verification hash unexpectedly accepted")
	}
}

func TestRegistrationVerificationExpiredState(t *testing.T) {
	store := newTestBadgerStore(t)
	id := uuid.New()
	if err := store.putRegistration(id, pendingRegistrationState{
		VerificationCodeHash: hashVerificationCode("AB12CD34"),
		Created:              time.Now().UTC().Add(-registrationStateTTL),
	}); err != nil {
		t.Fatalf("put registration state: %v", err)
	}

	if _, err := store.verifyRegistration(id, "AB12CD34"); err == nil {
		t.Fatal("expired verification unexpectedly succeeded")
	}
}

func TestVerificationCodeFormat(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := generateVerificationCode()
		if err != nil {
			t.Fatalf("generate verification code: %v", err)
		}
		if !verificationCodePattern.MatchString(code) {
			t.Fatalf("generated invalid verification code %q", code)
		}
	}
}
