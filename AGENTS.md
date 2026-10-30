# Repository guidance

## Project structure

- `client/` contains the React Router and TypeScript client.
- `server/` contains the Go/Gin API, PostgreSQL/Bun models, and Badger state store.
- `docs/` contains product requirements, cryptographic design, and implementation notes.

## Read before changing authentication or cryptography

Consult these documents before modifying related code:

- `docs/planning/requirements.md`
- `docs/crypto/key_management.md`
- `docs/architecture/authentication.md`

Update the relevant documentation when an architectural or protocol decision changes.

## Authentication invariants

- Passwords must never be sent to the server.
- OPAQUE client and server implementations must remain protocol-compatible.
- The client uses `@serenity-kit/opaque-p256`.
- The server uses `github.com/bytemare/opaque`.
- The OPAQUE suite is P-256 with SHA-256 KDF, MAC, and hash functions.
- The server uses Argon2id with the client-compatible parameters.
- The current OPAQUE credential identifier is the username.
- The credential identifier must be identical during registration and login.
- Do not change `OPAQUE_SERVER_KEY_MATERIAL` casually; changing it invalidates existing credentials.
- Private keys are generated and wrapped on the client.
- The server stores only encrypted private-key material.
- Session tokens are random, sent through an HttpOnly cookie, and stored in Badger by SHA-256 hash.

## Code-change guidance

- Inspect existing patterns before adding new helpers or endpoints.
- Keep changes focused and preserve existing behavior unless the task explicitly requires a change.
- Do not log passwords, private keys, OPAQUE secrets, raw session tokens, or environment secrets.
- Do not commit `.env` files, generated secrets, Badger data, or other credentials.
- Use `gofmt` for Go changes and the repository's existing formatting tools for client changes.
- Add or update tests when behavior changes.

## Validation

For client changes:

```powershell
cd client
npm run lint
npm run typecheck
```

For server changes:

```powershell
cd server
gofmt -w .
go test ./...
```

For documentation-only changes, no build is normally required. Run `git diff --check`
to catch whitespace errors.
