package main

import (
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bytemare/ksf"
	"github.com/bytemare/opaque"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

const (
	maxOpaqueMessage = 16 * 1024
	maxKeyMaterial   = 64 * 1024
)

type registrationStartRequest struct {
	Username      string `json:"username"`
	Email         string `json:"email"`
	OpaqueRequest string `json:"opaque_request"`
}

type registrationStartResponse struct {
	RegistrationID string `json:"registration_id"`
	OpaqueResponse string `json:"opaque_response"`
}

type registrationFinishRequest struct {
	RegistrationID           string         `json:"registration_id"`
	OpaqueRecord             string         `json:"opaque_record"`
	PublicKey                string         `json:"public_key"`
	SigningPublicKey         string         `json:"signing_public_key"`
	WrappedX25519PrivateKey  string         `json:"wrapped_x25519_private_key"`
	WrappedEd25519PrivateKey string         `json:"wrapped_ed25519_private_key"`
	KeyWrapNonce             string         `json:"key_wrap_nonce"`
	KeyWrapSalt              string         `json:"key_wrap_salt"`
	KeyWrapParams            map[string]any `json:"key_wrap_params"`
}

type authHandler struct {
	db           *bun.DB
	opaqueServer *opaque.Server
	state        *badgerStore
}

func newAuthHandler(db *bun.DB, state *badgerStore) (*authHandler, error) {
	config := &opaque.Configuration{
		OPRF: opaque.P256Sha256,
		AKE:  opaque.P256Sha256,
		KSF:  ksf.Scrypt,
		KDF:  crypto.SHA256,
		MAC:  crypto.SHA256,
		Hash: crypto.SHA256,
	}
	server, err := opaque.NewServer(config)
	if err != nil {
		return nil, fmt.Errorf("create OPAQUE server: %w", err)
	}

	materialHex := strings.TrimSpace(os.Getenv("OPAQUE_SERVER_KEY_MATERIAL"))
	if materialHex == "" {
		return nil, errors.New("OPAQUE_SERVER_KEY_MATERIAL is not set")
	}
	material, err := hex.DecodeString(materialHex)
	if err != nil {
		return nil, fmt.Errorf("decode OPAQUE_SERVER_KEY_MATERIAL: %w", err)
	}
	keyMaterial, err := config.DecodeServerKeyMaterial(material)
	if err != nil {
		return nil, fmt.Errorf("decode OPAQUE server key material: %w", err)
	}
	if err := server.SetKeyMaterial(keyMaterial); err != nil {
		return nil, fmt.Errorf("set OPAQUE server key material: %w", err)
	}

	return &authHandler{
		db:           db,
		opaqueServer: server,
		state:        state,
	}, nil
}

func (h *authHandler) registerStart(c *gin.Context) {
	var request registrationStartRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	request.Email = strings.TrimSpace(strings.ToLower(request.Email))
	if err := validateRegistrationIdentity(request.Username, request.Email); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	opaqueRequestBytes, err := decodeMessage(request.OpaqueRequest, maxOpaqueMessage)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid opaque_request")
		return
	}
	opaqueRequest, err := h.opaqueServer.Deserialize.RegistrationRequest(opaqueRequestBytes)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid OPAQUE registration request")
		return
	}

	credentialIdentifier := []byte(request.Username)
	response, err := h.opaqueServer.RegistrationResponse(opaqueRequest, credentialIdentifier, nil)
	if err != nil {
		respondError(c, http.StatusBadRequest, "could not process OPAQUE registration request")
		return
	}

	id := uuid.New()
	if err := h.state.putRegistration(id, pendingRegistrationState{
		Username: request.Username,
		Email:    request.Email,
		Created:  time.Now().UTC(),
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "could not store registration state")
		return
	}
	c.JSON(http.StatusOK, registrationStartResponse{
		RegistrationID: id.String(),
		OpaqueResponse: encodeMessage(response.Serialize()),
	})
}

func (h *authHandler) registerFinish(c *gin.Context) {
	var request registrationFinishRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	registrationID, err := uuid.Parse(request.RegistrationID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid registration_id")
		return
	}
	pending, err := h.state.getRegistration(registrationID)
	if badgerStateError(err) {
		respondError(c, http.StatusBadRequest, "registration is missing or expired")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not load registration state")
		return
	}
	if err := validateKeyMaterial(request); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	recordBytes, err := decodeMessage(request.OpaqueRecord, maxOpaqueMessage)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid opaque_record")
		return
	}
	record, err := h.opaqueServer.Deserialize.RegistrationRecord(recordBytes)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid OPAQUE registration record")
		return
	}

	ctx := c.Request.Context()
	exists, err := h.db.NewSelect().Model((*User)(nil)).
		Where("username = ?", pending.Username).
		Exists(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not check username availability")
		return
	}
	if exists {
		respondError(c, http.StatusConflict, "username is already registered")
		return
	}
	exists, err = h.db.NewSelect().Model((*User)(nil)).
		Where("email = ?", pending.Email).
		Exists(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not check email availability")
		return
	}
	if exists {
		respondError(c, http.StatusConflict, "email is already registered")
		return
	}

	user := &User{
		ID:                       uuid.New(),
		Username:                 pending.Username,
		Email:                    pending.Email,
		PublicKey:                request.PublicKey,
		SigningPublicKey:         request.SigningPublicKey,
		OpaqueRecord:             encodeMessage(record.Serialize()),
		WrappedX25519PrivateKey:  request.WrappedX25519PrivateKey,
		WrappedEd25519PrivateKey: request.WrappedEd25519PrivateKey,
		KeyWrapNonce:             request.KeyWrapNonce,
		KeyWrapSalt:              request.KeyWrapSalt,
		KeyWrapParams:            request.KeyWrapParams,
		CreatedAt:                time.Now().UTC(),
	}
	if _, err := h.db.NewInsert().Model(user).Exec(ctx); err != nil {
		respondError(c, http.StatusInternalServerError, "could not create account")
		return
	}
	if err := h.state.deleteRegistration(registrationID); err != nil {
		respondError(c, http.StatusInternalServerError, "account created but registration state cleanup failed")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": user.ID.String(), "username": user.Username})
}

type loginStartRequest struct {
	Username      string `json:"username"`
	OpaqueRequest string `json:"opaque_request"`
}

type loginStartResponse struct {
	LoginID        string `json:"login_id"`
	OpaqueResponse string `json:"opaque_response"`
}

type loginFinishRequest struct {
	LoginID      string `json:"login_id"`
	OpaqueFinish string `json:"opaque_finish"`
}

func (h *authHandler) loginStart(c *gin.Context) {
	var request loginStartRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" {
		respondError(c, http.StatusBadRequest, "username is required")
		return
	}

	var user User
	if err := h.db.NewSelect().Model(&user).
		Where("username = ?", request.Username).
		Where("deleted_at IS NULL").
		Scan(c.Request.Context()); err != nil {
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	requestBytes, err := decodeMessage(request.OpaqueRequest, maxOpaqueMessage)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid opaque_request")
		return
	}
	ke1, err := h.opaqueServer.Deserialize.KE1(requestBytes)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid OPAQUE login request")
		return
	}
	recordBytes, err := decodeMessage(user.OpaqueRecord, maxOpaqueMessage)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "stored OPAQUE record is invalid")
		return
	}
	record, err := h.opaqueServer.Deserialize.RegistrationRecord(recordBytes)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "stored OPAQUE record is invalid")
		return
	}

	ke2, output, err := h.opaqueServer.GenerateKE2(ke1, &opaque.ClientRecord{
		RegistrationRecord:   record,
		CredentialIdentifier: []byte(user.Username),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "could not process OPAQUE login request")
		return
	}

	id := uuid.New()
	if err := h.state.putLogin(id, pendingLoginState{
		UserID:            user.ID,
		ExpectedClientMAC: encodeMessage(output.ClientMAC),
		Created:           time.Now().UTC(),
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "could not store login state")
		return
	}
	c.JSON(http.StatusOK, loginStartResponse{
		LoginID:        id.String(),
		OpaqueResponse: encodeMessage(ke2.Serialize()),
	})
}

func (h *authHandler) loginFinish(c *gin.Context) {
	var request loginFinishRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	loginID, err := uuid.Parse(request.LoginID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid login_id")
		return
	}
	state, err := h.state.getLogin(loginID)
	if badgerStateError(err) {
		respondError(c, http.StatusUnauthorized, "login is missing or expired")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not load login state")
		return
	}
	finishBytes, err := decodeMessage(request.OpaqueFinish, maxOpaqueMessage)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid opaque_finish")
		return
	}
	ke3, err := h.opaqueServer.Deserialize.KE3(finishBytes)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid OPAQUE login finish")
		return
	}
	expectedMAC, err := decodeMessage(state.ExpectedClientMAC, maxOpaqueMessage)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "stored login state is invalid")
		return
	}
	if err := h.opaqueServer.LoginFinish(ke3, expectedMAC); err != nil {
		_ = h.state.deleteLogin(loginID)
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	rawToken, session, err := newSession(h.state, state.UserID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not create session")
		return
	}
	if err := h.state.deleteLogin(loginID); err != nil {
		_ = h.state.revokeSession(rawToken)
		respondError(c, http.StatusInternalServerError, "could not finalize login")
		return
	}
	setSessionCookie(c, rawToken, session.ExpiresAt)
	c.JSON(http.StatusOK, gin.H{"user_id": session.UserID.String()})
}

func (h *authHandler) logout(c *gin.Context) {
	rawToken, err := c.Cookie(sessionCookieName)
	if err == nil && rawToken != "" {
		if err := h.state.revokeSession(rawToken); err != nil {
			respondError(c, http.StatusInternalServerError, "could not revoke session")
			return
		}
	}
	clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *authHandler) logoutAll(c *gin.Context) {
	userID := c.MustGet("user_id").(uuid.UUID)
	if err := h.state.revokeUserSessions(userID); err != nil {
		respondError(c, http.StatusInternalServerError, "could not revoke sessions")
		return
	}
	clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *authHandler) currentUser(c *gin.Context) {
	userID := c.MustGet("user_id").(uuid.UUID)
	var user User
	if err := h.db.NewSelect().Model(&user).Where("id = ?", userID).Scan(c.Request.Context()); err != nil {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":       user.ID.String(),
		"username": user.Username,
		"email":    user.Email,
	})
}

func validateRegistrationIdentity(username, email string) error {
	if len(username) < 3 || len(username) > 64 {
		return errors.New("username must be between 3 and 64 characters")
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return errors.New("username must not contain whitespace")
	}
	if len(email) < 3 || len(email) > 320 || !strings.Contains(email, "@") {
		return errors.New("email is invalid")
	}
	return nil
}

func validateKeyMaterial(request registrationFinishRequest) error {
	fields := map[string]string{
		"public_key":                  request.PublicKey,
		"signing_public_key":          request.SigningPublicKey,
		"wrapped_x25519_private_key":  request.WrappedX25519PrivateKey,
		"wrapped_ed25519_private_key": request.WrappedEd25519PrivateKey,
		"key_wrap_nonce":              request.KeyWrapNonce,
		"key_wrap_salt":               request.KeyWrapSalt,
	}
	for name, value := range fields {
		if value == "" || len(value) > maxKeyMaterial {
			return fmt.Errorf("%s is missing or too large", name)
		}
	}
	if len(request.KeyWrapParams) == 0 {
		return errors.New("key_wrap_params is required")
	}
	return nil
}

func decodeMessage(value string, maxLength int) ([]byte, error) {
	if value == "" || len(value) > maxLength {
		return nil, errors.New("invalid encoded message length")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 || len(decoded) > maxLength {
		return nil, errors.New("invalid message length")
	}
	return decoded, nil
}

func encodeMessage(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}
