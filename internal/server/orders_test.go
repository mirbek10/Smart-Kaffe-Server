package server

import (
	"encoding/json"
	"koffe/api/internal/config"
	"testing"
)

func TestOrderValidation(t *testing.T) {
	var in orderInput
	_ = json.Unmarshal([]byte(`{"tableToken":"test","items":[{"productId":"0123456789abcdef01234567","quantity":1}]}`), &in)
	if e := validateOrder(in, "da3c56a7-01ed-49c6-83e6-f1d111111111"); e != nil {
		t.Fatal(e)
	}
	if validateOrder(in, "") == nil {
		t.Fatal("missing key accepted")
	}
	in.Items[0].Quantity = 21
	if validateOrder(in, "da3c56a7-01ed-49c6-83e6-f1d111111111") == nil {
		t.Fatal("invalid quantity accepted")
	}
	in.Items[0].Quantity = 1
	in.Items = append(in.Items, in.Items[0])
	if validateOrder(in, "da3c56a7-01ed-49c6-83e6-f1d111111111") == nil {
		t.Fatal("duplicate item accepted")
	}
}
func TestImageValidation(t *testing.T) {
	for _, v := range []string{"javascript:alert(1)", "//example.com/p.png", "https://u:p@example.com/x", "/\\evil"} {
		if validImage(v) {
			t.Errorf("unsafe URL accepted: %q", v)
		}
	}
	for _, v := range []string{"", "/images/pasta-hero.png", "https://example.com/pasta.png"} {
		if !validImage(v) {
			t.Errorf("safe URL rejected: %q", v)
		}
	}
}

func TestOriginValidation(t *testing.T) {
	s := &Server{
		Config: config.Config{
			Env:         "development",
			FrontendURL: "http://localhost:5173",
		},
	}
	allowed := []string{
		"",
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:5174",
		"http://127.0.0.1:5174",
		"http://192.168.1.50:5173",
		"https://smart-kaffe.vercel.app",
		"https://smart-kaffe.vercel.app/",
		"https://smart-kaffe-git-main.vercel.app",
	}
	for _, o := range allowed {
		if !s.isAllowedOrigin(o) {
			t.Errorf("expected origin %q to be allowed in development", o)
		}
	}
	disallowed := []string{
		"https://evil.com",
		"http://attacker.site:5173",
		"https://other-kaffe.vercel.app",
	}
	for _, o := range disallowed {
		if s.isAllowedOrigin(o) {
			t.Errorf("expected origin %q to be rejected", o)
		}
	}

	prod := &Server{
		Config: config.Config{
			Env:         "production",
			FrontendURL: "https://smart-kaffe.vercel.app",
		},
	}
	if !prod.isAllowedOrigin("https://smart-kaffe.vercel.app") {
		t.Error("expected https://smart-kaffe.vercel.app to be allowed in production")
	}
	if !prod.isAllowedOrigin("https://smart-kaffe.vercel.app/") {
		t.Error("expected https://smart-kaffe.vercel.app/ with trailing slash to be allowed in production")
	}
	if prod.isAllowedOrigin("http://localhost:5173") {
		t.Error("expected localhost to be rejected in production without explicit config")
	}
}
