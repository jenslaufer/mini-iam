package tenant

import (
	"testing"
)

func TestDeriveKeyDeterministic(t *testing.T) {
	// Same passphrase produces same key
	k1 := DeriveKey("test-passphrase")
	k2 := DeriveKey("test-passphrase")
	if k1 != k2 {
		t.Fatal("same passphrase should produce same key")
	}
}

func TestDeriveKeyDifferentPassphrases(t *testing.T) {
	k1 := DeriveKey("passphrase-a")
	k2 := DeriveKey("passphrase-b")
	if k1 == k2 {
		t.Fatal("different passphrases should produce different keys")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := DeriveKey("test-key")
	original := "my-smtp-password"

	encrypted, err := Encrypt(original, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if encrypted == original {
		t.Fatal("encrypted value should differ from original")
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if decrypted != original {
		t.Fatalf("got %q, want %q", decrypted, original)
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key := DeriveKey("test-key")
	e1, _ := Encrypt("same-password", key)
	e2, _ := Encrypt("same-password", key)
	if e1 == e2 {
		t.Fatal("each encryption should produce different ciphertext (random nonce)")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := DeriveKey("key-1")
	key2 := DeriveKey("key-2")

	encrypted, _ := Encrypt("secret", key1)
	_, err := Decrypt(encrypted, key2)
	if err == nil {
		t.Fatal("decrypting with wrong key should fail")
	}
}

func TestEncryptEmptyString(t *testing.T) {
	key := DeriveKey("test-key")
	encrypted, err := Encrypt("", key)
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if decrypted != "" {
		t.Fatalf("got %q, want empty string", decrypted)
	}
}

func TestIsEncrypted(t *testing.T) {
	key := DeriveKey("test-key")
	encrypted, _ := Encrypt("password", key)

	if !IsEncrypted(encrypted) {
		t.Fatal("encrypted value should be detected as encrypted")
	}
	if IsEncrypted("plaintext-password") {
		t.Fatal("plaintext should not be detected as encrypted")
	}
	if IsEncrypted("") {
		t.Fatal("empty string should not be detected as encrypted")
	}
}
