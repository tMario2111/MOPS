package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	badgerRegistrationPrefix         = "registration:"
	badgerRegistrationUsernamePrefix = "registration-username:"
	badgerRegistrationEmailPrefix    = "registration-email:"
	badgerTOTPSetupPrefix            = "totp-setup:"
	badgerTOTPLoginPrefix            = "totp-login:"
	badgerTOTPLoginAttemptsPrefix    = "totp-login-attempts:"
	badgerTOTPDisablePrefix          = "totp-disable:"
	badgerLoginPrefix                = "login:"
	badgerSessionPrefix              = "session:"
	registrationStateTTL             = 10 * time.Minute
	verificationMaxAttempts          = 3
	loginStateTTL                    = 10 * time.Minute
	totpSetupStateTTL                = 10 * time.Minute
	totpLoginChallengeTTL            = 5 * time.Minute
	totpMaxAttempts                  = 5
	totpLoginMaxAttempts             = totpMaxAttempts
	totpLoginLockTTL                 = 15 * time.Minute
	totpDisableMaxAttempts           = totpMaxAttempts
	totpDisableLockTTL               = 15 * time.Minute
	sessionAbsoluteTTL               = 30 * 24 * time.Hour
	sessionIdleTTL                   = 24 * time.Hour
	sessionCookieName                = "mops_session"
)

type badgerStore struct {
	db *badger.DB
}

var errVerificationAttemptsExceeded = errors.New("verification attempts exceeded")
var errUsernameRegistrationPending = errors.New("username registration already pending")
var errEmailRegistrationPending = errors.New("email registration already pending")
var errTOTPLoginAttemptsExceeded = errors.New("TOTP login attempts exceeded")
var errTOTPSetupAttemptsExceeded = errors.New("TOTP setup attempts exceeded")
var errTOTPDisableAttemptsExceeded = errors.New("TOTP disable attempts exceeded")

type registrationVerificationResult struct {
	Verified          bool
	AttemptsRemaining int
}

type pendingRegistrationState struct {
	Username             string    `json:"username"`
	Email                string    `json:"email"`
	VerificationCodeHash string    `json:"verification_code_hash"`
	VerificationAttempts int       `json:"verification_attempts"`
	EmailVerified        bool      `json:"email_verified"`
	Created              time.Time `json:"created"`
}

type pendingLoginState struct {
	UserID            uuid.UUID `json:"user_id"`
	ExpectedClientMAC string    `json:"expected_client_mac"`
	Created           time.Time `json:"created"`
}

type pendingTOTPSetupState struct {
	EncryptedVault string    `json:"encrypted_vault"`
	Attempts       int       `json:"attempts"`
	Created        time.Time `json:"created"`
}

type pendingTOTPLoginState struct {
	UserID    uuid.UUID `json:"user_id"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}

type totpDisableAttemptState struct {
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
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

func (s *badgerStore) reserveRegistration(id uuid.UUID, state pendingRegistrationState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal registration state: %w", err)
	}
	usernameKey := registrationReservationKey(badgerRegistrationUsernamePrefix, state.Username)
	emailKey := registrationReservationKey(badgerRegistrationEmailPrefix, state.Email)
	return s.db.Update(func(txn *badger.Txn) error {
		if _, err := txn.Get([]byte(usernameKey)); err == nil {
			return errUsernameRegistrationPending
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if _, err := txn.Get([]byte(emailKey)); err == nil {
			return errEmailRegistrationPending
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		entry := badger.NewEntry([]byte(badgerRegistrationPrefix+id.String()), data).WithTTL(registrationStateTTL)
		if err := txn.SetEntry(entry); err != nil {
			return err
		}
		for _, key := range []string{usernameKey, emailKey} {
			if err := txn.SetEntry(badger.NewEntry([]byte(key), []byte(id.String())).WithTTL(registrationStateTTL)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *badgerStore) getRegistration(id uuid.UUID) (pendingRegistrationState, error) {
	var state pendingRegistrationState
	err := s.getJSON(badgerRegistrationPrefix+id.String(), &state)
	return state, err
}

func (s *badgerStore) deleteRegistration(id uuid.UUID) error {
	state, err := s.getRegistration(id)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return deleteRegistrationEntries(txn, id, state)
	})
}

func (s *badgerStore) verifyRegistration(id uuid.UUID, code string) (registrationVerificationResult, error) {
	result := registrationVerificationResult{AttemptsRemaining: verificationMaxAttempts}
	attemptsExceeded := false
	err := s.db.Update(func(txn *badger.Txn) error {
		key := []byte(badgerRegistrationPrefix + id.String())
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var state pendingRegistrationState
		if err := item.Value(func(value []byte) error {
			return json.Unmarshal(value, &state)
		}); err != nil {
			return err
		}
		if state.EmailVerified {
			result.Verified = true
			result.AttemptsRemaining = verificationMaxAttempts - state.VerificationAttempts
			return nil
		}
		result.AttemptsRemaining = verificationMaxAttempts - state.VerificationAttempts

		codeHash := sha256.Sum256([]byte(code))
		expectedHash, err := hex.DecodeString(state.VerificationCodeHash)
		if err != nil {
			return fmt.Errorf("decode verification code hash: %w", err)
		}
		if subtle.ConstantTimeCompare(codeHash[:], expectedHash) != 1 {
			state.VerificationAttempts++
			if state.VerificationAttempts >= verificationMaxAttempts {
				attemptsExceeded = true
				return deleteRegistrationEntries(txn, id, state)
			}

			result.AttemptsRemaining = verificationMaxAttempts - state.VerificationAttempts
		} else {
			state.EmailVerified = true
			result.Verified = true
		}

		remaining := time.Until(state.Created.Add(registrationStateTTL))
		if remaining <= 0 {
			return badger.ErrKeyNotFound
		}
		data, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("marshal registration state: %w", err)
		}
		return txn.SetEntry(badger.NewEntry(key, data).WithTTL(remaining))
	})
	if err == nil && attemptsExceeded {
		err = errVerificationAttemptsExceeded
	}
	return result, err
}

func deleteRegistrationEntries(txn *badger.Txn, id uuid.UUID, state pendingRegistrationState) error {
	for _, key := range []string{
		badgerRegistrationPrefix + id.String(),
		registrationReservationKey(badgerRegistrationUsernamePrefix, state.Username),
		registrationReservationKey(badgerRegistrationEmailPrefix, state.Email),
	} {
		if err := txn.Delete([]byte(key)); err != nil {
			return err
		}
	}
	return nil
}

func registrationReservationKey(prefix, value string) string {
	hash := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(hash[:])
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

func (s *badgerStore) putTOTPSetup(userID uuid.UUID, state pendingTOTPSetupState) error {
	return s.putJSON(badgerTOTPSetupPrefix+userID.String(), state, totpSetupStateTTL)
}

func (s *badgerStore) getTOTPSetup(userID uuid.UUID) (pendingTOTPSetupState, error) {
	var state pendingTOTPSetupState
	err := s.getJSON(badgerTOTPSetupPrefix+userID.String(), &state)
	return state, err
}

func (s *badgerStore) deleteTOTPSetup(userID uuid.UUID) error {
	return s.delete(badgerTOTPSetupPrefix + userID.String())
}

func (s *badgerStore) failTOTPSetup(userID uuid.UUID) (int, error) {
	attemptsExceeded := false
	attemptsRemaining := 0
	err := s.db.Update(func(txn *badger.Txn) error {
		key := []byte(badgerTOTPSetupPrefix + userID.String())
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		var state pendingTOTPSetupState
		if err := item.Value(func(value []byte) error {
			return json.Unmarshal(value, &state)
		}); err != nil {
			return err
		}
		state.Attempts++
		attemptsRemaining = totpMaxAttempts - state.Attempts
		if attemptsRemaining <= 0 {
			attemptsExceeded = true
			return txn.Delete(key)
		}
		remaining := time.Until(state.Created.Add(totpSetupStateTTL))
		if remaining <= 0 {
			return badger.ErrKeyNotFound
		}
		data, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("marshal TOTP setup state: %w", err)
		}
		return txn.SetEntry(badger.NewEntry(key, data).WithTTL(remaining))
	})
	if err == nil && attemptsExceeded {
		err = errTOTPSetupAttemptsExceeded
	}
	return attemptsRemaining, err
}

func (s *badgerStore) putTOTPLogin(id uuid.UUID, state pendingTOTPLoginState) error {
	return s.putJSON(badgerTOTPLoginPrefix+id.String(), state, totpLoginChallengeTTL)
}

func (s *badgerStore) getTOTPLogin(id uuid.UUID) (pendingTOTPLoginState, error) {
	var state pendingTOTPLoginState
	err := s.getJSON(badgerTOTPLoginPrefix+id.String(), &state)
	return state, err
}

func (s *badgerStore) consumeTOTPLogin(id uuid.UUID) error {
	return s.delete(badgerTOTPLoginPrefix + id.String())
}

func (s *badgerStore) failTOTPLogin(id uuid.UUID) (int, error) {
	attemptsExceeded := false
	attemptsRemaining := 0
	err := s.db.Update(func(txn *badger.Txn) error {
		challengeKey := []byte(badgerTOTPLoginPrefix + id.String())
		item, err := txn.Get(challengeKey)
		if err != nil {
			return err
		}
		var challenge pendingTOTPLoginState
		if err := item.Value(func(value []byte) error {
			return json.Unmarshal(value, &challenge)
		}); err != nil {
			return err
		}
		loginAttemptsKey := []byte(badgerTOTPLoginAttemptsPrefix + challenge.UserID.String())
		loginAttempts := totpDisableAttemptState{CreatedAt: time.Now().UTC()}
		attemptsItem, err := txn.Get(loginAttemptsKey)
		if err == nil {
			if err := attemptsItem.Value(func(value []byte) error {
				return json.Unmarshal(value, &loginAttempts)
			}); err != nil {
				return err
			}
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if loginAttempts.Attempts >= totpLoginMaxAttempts {
			attemptsExceeded = true
			attemptsRemaining = 0
			return txn.Delete(challengeKey)
		}

		loginAttempts.Attempts++
		attemptsRemaining = totpLoginMaxAttempts - loginAttempts.Attempts
		if attemptsRemaining <= 0 {
			attemptsExceeded = true
			loginAttempts.CreatedAt = time.Now().UTC()
		}

		lockRemaining := time.Until(loginAttempts.CreatedAt.Add(totpLoginLockTTL))
		if lockRemaining <= 0 {
			loginAttempts.CreatedAt = time.Now().UTC()
			lockRemaining = totpLoginLockTTL
		}
		loginAttemptsData, err := json.Marshal(loginAttempts)
		if err != nil {
			return fmt.Errorf("marshal TOTP login attempts: %w", err)
		}
		if err := txn.SetEntry(badger.NewEntry(loginAttemptsKey, loginAttemptsData).WithTTL(lockRemaining)); err != nil {
			return err
		}
		if attemptsExceeded {
			return txn.Delete(challengeKey)
		}

		challenge.Attempts++
		remaining := time.Until(challenge.CreatedAt.Add(totpLoginChallengeTTL))
		if remaining <= 0 {
			return badger.ErrKeyNotFound
		}
		data, err := json.Marshal(challenge)
		if err != nil {
			return fmt.Errorf("marshal TOTP login state: %w", err)
		}
		return txn.SetEntry(badger.NewEntry(challengeKey, data).WithTTL(remaining))
	})
	if err == nil && attemptsExceeded {
		err = errTOTPLoginAttemptsExceeded
	}
	return attemptsRemaining, err
}

func (s *badgerStore) isTOTPLoginLocked(userID uuid.UUID) (bool, error) {
	var state totpDisableAttemptState
	err := s.getJSON(badgerTOTPLoginAttemptsPrefix+userID.String(), &state)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return state.Attempts >= totpLoginMaxAttempts, nil
}

func (s *badgerStore) clearTOTPLoginAttempts(userID uuid.UUID) error {
	return s.delete(badgerTOTPLoginAttemptsPrefix + userID.String())
}

func (s *badgerStore) failTOTPDisable(userID uuid.UUID) (int, error) {
	attemptsExceeded := false
	attemptsRemaining := 0
	key := badgerTOTPDisablePrefix + userID.String()
	err := s.db.Update(func(txn *badger.Txn) error {
		state := totpDisableAttemptState{CreatedAt: time.Now().UTC()}
		item, err := txn.Get([]byte(key))
		if err == nil {
			if err := item.Value(func(value []byte) error {
				return json.Unmarshal(value, &state)
			}); err != nil {
				return err
			}
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		state.Attempts++
		attemptsRemaining = totpDisableMaxAttempts - state.Attempts
		if attemptsRemaining <= 0 {
			attemptsExceeded = true
			attemptsRemaining = 0
		}
		data, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("marshal TOTP disable state: %w", err)
		}
		return txn.SetEntry(badger.NewEntry([]byte(key), data).WithTTL(totpDisableLockTTL))
	})
	if err == nil && attemptsExceeded {
		err = errTOTPDisableAttemptsExceeded
	}
	return attemptsRemaining, err
}

func (s *badgerStore) getTOTPDisableAttempts(userID uuid.UUID) (int, error) {
	var state totpDisableAttemptState
	err := s.getJSON(badgerTOTPDisablePrefix+userID.String(), &state)
	return state.Attempts, err
}

func (s *badgerStore) clearTOTPDisableAttempts(userID uuid.UUID) error {
	return s.delete(badgerTOTPDisablePrefix + userID.String())
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
