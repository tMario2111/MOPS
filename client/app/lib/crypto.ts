import sodium from 'libsodium-wrappers-sumo';
import {
  getOpaqueConfig,
  OpaqueClient,
  OpaqueID,
  KE1,
  KE2,
  RegistrationRequest,
  RegistrationResponse,
} from '@cloudflare/opaque-ts';

const NONCE_BYTES = 24;
const PASSWORD_KEY_BYTES = 32;
const PASSWORD_SALT_BYTES = 16;
const BASE64_VARIANT = 7;

const X25519_AAD = 'mops/account/x25519-private-key/v1';
const ED25519_AAD = 'mops/account/ed25519-private-key/v1';
const OPAQUE_CONFIG = getOpaqueConfig(OpaqueID.OPAQUE_P256);

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

export interface OpaqueRegistrationStart {
  request: string;
  session: OpaqueRegistrationSession;
}

export interface OpaqueRegistrationResult {
  record: string;
  exportKey: Uint8Array;
}

export interface OpaqueLoginStart {
  request: string;
  session: OpaqueLoginSession;
}

export interface OpaqueLoginResult {
  finish: string;
  sessionKey: Uint8Array;
  exportKey: Uint8Array;
}

export class OpaqueProtocolError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = 'OpaqueProtocolError';
  }
}

export class OpaqueRegistrationSession {
  readonly client: OpaqueClient;

  constructor(client: OpaqueClient) {
    this.client = client;
  }
}

export class OpaqueLoginSession {
  readonly client: OpaqueClient;

  constructor(client: OpaqueClient) {
    this.client = client;
  }
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

function encodeOpaqueMessage(message: { serialize(): number[] }): string {
  return encode(new Uint8Array(message.serialize()));
}

function decodeOpaqueMessage<T>(
  value: string,
  fieldName: string,
  deserialize: (config: typeof OPAQUE_CONFIG, bytes: number[]) => T
): T {
  if (typeof value !== 'string' || value.length === 0) {
    throw new OpaqueProtocolError(`${fieldName} is missing from the server response.`);
  }

  const bytes = decode(value, fieldName);
  try {
    return deserialize(OPAQUE_CONFIG, Array.from(bytes));
  } catch (error) {
    throw new OpaqueProtocolError(`${fieldName} is not a valid OPAQUE message.`, {
      cause: error,
    });
  } finally {
    sodium.memzero(bytes);
  }
}

function throwOpaqueError(operation: string, result: unknown): never {
  if (result instanceof Error) {
    throw new OpaqueProtocolError(`${operation} failed: ${result.message}`, { cause: result });
  }
  throw new OpaqueProtocolError(`${operation} returned an invalid result.`);
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
  if (salt.length !== PASSWORD_SALT_BYTES) {
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

/**
 * Starts OPAQUE registration. Send `request` to the server and retain
 * `session` until `finishOpaqueRegistration` is called with the response.
 */
export async function startOpaqueRegistration(password: string): Promise<OpaqueRegistrationStart> {
  await sodium.ready;
  requireNonEmptyPassword(password);

  const client = new OpaqueClient(OPAQUE_CONFIG);
  const request = await client.registerInit(password);
  if (!(request instanceof RegistrationRequest)) {
    throwOpaqueError('OPAQUE registration initialization', request);
  }

  return {
    request: encodeOpaqueMessage(request),
    session: new OpaqueRegistrationSession(client),
  };
}

/**
 * Completes OPAQUE registration and returns the opaque record to store on the
 * server. The record contains verifier material, not the user's password.
 */
export async function finishOpaqueRegistration(
  session: OpaqueRegistrationSession,
  response: string,
  serverIdentity?: string,
  clientIdentity?: string
): Promise<OpaqueRegistrationResult> {
  await sodium.ready;
  const registrationResponse = decodeOpaqueMessage(
    response,
    'OPAQUE registration response',
    (config, bytes) => RegistrationResponse.deserialize(config, bytes)
  );
  const result = await session.client.registerFinish(
    registrationResponse,
    serverIdentity,
    clientIdentity
  );
  if (result instanceof Error) {
    throwOpaqueError('OPAQUE registration finalization', result);
  }

  return {
    record: encodeOpaqueMessage(result.record),
    exportKey: new Uint8Array(result.export_key),
  };
}

/**
 * Starts OPAQUE login. Send `request` to the server and retain `session`
 * until `finishOpaqueLogin` is called with the server response.
 */
export async function startOpaqueLogin(password: string): Promise<OpaqueLoginStart> {
  await sodium.ready;
  requireNonEmptyPassword(password);

  const client = new OpaqueClient(OPAQUE_CONFIG);
  const request = await client.authInit(password);
  if (!(request instanceof KE1)) {
    throwOpaqueError('OPAQUE login initialization', request);
  }

  return {
    request: encodeOpaqueMessage(request),
    session: new OpaqueLoginSession(client),
  };
}

/**
 * Completes OPAQUE login. The returned `finish` message must be sent to the
 * server before accepting the authenticated session.
 */
export async function finishOpaqueLogin(
  session: OpaqueLoginSession,
  response: string,
  serverIdentity?: string,
  clientIdentity?: string,
  context?: string
): Promise<OpaqueLoginResult> {
  await sodium.ready;
  const keyExchangeResponse = decodeOpaqueMessage(
    response,
    'OPAQUE login response',
    (config, bytes) => KE2.deserialize(config, bytes)
  );
  const result = await session.client.authFinish(
    keyExchangeResponse,
    serverIdentity,
    clientIdentity,
    context
  );
  if (result instanceof Error) {
    throwOpaqueError('OPAQUE login finalization', result);
  }

  return {
    finish: encodeOpaqueMessage(result.ke3),
    sessionKey: new Uint8Array(result.session_key),
    exportKey: new Uint8Array(result.export_key),
  };
}

export function clearOpaqueSecrets(secrets: {
  sessionKey?: Uint8Array;
  exportKey?: Uint8Array;
}): void {
  if (secrets.sessionKey !== undefined) {
    sodium.memzero(secrets.sessionKey);
  }
  if (secrets.exportKey !== undefined) {
    sodium.memzero(secrets.exportKey);
  }
}
