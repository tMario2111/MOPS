package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type totpCodeRequest struct {
	Code string `json:"code"`
}

type totpLoginRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
}

func (h *authHandler) totpSetup(c *gin.Context) {
	if len(h.totpEncryptionKey) != totpEncryptionKeySize {
		respondError(c, http.StatusServiceUnavailable, "two-factor authentication is not configured")
		return
	}
	userID := c.MustGet("user_id").(uuid.UUID)
	var user User
	if err := h.db.NewSelect().Model(&user).Where("id = ?", userID).Scan(c.Request.Context()); err != nil {
		respondError(c, http.StatusNotFound, "account not found")
		return
	}
	if user.TOTPSecretEncrypted != nil && *user.TOTPSecretEncrypted != "" {
		respondError(c, http.StatusConflict, "two-factor authentication is already enabled")
		return
	}

	secret, err := generateTOTPSecret()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not create authenticator setup")
		return
	}
	recoveryCodes, recoveryHashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not create recovery codes")
		return
	}
	encryptedVault, err := encryptTOTPVault(h.totpEncryptionKey, totpVault{
		Secret:             secret,
		RecoveryCodeHashes: recoveryHashes,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not protect authenticator setup")
		return
	}
	if err := h.state.putTOTPSetup(userID, pendingTOTPSetupState{
		EncryptedVault: encryptedVault,
		Created:        time.Now().UTC(),
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "could not store authenticator setup")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"secret":         secret,
		"otpauth_url":    totpProvisioningURI(user.Username, secret),
		"recovery_codes": recoveryCodes,
	})
}

func (h *authHandler) totpEnable(c *gin.Context) {
	if len(h.totpEncryptionKey) != totpEncryptionKeySize {
		respondError(c, http.StatusServiceUnavailable, "two-factor authentication is not configured")
		return
	}
	userID := c.MustGet("user_id").(uuid.UUID)
	var request totpCodeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	setup, err := h.state.getTOTPSetup(userID)
	if errors.Is(err, badger.ErrKeyNotFound) {
		respondError(c, http.StatusGone, "authenticator setup expired; start setup again")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not load authenticator setup")
		return
	}
	vault, err := decryptTOTPVault(h.totpEncryptionKey, setup.EncryptedVault)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not read authenticator setup")
		return
	}
	step, valid, err := verifyTOTPCode(vault.Secret, strings.TrimSpace(request.Code), time.Now(), -1)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not verify authenticator code")
		return
	}
	if !valid {
		remaining, err := h.state.failTOTPSetup(userID)
		if errors.Is(err, errTOTPSetupAttemptsExceeded) {
			respondError(c, http.StatusGone, "too many incorrect codes; start authenticator setup again")
			return
		}
		if errors.Is(err, badger.ErrKeyNotFound) {
			respondError(c, http.StatusGone, "authenticator setup expired; start setup again")
			return
		}
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not update authenticator setup")
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "authenticator code is incorrect",
			"attempts_remaining": remaining,
		})
		return
	}
	vault.LastAcceptedStep = step
	encryptedVault, err := encryptTOTPVault(h.totpEncryptionKey, vault)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not protect authenticator secret")
		return
	}
	result, err := h.db.NewUpdate().Model((*User)(nil)).
		Set("totp_secret_encrypted = ?", encryptedVault).
		Where("id = ?", userID).
		Where("totp_secret_encrypted IS NULL").
		Exec(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not enable two-factor authentication")
		return
	}
	updated, err := result.RowsAffected()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not confirm two-factor authentication")
		return
	}
	if updated != 1 {
		respondError(c, http.StatusConflict, "two-factor authentication is already enabled")
		return
	}
	if err := h.state.deleteTOTPSetup(userID); err != nil {
		respondError(c, http.StatusInternalServerError, "two-factor authentication enabled but setup cleanup failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true})
}

func (h *authHandler) totpDisable(c *gin.Context) {
	if len(h.totpEncryptionKey) != totpEncryptionKeySize {
		respondError(c, http.StatusServiceUnavailable, "two-factor authentication is not configured")
		return
	}
	userID := c.MustGet("user_id").(uuid.UUID)
	disableAttempts, err := h.state.getTOTPDisableAttempts(userID)
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		respondError(c, http.StatusInternalServerError, "could not check authenticator attempts")
		return
	}
	if disableAttempts >= totpDisableMaxAttempts {
		respondError(c, http.StatusTooManyRequests, "too many incorrect codes; try again later")
		return
	}
	var request totpCodeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	var user User
	if err := h.db.NewSelect().Model(&user).Where("id = ?", userID).Scan(c.Request.Context()); err != nil {
		respondError(c, http.StatusNotFound, "account not found")
		return
	}
	if user.TOTPSecretEncrypted == nil || *user.TOTPSecretEncrypted == "" {
		respondError(c, http.StatusConflict, "two-factor authentication is not enabled")
		return
	}
	vault, err := decryptTOTPVault(h.totpEncryptionKey, *user.TOTPSecretEncrypted)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not read authenticator secret")
		return
	}
	if _, _, valid, err := verifyTOTPFactor(&vault, strings.TrimSpace(request.Code), time.Now()); err != nil {
		respondError(c, http.StatusInternalServerError, "could not verify authenticator code")
		return
	} else if !valid {
		remaining, err := h.state.failTOTPDisable(userID)
		if errors.Is(err, errTOTPDisableAttemptsExceeded) {
			respondError(c, http.StatusTooManyRequests, "too many incorrect codes; try again later")
			return
		}
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not update authenticator attempts")
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":              "authenticator or recovery code is incorrect",
			"attempts_remaining": remaining,
		})
		return
	}
	result, err := h.db.NewUpdate().Model((*User)(nil)).
		Set("totp_secret_encrypted = NULL").
		Where("id = ?", userID).
		Where("totp_secret_encrypted = ?", *user.TOTPSecretEncrypted).
		Exec(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not disable two-factor authentication")
		return
	}
	updated, err := result.RowsAffected()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not confirm two-factor settings")
		return
	}
	if updated != 1 {
		respondError(c, http.StatusConflict, "two-factor settings changed; try again")
		return
	}
	if err := h.state.clearTOTPDisableAttempts(userID); err != nil {
		respondError(c, http.StatusInternalServerError, "two-factor authentication disabled but attempt cleanup failed")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authHandler) loginTOTP(c *gin.Context) {
	if len(h.totpEncryptionKey) != totpEncryptionKeySize {
		respondError(c, http.StatusServiceUnavailable, "two-factor authentication is not configured")
		return
	}
	var request totpLoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	challengeID, err := uuid.Parse(request.ChallengeID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid challenge_id")
		return
	}
	challenge, err := h.state.getTOTPLogin(challengeID)
	if errors.Is(err, badger.ErrKeyNotFound) {
		respondError(c, http.StatusGone, "two-factor challenge expired; sign in again")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not load two-factor challenge")
		return
	}
	var user User
	if err := h.db.NewSelect().Model(&user).
		Where("id = ?", challenge.UserID).
		Where("deleted_at IS NULL").
		Scan(c.Request.Context()); err != nil {
		respondError(c, http.StatusUnauthorized, "invalid two-factor challenge")
		return
	}
	if user.TOTPSecretEncrypted == nil || *user.TOTPSecretEncrypted == "" {
		respondError(c, http.StatusUnauthorized, "invalid two-factor challenge")
		return
	}
	vault, err := decryptTOTPVault(h.totpEncryptionKey, *user.TOTPSecretEncrypted)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not read authenticator secret")
		return
	}
	step, recovery, valid, err := verifyTOTPFactor(&vault, strings.TrimSpace(request.Code), time.Now())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not verify authenticator code")
		return
	}
	if valid {
		if !recovery {
			vault.LastAcceptedStep = step
		}
		encryptedVault, err := encryptTOTPVault(h.totpEncryptionKey, vault)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not protect authenticator secret")
			return
		}
		updated, err := updateTOTPVault(c.Request.Context(), h.db, user, encryptedVault)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not update two-factor state")
			return
		}
		valid = updated
	}
	if !valid {
		remaining, err := h.state.failTOTPLogin(challengeID)
		if errors.Is(err, errTOTPLoginAttemptsExceeded) {
			respondError(c, http.StatusGone, "too many incorrect codes; sign in again")
			return
		}
		if errors.Is(err, badger.ErrKeyNotFound) {
			respondError(c, http.StatusGone, "two-factor challenge expired; sign in again")
			return
		}
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not update two-factor challenge")
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":              "authenticator or recovery code is incorrect",
			"attempts_remaining": remaining,
		})
		return
	}
	if err := h.state.consumeTOTPLogin(challengeID); err != nil {
		respondError(c, http.StatusInternalServerError, "could not complete two-factor challenge")
		return
	}
	rawToken, session, err := newSession(h.state, user.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not create session")
		return
	}
	setSessionCookie(c, rawToken, session.ExpiresAt)
	c.JSON(http.StatusOK, gin.H{"user_id": session.UserID.String()})
}

func verifyTOTPFactor(vault *totpVault, code string, now time.Time) (int64, bool, bool, error) {
	step, valid, err := verifyTOTPCode(vault.Secret, code, now, vault.LastAcceptedStep)
	if err != nil || valid {
		return step, false, valid, err
	}
	if consumeTOTPRecoveryCode(vault, code) {
		return 0, true, true, nil
	}
	return 0, false, false, nil
}

func updateTOTPVault(ctx context.Context, db *bun.DB, user User, encrypted string) (bool, error) {
	result, err := db.NewUpdate().Model((*User)(nil)).
		Set("totp_secret_encrypted = ?", encrypted).
		Where("id = ?", user.ID).
		Where("totp_secret_encrypted = ?", *user.TOTPSecretEncrypted).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("save TOTP state: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check TOTP state update: %w", err)
	}
	return updated == 1, nil
}
