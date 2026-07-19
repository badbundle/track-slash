package githubintegration

import (
	"bytes"
	"errors"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy failed") }

func TestCryptorRoundTripAndBinding(t *testing.T) {
	if _, err := NewCryptor(make([]byte, 31)); err == nil {
		t.Fatal("NewCryptor accepted a short key")
	}
	cryptor, err := NewCryptor(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewCryptor: %v", err)
	}
	ciphertext, nonce, err := cryptor.Encrypt([]byte("secret-token"), []byte("project/repo"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("secret-token")) || len(nonce) != 12 {
		t.Fatalf("ciphertext or nonce is unsafe: %x %x", ciphertext, nonce)
	}
	plaintext, err := cryptor.Decrypt(ciphertext, nonce, []byte("project/repo"))
	if err != nil || string(plaintext) != "secret-token" {
		t.Fatalf("Decrypt = %q, %v", plaintext, err)
	}
	if _, err := cryptor.Decrypt(ciphertext, nonce, []byte("other-repo")); err == nil {
		t.Fatal("Decrypt accepted different associated data")
	}
	if _, err := cryptor.Decrypt(ciphertext, nonce[:4], []byte("project/repo")); err == nil {
		t.Fatal("Decrypt accepted invalid nonce")
	}
	cryptor.rand = failingReader{}
	if _, _, err := cryptor.Encrypt([]byte("x"), nil); err == nil {
		t.Fatal("Encrypt ignored entropy failure")
	}
}
