# Authentication and Session Implementation

This document records the current authentication design and the compatibility
constraints that future changes must preserve.

Related documentation:

- [Requirements](../planning/requirements.md)
- [Key management](../crypto/key_management.md)

## Overview

The application uses OPAQUE for password authentication. Passwords are
processed in the browser and are never sent to the server. The server stores
an OPAQUE registration record and uses it to verify login attempts.

The implementation consists of:

- Browser-side OPAQUE registration and login helpers.
- Go/Gin authentication endpoints.
- PostgreSQL/Bun user storage.
- Badger-backed temporary authentication state.
- Badger-backed authenticated sessions.
- HttpOnly session cookies.
- Client-side encrypted account private keys.

## OPAQUE implementation

The client uses `@serenity-kit/opaque-p256`. The server uses
`github.com/bytemare/opaque`.

The client and server must use compatible OPAQUE implementations and matching
protocol parameters. The client uses the P-256 OPAQUE suite. The Go server is
configured with:

```go
OPRF: opaque.P256Sha256
AKE:  opaque.P256Sha256
KDF:  crypto.SHA256
MAC:  crypto.SHA256
Hash: crypto.SHA256
KSF:  ksf.Argon2id
```

The Argon2id parameters must remain compatible with the client:

```text
time:        3
memory:      65536 KiB
parallelism: 4
```

Changing the OPAQUE implementation, suite, key-stretching function, or server
key material can make existing registrations unusable.

## Credential identifier

OPAQUE registration records are bound to a credential identifier. This
identifier must be identical during registration and every subsequent login.

The current implementation uses the username as the credential identifier:

```go
// Registration
credentialIdentifier := []byte(request.Username)

// Login
CredentialIdentifier: []byte(user.Username)
```

The email remains a separate database field and may be used for contact
information, verification, password recovery, or notifications.

The login lookup field and OPAQUE credential identifier are separate concepts.
If login is changed to email lookup, the server must still use the original
username as the OPAQUE credential identifier for accounts registered with
usernames.

Changing the credential identifier after registration requires users to
register new OPAQUE credentials.

## Registration flow

1. The client calls `opaque.client.startRegistration`.
2. The client sends the registration request to
   `POST /api/auth/register/start`.
3. The server validates the username and email, creates the OPAQUE
   registration response, and stores temporary registration state in Badger.
4. The client finishes OPAQUE registration.
5. The client generates X25519 and Ed25519 identity keys.
6. The client derives a wrapping key with Argon2id and wraps both private keys
   with XChaCha20-Poly1305.
7. The client sends the OPAQUE record and encrypted key material to
   `POST /api/auth/register/finish`.
8. The server stores the user in PostgreSQL.

The server stores encrypted private-key material only. It does not receive the
password or plaintext private keys.

## Login flow

1. The client calls `opaque.client.startLogin`.
2. The client sends the login request to `POST /api/auth/login/start`.
3. The server looks up the user, loads the stored OPAQUE record, uses the
   username as the credential identifier, creates the OPAQUE login response,
   and stores temporary login state in Badger.
4. The client calls `opaque.client.finishLogin`.
5. The client sends the finish request to `POST /api/auth/login/finish`.
6. The server verifies the client MAC.
7. The server creates an authenticated session and sets the `mops_session`
   HttpOnly cookie.

## Badger temporary state

Badger stores short-lived protocol state:

| State | Prefix | TTL |
|---|---|---:|
| Registration state | `registration:` | 10 minutes |
| Login state | `login:` | 10 minutes |

Registration and login state is deleted after successful completion.

## Sessions

Authenticated sessions are stored in Badger. Session properties are:

- 32 cryptographically random bytes.
- The raw token is sent only through the cookie.
- SHA-256 of the raw token is used as the Badger lookup key.
- HttpOnly cookie named `mops_session`.
- `SameSite=Lax`.
- The `Secure` flag is controlled by `COOKIE_SECURE`.
- Absolute lifetime of 30 days.
- Idle lifetime of 24 hours.

The server never stores the raw session token as a database key.

The session middleware reads the cookie, hashes the raw token, checks the
session's absolute and idle expiration, refreshes the idle timestamp, and
places the authenticated user ID in the Gin context.

Available session endpoints:

```text
GET  /api/auth/me
POST /api/auth/logout
POST /api/auth/logout-all
```

## OPAQUE server key material

`OPAQUE_SERVER_KEY_MATERIAL` contains persistent server-side OPAQUE key
material. It must be generated securely, stored outside source control, and
remain unchanged while existing accounts are in use.

Changing this value invalidates existing OPAQUE credentials because the server
identity and OPRF material change.

## Client private-key storage

During registration, the client generates:

- An X25519 private key for key agreement.
- An Ed25519 private key for signing.

Private keys are wrapped as follows:

```text
Argon2id(password, random salt) -> 32-byte wrapping key
XChaCha20-Poly1305 -> encrypted private key
```

Separate nonces are used for the X25519 and Ed25519 ciphertexts. The nonce
metadata is stored in the `key_wrap_nonce` field.

The server stores public identity keys, wrapped private keys, the key-wrap
salt, key-wrap parameters, and nonce metadata. It cannot decrypt the private
keys without the user's password.

## Compatibility rules

Do not change any of the following without a migration plan:

- OPAQUE client library.
- OPAQUE server library.
- OPAQUE suite.
- Hash, MAC, or KDF configuration.
- Key-stretching function.
- OPAQUE server key material.
- Credential identifier format.
- Username normalization rules.

For prototype accounts, the simplest recovery after an incompatible protocol
change is:

1. Keep the existing `OPAQUE_SERVER_KEY_MATERIAL`.
2. Delete the affected test user.
3. Register the user again.
4. Test login with the same username and password.
