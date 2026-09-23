package security

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	hash := Password("A sufficiently long password")
	if !VerifyPassword(hash, "A sufficiently long password") {
		t.Fatal("password mismatch")
	}
	if VerifyPassword(hash, "incorrect") {
		t.Fatal("accepted invalid password")
	}
	if VerifyPassword(strings.Replace(hash, "m=65536", "m=999999999", 1), "x") {
		t.Fatal("accepted unsafe parameters")
	}
}
func TestTableSignature(t *testing.T) {
	token := SignTable("0123456789abcdef01234567", "secret")
	id, e := VerifyTable(token, "secret")
	if e != nil || id != "0123456789abcdef01234567" {
		t.Fatal("invalid signature")
	}
	if _, e = VerifyTable(token, "other"); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = VerifyTable("f"+token[1:], "secret"); e == nil {
		t.Fatal("modified token accepted")
	}
}
func TestAccessClaims(t *testing.T) {
	token, e := Access("123", 4, "test secret")
	if e != nil {
		t.Fatal(e)
	}
	cl, e := Parse(token, "test secret")
	if e != nil || cl.Subject != "123" || cl.Version != 4 {
		t.Fatal("incorrect claims")
	}
	if _, e = Parse(token, "wrong secret"); e == nil {
		t.Fatal("wrong secret accepted")
	}
}
