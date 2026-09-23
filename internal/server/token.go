package server

import (
	"koffe/api/internal/security"
	"time"
)

func securityClaims(token, secret string) (time.Time, error) {
	c, e := security.Parse(token, secret)
	if e != nil {
		return time.Time{}, e
	}
	return c.ExpiresAt.Time, nil
}
