package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"medium", []byte("the quick brown fox jumps over the lazy dog")},
		{"large", bytes.Repeat([]byte("A"), 10000)},
		{"binary", []byte{0x00, 0xFF, 0x01, 0xFE}},
	}

	key := testKey(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := Encrypt(tt.plaintext, key)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			got, err := Decrypt(ciphertext, key)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			if !bytes.Equal(got, tt.plaintext) {
				t.Errorf("round-trip mismatch: got %q, want %q", got, tt.plaintext)
			}
		})
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("same input every time")

	ct1, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() #1 error = %v", err)
	}
	ct2, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() #2 error = %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of same plaintext produced identical ciphertext (nonce should differ)")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := testKey(t)
	key2 := testKey(t)

	ciphertext, err := Encrypt([]byte("secret"), key1)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	_, err = Decrypt(ciphertext, key2)
	if err == nil {
		t.Error("Decrypt() with wrong key should return error")
	}
}

func TestDecryptTruncatedCiphertext(t *testing.T) {
	key := testKey(t)

	ciphertext, err := Encrypt([]byte("hello"), key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	tests := []struct {
		name       string
		ciphertext []byte
	}{
		{"empty", []byte{}},
		{"one byte", ciphertext[:1]},
		{"nonce only", ciphertext[:12]}, // GCM nonce is 12 bytes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decrypt(tt.ciphertext, key)
			if err == nil {
				t.Error("Decrypt() with truncated ciphertext should return error")
			}
		})
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key := testKey(t)

	ciphertext, err := Encrypt([]byte("important data"), key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Flip a bit in the encrypted payload (after the nonce)
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xFF

	_, err = Decrypt(tampered, key)
	if err == nil {
		t.Error("Decrypt() with tampered ciphertext should return error")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		name    string
		hexKey  string
		wantErr string
	}{
		{
			name:   "valid 32-byte key",
			hexKey: hex.EncodeToString(bytes.Repeat([]byte{0xAB}, 32)),
		},
		{
			name:    "invalid hex characters",
			hexKey:  "zzzz" + strings.Repeat("0", 60),
			wantErr: "decode hex key",
		},
		{
			name:    "too short (16 bytes)",
			hexKey:  hex.EncodeToString(bytes.Repeat([]byte{0xAB}, 16)),
			wantErr: "key must be 32 bytes, got 16",
		},
		{
			name:    "too long (64 bytes)",
			hexKey:  hex.EncodeToString(bytes.Repeat([]byte{0xAB}, 64)),
			wantErr: "key must be 32 bytes, got 64",
		},
		{
			name:    "empty string",
			hexKey:  "",
			wantErr: "key must be 32 bytes, got 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParseKey(tt.hexKey)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseKey() expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParseKey() error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseKey() unexpected error: %v", err)
			}
			if len(key) != 32 {
				t.Errorf("ParseKey() key length = %d, want 32", len(key))
			}
		})
	}
}

func TestParseKeyAndUseForEncryption(t *testing.T) {
	hexKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	key, err := ParseKey(hexKey)
	if err != nil {
		t.Fatalf("ParseKey() error = %v", err)
	}

	plaintext := []byte("integration test")
	ct, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	got, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip with ParseKey: got %q, want %q", got, plaintext)
	}
}
