package githubintegration

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type Cryptor struct {
	aead cipher.AEAD
	rand io.Reader
}

func NewCryptor(key []byte) (*Cryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("GitHub encryption key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cryptor{aead: aead, rand: rand.Reader}, nil
}

func (c *Cryptor) Encrypt(plaintext, associatedData []byte) ([]byte, []byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return nil, nil, err
	}
	return c.aead.Seal(nil, nonce, plaintext, associatedData), nonce, nil
}

func (c *Cryptor) Decrypt(ciphertext, nonce, associatedData []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, errors.New("invalid GitHub credential nonce")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, errors.New("GitHub credential cannot be decrypted")
	}
	return plaintext, nil
}
