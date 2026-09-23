package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

func qrCipher(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("tamak:qr-storage:v1:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func EncryptQR(value, tableID, secret string) (string, error) {
	aead, err := qrCipher(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), []byte(tableID))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}
func DecryptQR(value, tableID, secret string) (string, error) {
	aead, err := qrCipher(secret)
	if err != nil {
		return "", err
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) < aead.NonceSize()+aead.Overhead() {
		return "", errors.New("invalid encrypted QR")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(tableID))
	return string(plain), err
}
