package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/joho/godotenv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port, Env, MongoURI, Database, JWTSecret, RefreshSecret, TableSecret, FrontendURL string
	ConfirmationMinutes                                                               int
}

func Load() (Config, error) {
	root, _ := os.Getwd()
	for candidate := root; ; candidate = filepath.Dir(candidate) {
		if _, e := os.Stat(filepath.Join(candidate, "go.mod")); e == nil {
			root = candidate
			break
		}
		if filepath.Dir(candidate) == candidate {
			break
		}
	}
	_ = godotenv.Load(filepath.Join(root, ".env.local"))
	_ = godotenv.Load(filepath.Join(root, ".env"))
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	// APP_PORT is the explicit project setting; PORT is supported by Render,
	// Railway and other container platforms that inject it automatically.
	port := get("APP_PORT", "")
	if port == "" {
		port = get("PORT", "8080")
	}
	c := Config{Port: port, Env: get("APP_ENV", "development"), MongoURI: get("MONGODB_URI", "mongodb://localhost:27017/?replicaSet=rs0"), Database: get("MONGODB_DATABASE", "cafe_mvp"), FrontendURL: strings.TrimRight(get("FRONTEND_URL", "http://localhost:5173,https://smart-kaffe.vercel.app"), "/"), ConfirmationMinutes: 5}
	if v := os.Getenv("ORDER_CONFIRMATION_TIMEOUT_MINUTES"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 60 {
			return c, errors.New("invalid confirmation timeout")
		}
		c.ConfirmationMinutes = n
	}
	secrets := map[string]*string{"JWT_SECRET": &c.JWTSecret, "JWT_REFRESH_SECRET": &c.RefreshSecret, "TABLE_TOKEN_SECRET": &c.TableSecret}
	generated := map[string]string{}
	for k, p := range secrets {
		*p = os.Getenv(k)
		if len(*p) < 32 {
			if c.Env == "production" {
				return c, errors.New(k + " must have at least 32 characters")
			}
			b := make([]byte, 48)
			if _, e := rand.Read(b); e != nil {
				return c, e
			}
			*p = base64.RawURLEncoding.EncodeToString(b)
			generated[k] = *p
		}
	}
	if len(generated) > 0 {
		path := filepath.Join(root, ".env.local")
		existing, _ := godotenv.Read(path)
		if existing == nil {
			existing = map[string]string{}
		}
		for k, v := range generated {
			existing[k] = v
		}
		if e := godotenv.Write(existing, path); e != nil {
			return c, e
		}
		_ = os.Chmod(path, 0600)
	}
	return c, nil
}
