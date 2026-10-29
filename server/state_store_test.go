package main

import (
	"testing"

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
