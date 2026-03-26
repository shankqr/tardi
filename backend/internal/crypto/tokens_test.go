package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func validKey() []byte {
	key, _ := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	return key
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := validKey()
	plaintext := []byte("hello, world!")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("roundtrip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key := validKey()
	plaintext := []byte("same input")

	ct1, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("first Encrypt failed: %v", err)
	}

	ct2, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("second Encrypt failed: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key := validKey()
	plaintext := []byte("secret data")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	wrongKey, _ := hex.DecodeString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	_, err = Decrypt(ciphertext, wrongKey)
	if err == nil {
		t.Fatal("expected Decrypt with wrong key to fail, but it succeeded")
	}
}

func TestDecryptTruncatedCiphertext(t *testing.T) {
	key := validKey()
	plaintext := []byte("some data")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Truncate to less than nonce size (12 bytes for AES-GCM)
	truncated := ciphertext[:5]
	_, err = Decrypt(truncated, key)
	if err == nil {
		t.Fatal("expected Decrypt with truncated ciphertext to fail")
	}
}

func TestDecryptCorruptedCiphertext(t *testing.T) {
	key := validKey()
	plaintext := []byte("important data")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Flip a byte in the ciphertext body (after the nonce)
	corrupted := make([]byte, len(ciphertext))
	copy(corrupted, ciphertext)
	corrupted[len(corrupted)-1] ^= 0xff

	_, err = Decrypt(corrupted, key)
	if err == nil {
		t.Fatal("expected Decrypt with corrupted ciphertext to fail")
	}
}

func TestParseKeyValid(t *testing.T) {
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key, err := ParseKey(hexKey)
	if err != nil {
		t.Fatalf("ParseKey failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}

func TestParseKeyInvalidHex(t *testing.T) {
	_, err := ParseKey("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("expected ParseKey with invalid hex to fail")
	}
}

func TestParseKeyWrongLength(t *testing.T) {
	// 16 bytes = 32 hex chars, should fail (need 32 bytes = 64 hex chars)
	_, err := ParseKey("0123456789abcdef0123456789abcdef")
	if err == nil {
		t.Fatal("expected ParseKey with 16-byte key to fail")
	}
}

func TestEncryptDecryptEmptyPlaintext(t *testing.T) {
	key := validKey()
	plaintext := []byte{}

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt with empty plaintext failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt of empty plaintext failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}

func TestEncryptInvalidKeyLength(t *testing.T) {
	shortKey := []byte("tooshort")
	_, err := Encrypt([]byte("data"), shortKey)
	if err == nil {
		t.Fatal("expected Encrypt with invalid key length to fail")
	}
}
