package fiscal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
)

// EnvSecretsKey names the environment variable holding the AES-256 key that
// wraps the FBR token at rest: base64 of exactly 32 bytes.
const EnvSecretsKey = "FISCAL_SECRETS_KEY"

// devSecretsSeed derives the development-only key. It is a constant in the
// repository on purpose: it must never be reachable in release mode, which is
// what KeyFromEnv enforces.
const devSecretsSeed = "elevon-fiscal-dev-only"

var (
	// ErrSecretsKeyMissing is the release-mode refusal: no FISCAL_SECRETS_KEY.
	ErrSecretsKeyMissing = errors.New("fiscal_secrets_key_missing")
	// ErrSecretsKeyInvalid means the variable is set but is not base64 of 32 bytes.
	ErrSecretsKeyInvalid = errors.New("fiscal_secrets_key_invalid")
	// ErrCiphertextInvalid means the stored token is not something this key wrote.
	ErrCiphertextInvalid = errors.New("fiscal_token_unreadable")
)

// KeyFromEnv resolves the secrets key.
//
// Set: base64 of exactly 32 bytes, or ErrSecretsKeyInvalid. Unset: in release
// mode ErrSecretsKeyMissing — a production store must hold its own key, or a
// database copy would carry a usable token. Outside release it falls back to
// sha256(devSecretsSeed) so a developer can run the fiscal screens without
// provisioning anything.
func KeyFromEnv() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(EnvSecretsKey))
	if raw == "" {
		if os.Getenv("GIN_MODE") == "release" {
			return nil, ErrSecretsKeyMissing
		}
		sum := sha256.Sum256([]byte(devSecretsSeed))
		return sum[:], nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrSecretsKeyInvalid
	}
	if len(key) != 32 {
		return nil, ErrSecretsKeyInvalid
	}
	return key, nil
}

// Encrypt seals plaintext with AES-256-GCM and returns base64(nonce‖ciphertext).
func Encrypt(key []byte, plaintext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A wrong key, a truncated value or a single
// flipped byte all come back as ErrCiphertextInvalid: GCM authenticates, so
// there is no such thing as a partially valid token.
func Decrypt(key []byte, b64 string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", ErrCiphertextInvalid
	}
	if len(sealed) < gcm.NonceSize() {
		return "", ErrCiphertextInvalid
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", ErrCiphertextInvalid
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrSecretsKeyInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrSecretsKeyInvalid
	}
	return cipher.NewGCM(block)
}
