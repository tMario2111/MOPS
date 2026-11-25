package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpSecretBytes       = 20
	totpCodeDigits        = 6
	totpPeriodSeconds     = 30
	totpRecoveryCodeCount = 10
	totpEncryptionKeySize = 32
	totpSecretAAD         = "mops/totp-secret/v1"
	totpIssuer            = "Still"
)

type totpVault struct {
	Secret             string   `json:"secret"`
	RecoveryCodeHashes []string `json:"recovery_code_hashes"`
	LastAcceptedStep   int64    `json:"last_accepted_step"`
}

func decodeTOTPEncryptionKey(encoded string) ([]byte, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, errors.New("TOTP_ENCRYPTION_KEY is not set")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode TOTP_ENCRYPTION_KEY: %w", err)
	}
	if len(key) != totpEncryptionKeySize {
		return nil, fmt.Errorf("TOTP_ENCRYPTION_KEY must decode to %d bytes", totpEncryptionKeySize)
	}
	return key, nil
}

func generateTOTPSecret() (string, error) {
	secret := make([]byte, totpSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

func generateTOTPRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 0, totpRecoveryCodeCount)
	hashes := make([]string, 0, totpRecoveryCodeCount)
	for i := 0; i < totpRecoveryCodeCount; i++ {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("generate TOTP recovery code: %w", err)
		}
		encoded := strings.ToUpper(hex.EncodeToString(raw))
		code := strings.Join([]string{encoded[:8], encoded[8:16], encoded[16:24], encoded[24:]}, "-")
		codes = append(codes, code)
		hashes = append(hashes, hashTOTPRecoveryCode(code))
	}
	return codes, hashes, nil
}

func hashTOTPRecoveryCode(code string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func totpProvisioningURI(username, secret string) string {
	label := url.PathEscape(totpIssuer + ":" + username)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", totpIssuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", strconv.Itoa(totpCodeDigits))
	query.Set("period", strconv.Itoa(totpPeriodSeconds))
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func encryptTOTPVault(key []byte, vault totpVault) (string, error) {
	if len(key) != totpEncryptionKeySize {
		return "", errors.New("invalid TOTP encryption key")
	}
	plaintext, err := json.Marshal(vault)
	if err != nil {
		return "", fmt.Errorf("marshal TOTP vault: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create TOTP cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create TOTP cipher: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate TOTP encryption nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(totpSecretAAD))
	return base64.RawURLEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func decryptTOTPVault(key []byte, encoded string) (totpVault, error) {
	if len(key) != totpEncryptionKeySize {
		return totpVault{}, errors.New("invalid TOTP encryption key")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return totpVault{}, fmt.Errorf("decode encrypted TOTP vault: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return totpVault{}, fmt.Errorf("create TOTP cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return totpVault{}, fmt.Errorf("create TOTP cipher: %w", err)
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return totpVault{}, errors.New("encrypted TOTP vault is truncated")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(totpSecretAAD))
	if err != nil {
		return totpVault{}, fmt.Errorf("decrypt TOTP vault: %w", err)
	}
	var vault totpVault
	if err := json.Unmarshal(plaintext, &vault); err != nil {
		return totpVault{}, fmt.Errorf("unmarshal TOTP vault: %w", err)
	}
	return vault, nil
}

func generateTOTPCode(secret string, timestamp time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("decode TOTP secret: %w", err)
	}
	counter := uint64(timestamp.Unix() / totpPeriodSeconds)
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := (uint32(digest[offset])&0x7f)<<24 |
		(uint32(digest[offset+1])&0xff)<<16 |
		(uint32(digest[offset+2])&0xff)<<8 |
		(uint32(digest[offset+3]) & 0xff)
	modulus := uint32(1)
	for i := 0; i < totpCodeDigits; i++ {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", totpCodeDigits, value%modulus), nil
}

func verifyTOTPCode(secret, code string, now time.Time, lastAcceptedStep int64) (int64, bool, error) {
	if len(code) != totpCodeDigits {
		return 0, false, nil
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return 0, false, nil
		}
	}

	currentStep := now.Unix() / totpPeriodSeconds
	for step := currentStep - 1; step <= currentStep+1; step++ {
		if step < 0 || step <= lastAcceptedStep {
			continue
		}
		expected, err := generateTOTPCode(secret, time.Unix(step*totpPeriodSeconds, 0))
		if err != nil {
			return 0, false, err
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return step, true, nil
		}
	}
	return 0, false, nil
}

func consumeTOTPRecoveryCode(vault *totpVault, code string) bool {
	candidate := hashTOTPRecoveryCode(code)
	for index, stored := range vault.RecoveryCodeHashes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(stored)) == 1 {
			vault.RecoveryCodeHashes = append(
				vault.RecoveryCodeHashes[:index],
				vault.RecoveryCodeHashes[index+1:]...,
			)
			return true
		}
	}
	return false
}
