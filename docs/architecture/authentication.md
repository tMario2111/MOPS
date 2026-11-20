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

Usernames are validated on both the client and server. They must be 4–64
characters and contain only lowercase ASCII letters, digits, and underscores.
An underscore cannot be the first or last character, and consecutive
underscores are not allowed.

## Registration flow

The account is not created until both the email verification and the client-side
OPAQUE registration are complete. The exact sequence is:

1. The client validates the form locally and calls
   `opaque.client.startRegistration(password)`. The password remains in the
   browser.
2. The client sends the first request:

   ```http
   POST /api/auth/register/start
   Content-Type: application/json

   {
     "username": "mario_123",
     "email": "mario@example.com",
     "opaque_request": "<base64url OPAQUE registration request>"
   }
   ```

   The server validates the identity, creates the OPAQUE registration response,
   generates an 8-character uppercase alphanumeric verification code, stores
   only its hash in temporary Badger state, and atomically reserves the
   normalized username and email for the pending registration. The reservations
   have the same 10-minute TTL as the registration state. It prints the mock
   email code in the server terminal and returns:

   ```json
   {
     "registration_id": "<uuid>",
     "opaque_response": "<base64url OPAQUE registration response>"
   }
   ```

3. The client processes `opaque_response` locally with
   `opaque.client.finishRegistration`. It also generates the account identity
   keys and wraps the private keys locally. The resulting OPAQUE record and
   encrypted key material are held in browser route state; no account is
   created yet.
4. The user enters the code and the client sends:

   ```http
   POST /api/auth/register/verify
   Content-Type: application/json

   {
     "registration_id": "<uuid>",
     "code": "A1B2C3D4"
   }
   ```

   On success, the server marks the pending registration as email-verified and
   returns:

   ```json
   { "verified": true }
   ```

   The code expires after 10 minutes. Each invalid code increments the
   attempt count and the response includes the remaining attempt count:

   ```json
   {
     "error": "invalid verification code",
     "attempts_remaining": 2
   }
   ```

   After the third invalid attempt, the pending registration is deleted and
   the server returns `410 Gone`. The client disables further code entry and
   submission, and prompts the user to start registration again for a new
   code. An expired or missing registration also returns `410 Gone`.

   A second registration cannot reserve the same username or email while a
   pending registration is active; the server returns `409 Conflict` and
   identifies whether the username or email is already pending. The reservation
   is released when registration finishes successfully or when the pending
   registration is invalidated. If the user abandons the flow, the reservation
   expires with the 10-minute Badger TTL, after which registration can be
   started again.
5. Only after successful verification does the client send the final request:

   ```http
   POST /api/auth/register/finish
   Content-Type: application/json

   {
     "registration_id": "<uuid>",
     "opaque_record": "<base64url OPAQUE registration record>",
     "public_key": "<base64url public X25519 key>",
     "signing_public_key": "<base64url public Ed25519 key>",
     "wrapped_x25519_private_key": "<encrypted private key>",
     "wrapped_ed25519_private_key": "<encrypted private key>",
     "key_wrap_nonce": "<nonce metadata>",
     "key_wrap_salt": "<Argon2id salt>",
     "key_wrap_params": {}
   }
   ```

   The server checks that the registration exists and is email-verified before
   validating the OPAQUE record, checking username/email uniqueness, and
   inserting the account into PostgreSQL. If verification has not succeeded,
   the server returns `403 Forbidden` and does not create the account.

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

Registration state includes the pending identity, a hash of the email
verification code, the invalid-attempt count, and the verification status.

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
