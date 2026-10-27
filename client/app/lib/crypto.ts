import sodium from 'libsodium-wrappers-sumo';

const NONCE_BYTES = sodium.crypto_aead_xchacha20poly1305_ietf_NPUBBYTES;
const PASSWORD_KEY_BYTES = sodium.crypto_aead_xchacha20poly1305_ietf_KEYBYTES;
const BASE64_VARIANT = sodium.base64_variants.URLSAFE_NO_PADDING;

const X25519_AAD = 'mops/account/x25519-private-key/v1';
const ED25519_AAD = 'mops/account/ed25519-private-key/v1';

export interface KeyWrapParams {
  algorithm: 'argon2id13';
  opsLimit: number;
  memLimit: number;
  keyLength: number;
  version: 1;
}

export interface WrappedAccountKeys {
  publicX25519Key: string;
  publicEd25519Key: string;
  wrappedX25519PrivateKey: string;
  wrappedEd25519PrivateKey: string;
  keyWrapNonce: string;
  keyWrapSalt: string;
  keyWrapParams: KeyWrapParams;
}

export interface AccountPrivateKeys {
  x25519PrivateKey: Uint8Array;
  ed25519PrivateKey: Uint8Array;
}

interface KeyWrapNonces {
  x25519: string;
  ed25519: string;
}

export class InvalidKeyMaterialError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'InvalidKeyMaterialError';
  }
}

function requireNonEmptyPassword(password: string): void {
  if (password.length === 0) {
    throw new TypeError('Password must not be empty.');
  }
}

function encode(bytes: Uint8Array): string {
  return sodium.to_base64(bytes, BASE64_VARIANT);
}

function decode(value: string, fieldName: string): Uint8Array {
  try {
    const bytes = sodium.from_base64(value, BASE64_VARIANT);
    if (bytes.length === 0) {
      throw new Error('empty value');
    }
    return bytes;
  } catch {
    throw new InvalidKeyMaterialError(`${fieldName} is not valid base64.`);
  }
}

function parseNonces(value: string): { x25519: Uint8Array; ed25519: Uint8Array } {
  let encoded: KeyWrapNonces;
  try {
    encoded = JSON.parse(value) as KeyWrapNonces;
  } catch {
    throw new InvalidKeyMaterialError('keyWrapNonce is not valid JSON.');
  }

  if (
    typeof encoded !== 'object' ||
    encoded === null ||
    typeof encoded.x25519 !== 'string' ||
    typeof encoded.ed25519 !== 'string'
  ) {
    throw new InvalidKeyMaterialError('keyWrapNonce has an invalid shape.');
  }

  const x25519 = decode(encoded.x25519, 'x25519 nonce');
  const ed25519 = decode(encoded.ed25519, 'ed25519 nonce');
  if (x25519.length !== NONCE_BYTES || ed25519.length !== NONCE_BYTES) {
    throw new InvalidKeyMaterialError('keyWrapNonce contains an invalid nonce length.');
  }

  return { x25519, ed25519 };
}

function validateParams(params: KeyWrapParams): void {
  if (
    params?.algorithm !== 'argon2id13' ||
    params.version !== 1 ||
    params.keyLength !== PASSWORD_KEY_BYTES ||
    !Number.isSafeInteger(params.opsLimit) ||
    !Number.isSafeInteger(params.memLimit) ||
    params.opsLimit <= 0 ||
    params.memLimit <= 0
  ) {
    throw new InvalidKeyMaterialError('Unsupported or invalid key-wrap parameters.');
  }
}

async function deriveWrappingKey(
  password: string,
  salt: Uint8Array,
  params: KeyWrapParams
): Promise<Uint8Array> {
  await sodium.ready;
  requireNonEmptyPassword(password);
  validateParams(params);
  return sodium.crypto_pwhash(
    params.keyLength,
    password,
    salt,
    params.opsLimit,
    params.memLimit,
    sodium.crypto_pwhash_ALG_ARGON2ID13
  );
}

function encryptPrivateKey(
  privateKey: Uint8Array,
  wrappingKey: Uint8Array,
  nonce: Uint8Array,
  associatedData: string
): string {
  return encode(
    sodium.crypto_aead_xchacha20poly1305_ietf_encrypt(
      privateKey,
      associatedData,
      null,
      nonce,
      wrappingKey
    )
  );
}

function decryptPrivateKey(
  ciphertext: Uint8Array,
  wrappingKey: Uint8Array,
  nonce: Uint8Array,
  associatedData: string
): Uint8Array {
  return sodium.crypto_aead_xchacha20poly1305_ietf_decrypt(
    null,
    ciphertext,
    associatedData,
    nonce,
    wrappingKey
  );
}

/**
 * Generates account identity keys and wraps both private keys for registration.
 *
 * The returned values are safe to send to the server. Private keys and the
 * password-derived wrapping key are cleared from temporary buffers before this
 * function returns.
 */
export async function generateAccountKeys(password: string): Promise<WrappedAccountKeys> {
  await sodium.ready;
  requireNonEmptyPassword(password);

  const salt = sodium.randombytes_buf(sodium.crypto_pwhash_SALTBYTES);
  const x25519Nonce = sodium.randombytes_buf(NONCE_BYTES);
  const ed25519Nonce = sodium.randombytes_buf(NONCE_BYTES);
  const x25519KeyPair = sodium.crypto_box_keypair();
  const ed25519KeyPair = sodium.crypto_sign_keypair();
  const keyWrapParams: KeyWrapParams = {
    algorithm: 'argon2id13',
    opsLimit: sodium.crypto_pwhash_OPSLIMIT_MODERATE,
    memLimit: sodium.crypto_pwhash_MEMLIMIT_MODERATE,
    keyLength: PASSWORD_KEY_BYTES,
    version: 1,
  };
  let wrappingKey: Uint8Array | undefined;

  try {
    wrappingKey = await deriveWrappingKey(password, salt, keyWrapParams);
    return {
      publicX25519Key: encode(x25519KeyPair.publicKey),
      publicEd25519Key: encode(ed25519KeyPair.publicKey),
      wrappedX25519PrivateKey: encryptPrivateKey(
        x25519KeyPair.privateKey,
        wrappingKey,
        x25519Nonce,
        X25519_AAD
      ),
      wrappedEd25519PrivateKey: encryptPrivateKey(
        ed25519KeyPair.privateKey,
        wrappingKey,
        ed25519Nonce,
        ED25519_AAD
      ),
      keyWrapNonce: JSON.stringify({
        x25519: encode(x25519Nonce),
        ed25519: encode(ed25519Nonce),
      }),
      keyWrapSalt: encode(salt),
      keyWrapParams,
    };
  } finally {
    sodium.memzero(salt);
    sodium.memzero(x25519Nonce);
    sodium.memzero(ed25519Nonce);
    sodium.memzero(x25519KeyPair.privateKey);
    sodium.memzero(ed25519KeyPair.publicKey);
    sodium.memzero(ed25519KeyPair.privateKey);
    sodium.memzero(ed25519KeyPair.publicKey);
    if (wrappingKey !== undefined) {
      sodium.memzero(wrappingKey);
    }
  }
}

/**
 * Decrypts private keys downloaded from the server after account login.
 *
 * Callers own the returned buffers and should call `clearPrivateKeys` when
 * the active session no longer needs them.
 */
export async function decryptAccountKeys(
  password: string,
  wrappedKeys: WrappedAccountKeys
): Promise<AccountPrivateKeys> {
  await sodium.ready;
  requireNonEmptyPassword(password);
  validateParams(wrappedKeys.keyWrapParams);

  const salt = decode(wrappedKeys.keyWrapSalt, 'keyWrapSalt');
  if (salt.length !== sodium.crypto_pwhash_SALTBYTES) {
    throw new InvalidKeyMaterialError('keyWrapSalt has an invalid length.');
  }
  const nonces = parseNonces(wrappedKeys.keyWrapNonce);
  const wrappedX25519 = decode(wrappedKeys.wrappedX25519PrivateKey, 'wrappedX25519PrivateKey');
  const wrappedEd25519 = decode(wrappedKeys.wrappedEd25519PrivateKey, 'wrappedEd25519PrivateKey');
  let wrappingKey: Uint8Array | undefined;
  let x25519PrivateKey: Uint8Array | undefined;
  let ed25519PrivateKey: Uint8Array | undefined;
  let keysReturned = false;

  try {
    wrappingKey = await deriveWrappingKey(password, salt, wrappedKeys.keyWrapParams);
    x25519PrivateKey = decryptPrivateKey(wrappedX25519, wrappingKey, nonces.x25519, X25519_AAD);
    ed25519PrivateKey = decryptPrivateKey(wrappedEd25519, wrappingKey, nonces.ed25519, ED25519_AAD);

    if (
      x25519PrivateKey.length !== sodium.crypto_box_SECRETKEYBYTES ||
      ed25519PrivateKey.length !== sodium.crypto_sign_SECRETKEYBYTES
    ) {
      throw new InvalidKeyMaterialError('Decrypted private key has an invalid length.');
    }

    keysReturned = true;
    return { x25519PrivateKey, ed25519PrivateKey };
  } finally {
    sodium.memzero(salt);
    sodium.memzero(nonces.x25519);
    sodium.memzero(nonces.ed25519);
    if (!keysReturned) {
      if (x25519PrivateKey !== undefined) {
        sodium.memzero(x25519PrivateKey);
      }
      if (ed25519PrivateKey !== undefined) {
        sodium.memzero(ed25519PrivateKey);
      }
    }
    if (wrappingKey !== undefined) {
      sodium.memzero(wrappingKey);
    }
  }
}

export function clearPrivateKeys(privateKeys: AccountPrivateKeys): void {
  sodium.memzero(privateKeys.x25519PrivateKey);
  sodium.memzero(privateKeys.ed25519PrivateKey);
}
