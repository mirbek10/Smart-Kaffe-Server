package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/bson"
	"koffe/api/internal/database"
	"koffe/api/internal/models"
	"koffe/api/internal/security"
	"strings"
	"time"
)

func (s *Server) initDummy() { s.dummyPassword = security.Password(security.Random()) }
func (s *Server) authenticate(ctx context.Context, token string) (models.User, error) {
	var u models.User
	cl, e := security.Parse(token, s.Config.JWTSecret)
	if e != nil {
		return u, fail(401, "unauthorized", "Войдите в систему")
	}
	id, e := bson.ObjectIDFromHex(cl.Subject)
	if e != nil {
		return u, fail(401, "unauthorized", "Недействительная сессия")
	}
	e = s.DB.Collection("users").FindOne(ctx, bson.M{"_id": id, "isActive": true, "version": cl.Version, "role": bson.M{"$in": []string{"admin", "cashier"}}}).Decode(&u)
	if e != nil {
		return u, fail(401, "unauthorized", "Сессия завершена")
	}
	return u, nil
}
func (s *Server) require(roles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			return fail(401, "unauthorized", "Войдите в систему")
		}
		u, e := s.authenticate(c.Context(), strings.TrimPrefix(h, "Bearer "))
		if e != nil {
			return e
		}
		if len(roles) > 0 {
			ok := false
			for _, r := range roles {
				if u.Role == r {
					ok = true
				}
			}
			if !ok {
				return fail(403, "forbidden", "Недостаточно прав")
			}
		}
		c.Locals("user", u)
		return c.Next()
	}
}
func (s *Server) refreshHash(v string) string {
	h := hmac.New(sha256.New, []byte(s.Config.RefreshSecret))
	h.Write([]byte(v))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) cookie(c fiber.Ctx, token string, expires time.Time) {
	sameSite := "Lax"
	secure := false
	if s.Config.Env == "production" {
		sameSite = "None"
		secure = true
	}
	c.Cookie(&fiber.Cookie{
		Name:     "tamak_refresh",
		Value:    token,
		Path:     "/api/auth",
		HTTPOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  expires,
	})
}
func (s *Server) session(ctx context.Context, u models.User, raw string) error {
	_, e := s.DB.Collection("refresh_sessions").InsertOne(ctx, bson.M{"_id": bson.NewObjectID(), "userId": u.ID, "version": u.Version, "tokenHash": s.refreshHash(raw), "expiresAt": time.Now().Add(7 * 24 * time.Hour)})
	return e
}
func (s *Server) authResponse(c fiber.Ctx, u models.User, raw string) error {
	access, e := security.Access(u.ID.Hex(), u.Version, s.Config.JWTSecret)
	if e != nil {
		return e
	}
	s.cookie(c, raw, time.Now().Add(7*24*time.Hour))
	return c.JSON(fiber.Map{"accessToken": access, "user": u})
}
func (s *Server) login(c fiber.Ctx) error {
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if e := decode(c, &in); e != nil {
		return e
	}
	in.Login = strings.ToLower(strings.TrimSpace(in.Login))
	if len(in.Login) > 80 || len(in.Password) > 128 {
		return bad("Некорректный логин или пароль")
	}
	var u models.User
	e := s.DB.Collection("users").FindOne(c.Context(), bson.M{"login": in.Login}).Decode(&u)
	hash := u.PasswordHash
	if e != nil {
		hash = s.dummyPassword
	}
	valid := security.VerifyPassword(hash, in.Password)
	if e != nil || !valid || !u.IsActive || (u.Role != "admin" && u.Role != "cashier") {
		return fail(401, "invalid_credentials", "Неверный логин или пароль")
	}
	raw := security.Random()
	_, e = database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		if e := s.session(ctx, u, raw); e != nil {
			return nil, e
		}
		return nil, s.audit(ctx, u.ID, "auth.login", "users", u.ID, bson.M{})
	})
	if e != nil {
		return e
	}
	return s.authResponse(c, u, raw)
}
func (s *Server) refresh(c fiber.Ctx) error {
	old := c.Cookies("tamak_refresh")
	if old == "" {
		return fail(401, "unauthorized", "Сессия завершена")
	}
	raw := security.Random()
	var u models.User
	_, e := database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		var r struct {
			UserID  bson.ObjectID `bson:"userId"`
			Version int           `bson:"version"`
		}
		e := s.DB.Collection("refresh_sessions").FindOneAndDelete(ctx, bson.M{"tokenHash": s.refreshHash(old), "expiresAt": bson.M{"$gt": time.Now()}}).Decode(&r)
		if e != nil {
			return nil, fail(401, "unauthorized", "Сессия завершена")
		}
		e = s.DB.Collection("users").FindOne(ctx, bson.M{"_id": r.UserID, "version": r.Version, "isActive": true, "role": bson.M{"$in": []string{"admin", "cashier"}}}).Decode(&u)
		if e != nil {
			return nil, fail(401, "unauthorized", "Сессия завершена")
		}
		return nil, s.session(ctx, u, raw)
	})
	if e != nil {
		s.cookie(c, "", time.Unix(1, 0))
		return e
	}
	return s.authResponse(c, u, raw)
}
func (s *Server) logout(c fiber.Ctx) error {
	raw := c.Cookies("tamak_refresh")
	if raw != "" {
		if _, e := s.DB.Collection("refresh_sessions").DeleteOne(c.Context(), bson.M{"tokenHash": s.refreshHash(raw)}); e != nil {
			return e
		}
	}
	s.cookie(c, "", time.Unix(1, 0))
	return c.JSON(fiber.Map{"ok": true})
}
func (s *Server) password(c fiber.Ctx) error {
	var in struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if e := decode(c, &in); e != nil {
		return e
	}
	u := current(c)
	if len(in.Current) > 128 || !security.VerifyPassword(u.PasswordHash, in.Current) {
		return fail(400, "invalid_password", "Текущий пароль неверен")
	}
	if len(in.New) < 12 || len(in.New) > 128 {
		return bad("Новый пароль должен содержать 12–128 символов")
	}
	hash := security.Password(in.New)
	_, e := database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		r, e := s.DB.Collection("users").UpdateOne(ctx, bson.M{"_id": u.ID, "version": u.Version}, bson.M{"$set": bson.M{"passwordHash": hash, "updatedAt": time.Now()}, "$inc": bson.M{"version": 1}})
		if e != nil {
			return nil, e
		}
		if r.MatchedCount != 1 {
			return nil, fail(409, "conflict", "Сессия уже изменена")
		}
		if _, e = s.DB.Collection("refresh_sessions").DeleteMany(ctx, bson.M{"userId": u.ID}); e != nil {
			return nil, e
		}
		return nil, s.audit(ctx, u.ID, "auth.password_changed", "users", u.ID, bson.M{})
	})
	if e != nil {
		return e
	}
	s.cookie(c, "", time.Unix(1, 0))
	return c.JSON(fiber.Map{"ok": true})
}
