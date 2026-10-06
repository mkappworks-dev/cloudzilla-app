# security.secret_key and an encryption helper

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md#shared)

## What

- Config: `security.secret_key` (`CZ_SECURITY_SECRET_KEY`), empty by default. When it is set, it must be at least 32 bytes, or startup fails with a message naming the key.
- New package `internal/secretbox`:
  - `New(key []byte) (*Box, error)`
  - `(*Box).Seal(purpose string, plaintext []byte) ([]byte, error)`
  - `(*Box).Open(purpose string, ciphertext []byte) ([]byte, error)`
  - `ErrNoKey` and `ErrUndecryptable`
- Crypto:
  - The subkey is `HKDF-SHA256(secret_key, info="cloudzilla/"+purpose)` (stdlib `crypto/hkdf`).
  - The ciphertext is `version(0x01) || nonce(12) || AES-256-GCM(ct||tag)`.
  - `purpose` is also the AAD, so a ciphertext sealed for one purpose fails to open under another.
- The service layer gets the box from `Services`, nil when no key is set.

## Acceptance criteria

- [x] Sealed values round-trip. Opening with a different key, a different purpose, or a ciphertext with a flipped byte returns `ErrUndecryptable`.
- [x] An unknown version byte returns `ErrUndecryptable`.
- [x] A short key fails config validation. An empty key leaves the box nil.
- [x] No plaintext or key material appears in error strings.
- [x] `docs/configuration.md` lists the key and says that losing it makes stored mirror credentials unreadable.
