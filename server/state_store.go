package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	badgerRegistrationPrefix = "registration:"
	badgerLoginPrefix        = "login:"
	badgerSessionPrefix      = "session:"
	registrationStateTTL     = 10 * time.Minute
	loginStateTTL            = 10 * time.Minute
	sessionAbsoluteTTL       = 30 * 24 * time.Hour
	sessionIdleTTL           = 24 * time.Hour
	sessionCookieName        = "mops_session"
)

type badgerStore struct {
	db *badger.DB
}

type pendingRegistrationState struct {
	Username string    `json:"username"`
	Email    string    `json:"email"`
	Created  time.Time `json:"created"`
}

type pendingLoginState struct {
	UserID            uuid.UUID `json:"user_id"`
	ExpectedClientMAC string    `json:"expected_client_mac"`
	Created           time.Time `json:"created"`
}

type sessionState struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type authenticatedSession struct {
	State sessionState
}

func openBadgerStore() (*badgerStore, error) {
	path := strings.TrimSpace(os.Getenv("BADGER_PATH"))
	if path == "" {
		path = "./data/badger"
	}

	db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil))
	if err != nil {
		return nil, fmt.Errorf("open badger store: %w", err)
	}
	return &badgerStore{db: db}, nil
}

func (s *badgerStore) close() error {
	return s.db.Close()
}

func (s *badgerStore) putJSON(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return s.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), data).WithTTL(ttl)
		return txn.SetEntry(entry)
	})
}

func (s *badgerStore) getJSON(key string, target any) error {
	return s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(value []byte) error {
			return json.Unmarshal(value, target)
		})
	})
}

func (s *badgerStore) delete(key string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

func (s *badgerStore) putRegistration(id uuid.UUID, state pendingRegistrationState) error {
	return s.putJSON(badgerRegistrationPrefix+id.String(), state, registrationStateTTL)
}

func (s *badgerStore) getRegistration(id uuid.UUID) (pendingRegistrationState, error) {
	var state pendingRegistrationState
	err := s.getJSON(badgerRegistrationPrefix+id.String(), &state)
	return state, err
}

func (s *badgerStore) deleteRegistration(id uuid.UUID) error {
	return s.delete(badgerRegistrationPrefix + id.String())
}

func (s *badgerStore) putLogin(id uuid.UUID, state pendingLoginState) error {
	return s.putJSON(badgerLoginPrefix+id.String(), state, loginStateTTL)
}

func (s *badgerStore) getLogin(id uuid.UUID) (pendingLoginState, error) {
	var state pendingLoginState
	err := s.getJSON(badgerLoginPrefix+id.String(), &state)
	return state, err
}

func (s *badgerStore) deleteLogin(id uuid.UUID) error {
	return s.delete(badgerLoginPrefix + id.String())
}

func newSession(store *badgerStore, userID uuid.UUID) (string, sessionState, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", sessionState{}, fmt.Errorf("generate session token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	now := time.Now().UTC()
	state := sessionState{
		ID:         uuid.New(),
		UserID:     userID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(sessionAbsoluteTTL),
	}
	if err := store.putJSON(sessionKey(rawToken), state, sessionAbsoluteTTL); err != nil {
		return "", sessionState{}, fmt.Errorf("store session: %w", err)
	}
	return rawToken, state, nil
}

func sessionKey(rawToken string) string {
	hash := sha256.Sum256([]byte(rawToken))
	return badgerSessionPrefix + hex.EncodeToString(hash[:])
}

func (s *badgerStore) getSession(rawToken string) (authenticatedSession, error) {
	var state sessionState
	if err := s.getJSON(sessionKey(rawToken), &state); err != nil {
		return authenticatedSession{}, err
	}
	now := time.Now().UTC()
	if now.After(state.ExpiresAt) || now.Sub(state.LastSeenAt) > sessionIdleTTL {
		_ = s.delete(sessionKey(rawToken))
		return authenticatedSession{}, badger.ErrKeyNotFound
	}
	return authenticatedSession{State: state}, nil
}

func (s *badgerStore) touchSession(rawToken string, session authenticatedSession) error {
	session.State.LastSeenAt = time.Now().UTC()
	remaining := time.Until(session.State.ExpiresAt)
	if remaining <= 0 {
		return s.delete(sessionKey(rawToken))
	}
	return s.putJSON(sessionKey(rawToken), session.State, remaining)
}

func (s *badgerStore) revokeSession(rawToken string) error {
	return s.delete(sessionKey(rawToken))
}

func (s *badgerStore) revokeUserSessions(userID uuid.UUID) error {
	return s.db.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerSessionPrefix)
		return iteratePrefix(txn, opts, func(key []byte) error {
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			var state sessionState
			if err := item.Value(func(value []byte) error {
				return json.Unmarshal(value, &state)
			}); err != nil {
				return err
			}
			if state.UserID == userID {
				return txn.Delete(key)
			}
			return nil
		})
	})
}

func iteratePrefix(
	txn *badger.Txn,
	options badger.IteratorOptions,
	fn func(key []byte) error,
) error {
	iterator := txn.NewIterator(options)
	defer iterator.Close()
	for iterator.Rewind(); iterator.Valid(); iterator.Next() {
		key := append([]byte(nil), iterator.Item().Key()...)
		if err := fn(key); err != nil {
			return err
		}
	}
	return nil
}

func requireSession(store *badgerStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken, err := c.Cookie(sessionCookieName)
		if err != nil || rawToken == "" {
			respondError(c, http.StatusUnauthorized, "authentication required")
			c.Abort()
			return
		}

		session, err := store.getSession(rawToken)
		if err != nil {
			clearSessionCookie(c)
			respondError(c, http.StatusUnauthorized, "invalid or expired session")
			c.Abort()
			return
		}
		if err := store.touchSession(rawToken, session); err != nil {
			respondError(c, http.StatusInternalServerError, "could not refresh session")
			c.Abort()
			return
		}

		c.Set("session", session)
		c.Set("user_id", session.State.UserID)
		c.Next()
	}
}

func setSessionCookie(c *gin.Context, rawToken string, expiresAt time.Time) {
	maxAge := max(0, int(time.Until(expiresAt).Seconds()))
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, rawToken, maxAge, "/", "", cookieSecure(), true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", cookieSecure(), true)
}

func cookieSecure() bool {
	value, err := strconv.ParseBool(os.Getenv("COOKIE_SECURE"))
	return err == nil && value
}

func badgerStateError(err error) bool {
	return errors.Is(err, badger.ErrKeyNotFound)
}
