package main

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestTOTPCodeMatchesRFC6238SHA1Vector(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))
	code, err := generateTOTPCode(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	if code != "287082" {
		t.Fatalf("TOTP code = %q, want %q", code, "287082")
	}
}

func TestTOTPVerificationAcceptsOnceAndRejectsReplay(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := generateTOTPCode(secret, now)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	step, valid, err := verifyTOTPCode(secret, code, now, -1)
	if err != nil {
		t.Fatalf("verify code: %v", err)
	}
	if !valid {
		t.Fatal("expected current TOTP code to verify")
	}
	if _, valid, err := verifyTOTPCode(secret, code, now, step); err != nil {
		t.Fatalf("check replay: %v", err)
	} else if valid {
		t.Fatal("expected previously accepted TOTP time step to be rejected")
	}
	if _, valid, err := verifyTOTPCode(secret, "abc123", now, -1); err != nil {
		t.Fatalf("verify malformed code: %v", err)
	} else if valid {
		t.Fatal("malformed TOTP code unexpectedly accepted")
	}
}

func TestTOTPVaultEncryptionAndTamperDetection(t *testing.T) {
	key := make([]byte, totpEncryptionKeySize)
	vault := totpVault{
		Secret:             "JBSWY3DPEHPK3PXP",
		RecoveryCodeHashes: []string{"example-hash"},
		LastAcceptedStep:   42,
	}
	encrypted, err := encryptTOTPVault(key, vault)
	if err != nil {
		t.Fatalf("encrypt vault: %v", err)
	}
	if strings.Contains(encrypted, vault.Secret) {
		t.Fatal("encrypted vault contains plaintext secret")
	}
	decrypted, err := decryptTOTPVault(key, encrypted)
	if err != nil {
		t.Fatalf("decrypt vault: %v", err)
	}
	if decrypted.Secret != vault.Secret || decrypted.LastAcceptedStep != vault.LastAcceptedStep {
		t.Fatalf("decrypted vault did not round-trip: %#v", decrypted)
	}

	raw, err := base64.RawURLEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("decode encrypted vault: %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := decryptTOTPVault(key, tampered); err == nil {
		t.Fatal("tampered vault unexpectedly decrypted")
	}
}

func TestTOTPRecoveryCodesAreSingleUse(t *testing.T) {
	codes, hashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	if len(codes) != totpRecoveryCodeCount || len(hashes) != totpRecoveryCodeCount {
		t.Fatalf("generated %d codes and %d hashes", len(codes), len(hashes))
	}
	for _, code := range codes {
		decoded, err := hex.DecodeString(strings.ReplaceAll(code, "-", ""))
		if err != nil || len(decoded) != 16 {
			t.Fatalf("recovery code %q does not have 128 bits", code)
		}
	}

	vault := totpVault{RecoveryCodeHashes: hashes}
	if !consumeTOTPRecoveryCode(&vault, strings.ToLower(codes[0])) {
		t.Fatal("valid recovery code was rejected")
	}
	if consumeTOTPRecoveryCode(&vault, codes[0]) {
		t.Fatal("recovery code was accepted more than once")
	}
	if len(vault.RecoveryCodeHashes) != len(hashes)-1 {
		t.Fatalf("remaining hashes = %d, want %d", len(vault.RecoveryCodeHashes), len(hashes)-1)
	}
}

func TestDecodeTOTPEncryptionKey(t *testing.T) {
	key := make([]byte, totpEncryptionKeySize)
	decoded, err := decodeTOTPEncryptionKey(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("decode valid key: %v", err)
	}
	if len(decoded) != totpEncryptionKeySize {
		t.Fatalf("key length = %d, want %d", len(decoded), totpEncryptionKeySize)
	}
	if _, err := decodeTOTPEncryptionKey(""); err == nil {
		t.Fatal("empty encryption key unexpectedly accepted")
	}
	if _, err := decodeTOTPEncryptionKey(base64.StdEncoding.EncodeToString(key[:16])); err == nil {
		t.Fatal("short encryption key unexpectedly accepted")
	}
}
