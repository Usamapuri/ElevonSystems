package fiscal

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := testKey(7)
	const token = "eyJhbGciOiJIUzI1NiJ9.sandbox-token.0123456789"
	sealed, err := Encrypt(key, token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, token) {
		t.Fatal("the ciphertext must not contain the plaintext")
	}
	if _, err := base64.StdEncoding.DecodeString(sealed); err != nil {
		t.Fatalf("Encrypt must return base64: %v", err)
	}
	back, err := Decrypt(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if back != token {
		t.Fatalf("round trip: %q", back)
	}

	// The nonce is fresh every time, so the same token seals differently.
	again, err := Encrypt(key, token)
	if err != nil {
		t.Fatal(err)
	}
	if again == sealed {
		t.Fatal("two seals of the same token must differ")
	}
}

func TestDecrypt_RejectsTamperWrongKeyAndJunk(t *testing.T) {
	key := testKey(7)
	sealed, err := Encrypt(key, "token")
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := base64.StdEncoding.DecodeString(sealed)
	raw[len(raw)-1] ^= 0xFF
	if _, err := Decrypt(key, base64.StdEncoding.EncodeToString(raw)); err != ErrCiphertextInvalid {
		t.Fatalf("tampered ciphertext: %v", err)
	}

	if _, err := Decrypt(testKey(9), sealed); err != ErrCiphertextInvalid {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := Decrypt(key, "not base64!!"); err != ErrCiphertextInvalid {
		t.Fatalf("junk: %v", err)
	}
	if _, err := Decrypt(key, base64.StdEncoding.EncodeToString([]byte("short"))); err != ErrCiphertextInvalid {
		t.Fatalf("shorter than a nonce: %v", err)
	}
}

func TestEncrypt_RejectsShortKey(t *testing.T) {
	if _, err := Encrypt(make([]byte, 31), "token"); err != ErrSecretsKeyInvalid {
		t.Fatalf("31-byte key: %v", err)
	}
	if _, err := Decrypt(make([]byte, 31), "AAAA"); err != ErrSecretsKeyInvalid {
		t.Fatalf("31-byte key on decrypt: %v", err)
	}
}

func TestKeyFromEnv(t *testing.T) {
	// Unset outside release: the fixed development key, never a random one.
	t.Setenv("GIN_MODE", "debug")
	t.Setenv(EnvSecretsKey, "")
	key, err := KeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(devSecretsSeed))
	if string(key) != string(want[:]) {
		t.Fatal("the dev fallback must be sha256 of the fixed seed")
	}

	// Unset in release: refuse. A live store holds its own key.
	t.Setenv("GIN_MODE", "release")
	if _, err := KeyFromEnv(); err != ErrSecretsKeyMissing {
		t.Fatalf("release without a key: %v", err)
	}

	// Set and well formed: used in release too.
	t.Setenv(EnvSecretsKey, base64.StdEncoding.EncodeToString(testKey(3)))
	key, err = KeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if string(key) != string(testKey(3)) {
		t.Fatal("the environment key must be used verbatim")
	}

	for _, bad := range []string{
		base64.StdEncoding.EncodeToString(make([]byte, 31)),
		base64.StdEncoding.EncodeToString(make([]byte, 33)),
		"not base64!!",
	} {
		t.Setenv(EnvSecretsKey, bad)
		if _, err := KeyFromEnv(); err != ErrSecretsKeyInvalid {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

func TestMaskAPIKey(t *testing.T) {
	cases := map[string]string{"": "", "abc": "****", "abcd": "****", "abcde": "****bcde", "token-1234": "****1234"}
	for in, want := range cases {
		if got := MaskAPIKey(in); got != want {
			t.Errorf("MaskAPIKey(%q) = %q, want %q", in, got, want)
		}
	}
}
