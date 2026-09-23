package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
	"strings"
	"time"
)

func Random() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func Password(s string) string {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		panic(e)
	}
	key := argon2.IDKey([]byte(s), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}
func VerifyPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	key, e := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || len(key) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(key, actual) == 1
}

type Claims struct {
	Version int `json:"ver"`
	jwt.RegisteredClaims
}

func Access(id string, version int, secret string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{version, jwt.RegisteredClaims{Subject: id, Issuer: "tamak", Audience: jwt.ClaimStrings{"tamak-staff"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute))}}).SignedString([]byte(secret))
}
func Parse(token, secret string) (*Claims, error) {
	c := &Claims{}
	_, e := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("tamak"), jwt.WithAudience("tamak-staff"), jwt.WithExpirationRequired())
	return c, e
}
func SignTable(id, secret string) string {
	payload := id + "." + Random()
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func VerifyTable(token, secret string) (string, error) {
	p := strings.Split(token, ".")
	if len(p) != 3 {
		return "", errors.New("invalid QR")
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(p[0] + "." + p[1]))
	sig, e := base64.RawURLEncoding.DecodeString(p[2])
	if e != nil || !hmac.Equal(sig, m.Sum(nil)) {
		return "", errors.New("invalid QR")
	}
	return p[0], nil
}
