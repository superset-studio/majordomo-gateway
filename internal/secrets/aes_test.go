package secrets

import (
	"encoding/hex"
	"testing"
)

func testKey() string {
	// 32 bytes = 64 hex chars
	return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}

func TestNewAESStore_ValidHexKey(t *testing.T) {
	store, err := NewAESStore(testKey())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestNewAESStore_EmptyKey(t *testing.T) {
	_, err := NewAESStore("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNewAESStore_InvalidKey(t *testing.T) {
	_, err := NewAESStore("too-short")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	store, err := NewAESStore(testKey())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testCases := []string{
		"sk-proj-1234567890abcdef",
		"",
		"a",
		"a longer API key with special chars: !@#$%^&*()",
	}

	for _, plaintext := range testCases {
		encrypted, err := store.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt failed for %q: %v", plaintext, err)
		}

		decrypted, err := store.Decrypt(encrypted)
		if err != nil {
			t.Fatalf("decrypt failed for %q: %v", plaintext, err)
		}

		if decrypted != plaintext {
			t.Fatalf("roundtrip failed: got %q, want %q", decrypted, plaintext)
		}
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	store, err := NewAESStore(testKey())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	plaintext := "sk-proj-1234567890abcdef"
	enc1, _ := store.Encrypt(plaintext)
	enc2, _ := store.Encrypt(plaintext)

	if enc1 == enc2 {
		t.Fatal("expected different ciphertexts due to random nonce")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	store1, _ := NewAESStore(testKey())

	// Different key
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = 0xff
	}
	store2, _ := NewAESStore(hex.EncodeToString(key2))

	encrypted, err := store1.Encrypt("secret-api-key")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	_, err = store2.Decrypt(encrypted)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	store, _ := NewAESStore(testKey())

	encrypted, err := store.Encrypt("secret-api-key")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	// Tamper with the ciphertext by modifying a character
	tampered := []byte(encrypted)
	if tampered[len(tampered)-2] == 'A' {
		tampered[len(tampered)-2] = 'B'
	} else {
		tampered[len(tampered)-2] = 'A'
	}

	_, err = store.Decrypt(string(tampered))
	if err == nil {
		t.Fatal("expected error when decrypting tampered ciphertext")
	}
}
