package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"io"
	"koffe/api/internal/config"
	"koffe/api/internal/models"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	DB            *mongo.Database
	Config        config.Config
	Hub           *Hub
	App           *fiber.App
	dummyPassword string
}
type problem struct {
	status        int
	code, message string
}

func (e *problem) Error() string                  { return e.message }
func fail(status int, code, message string) error { return &problem{status, code, message} }
func bad(message string) error                    { return fail(400, "validation_error", message) }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.socket)
	mux.Handle("/", adaptor.FiberApp(s.App))
	return mux
}
func New(db *mongo.Database, c config.Config) *Server {
	s := &Server{DB: db, Config: c, Hub: NewHub()}
	s.initDummy()
	app := fiber.New(fiber.Config{BodyLimit: 64 * 1024, ErrorHandler: func(c fiber.Ctx, e error) error {
		var p *problem
		if errors.As(e, &p) {
			return c.Status(p.status).JSON(fiber.Map{"error": p.message, "code": p.code})
		}
		if errors.Is(e, mongo.ErrNoDocuments) {
			return c.Status(404).JSON(fiber.Map{"error": "Запись не найдена", "code": "not_found"})
		}
		if mongo.IsDuplicateKeyError(e) {
			return c.Status(409).JSON(fiber.Map{"error": "Такая запись уже существует", "code": "conflict"})
		}
		var f *fiber.Error
		if errors.As(e, &f) {
			return c.Status(f.Code).JSON(fiber.Map{"error": f.Message, "code": "request_error"})
		}
		slog.Error("request failed", "method", c.Method(), "errorType", fmtType(e))
		return c.Status(500).JSON(fiber.Map{"error": "Ошибка сервера. Попробуйте ещё раз.", "code": "internal_error"})
	}})
	s.App = app
	app.Use(recover.New())
	app.Use(func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 20*time.Second)
		defer cancel()
		c.SetContext(ctx)
		c.Set("Cache-Control", "no-store")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "no-referrer")
		if s.Config.Env == "production" {
			c.Set("Strict-Transport-Security", "max-age=31536000")
		}
		return c.Next()
	})
	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: s.isAllowedOrigin,
		AllowCredentials: true,
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Idempotency-Key"},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
	}))
	app.Use(func(c fiber.Ctx) error {
		if c.Method() != "GET" && c.Method() != "HEAD" && c.Method() != "OPTIONS" {
			origin := c.Get("Origin")
			if origin != "" && !s.isAllowedOrigin(origin) {
				return fail(403, "origin_rejected", "Недопустимый источник запроса")
			}
		}
		return c.Next()
	})
	app.Get("/healthz", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	app.Get("/readyz", func(c fiber.Ctx) error {
		if e := s.DB.Client().Ping(c.Context(), nil); e != nil {
			return fail(503, "database_unavailable", "База данных недоступна")
		}
		return c.JSON(fiber.Map{"ok": true})
	})
	app.Get("/openapi.json", s.openapi)
	docsHandler := func(c fiber.Ctx) error { c.Type("html"); return c.SendString(docsHTML) }
	app.Get("/docs", docsHandler)
	app.Get("/swagger", docsHandler)
	app.Get("/swagger/*", docsHandler)
	auth := app.Group("/api/auth")
	auth.Post("/login", limiter.New(limiter.Config{Max: 10, Expiration: time.Minute}), s.login)
	auth.Post("/refresh", limiter.New(limiter.Config{Max: 60, Expiration: time.Minute}), s.refresh)
	auth.Post("/logout", s.logout)
	auth.Get("/me", s.require(), func(c fiber.Ctx) error { return c.JSON(current(c)) })
	auth.Patch("/password", s.require(), s.password)
	pub := app.Group("/api/public", limiter.New(limiter.Config{Max: 180, Expiration: time.Minute}))
	pub.Get("/categories", s.publicCategories)
	pub.Get("/products", s.publicProducts)
	pub.Get("/menu/:tableToken", s.menu)
	pub.Post("/orders", limiter.New(limiter.Config{Max: 12, Expiration: time.Minute}), s.createOrder)
	pub.Get("/orders/:publicToken", s.publicOrder)
	pub.Post("/orders/:publicToken/cancel", s.cancelOrder)
	admin := app.Group("/api/admin", s.require("admin"))
	admin.Get("/dashboard", s.dashboard)
	admin.Get("/audit-log", s.auditList)
	admin.Get("/orders", s.orders)
	for _, kind := range []string{"categories", "products", "tables", "employees"} {
		k := kind
		admin.Get("/"+k, func(c fiber.Ctx) error { return s.listAdmin(c, k) })
		admin.Post("/"+k, func(c fiber.Ctx) error { return s.writeAdmin(c, k, true, false) })
		admin.Patch("/"+k+"/:id", func(c fiber.Ctx) error { return s.writeAdmin(c, k, false, false) })
		admin.Delete("/"+k+"/:id", func(c fiber.Ctx) error { return s.writeAdmin(c, k, false, true) })
	}
	admin.Post("/tables/:id/qr", s.rotateQR)
	admin.Get("/tables/:id/qr", s.getQR)
	admin.Post("/tables/:id/qr/restore", s.restoreQR)
	cashier := app.Group("/api/cashier", s.require("cashier"))
	cashier.Get("/orders", s.orders)
	cashier.Get("/tables", func(c fiber.Ctx) error { return s.listAdmin(c, "tables") })
	for _, action := range []string{"confirm", "accept", "reject", "preparing", "ready", "delivering", "deliver", "pay"} {
		a := action
		cashier.Post("/orders/:id/"+a, func(c fiber.Ctx) error { return s.changeOrder(c, a) })
	}
	return s
}
func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if (u.Scheme == "https" || u.Scheme == "http") && (host == "smart-kaffe.vercel.app" || (strings.HasPrefix(host, "smart-kaffe") && strings.HasSuffix(host, ".vercel.app"))) {
		return true
	}
	if s.Config.Env != "production" {
		if host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "172.") {
			return true
		}
	}
	for _, target := range strings.Split(s.Config.FrontendURL, ",") {
		target = strings.TrimRight(strings.TrimSpace(target), "/")
		if target != "" && strings.EqualFold(origin, target) {
			return true
		}
		if fu, err := url.Parse(target); err == nil && fu.Hostname() != "" {
			if strings.EqualFold(u.Scheme, fu.Scheme) && strings.EqualFold(host, fu.Hostname()) {
				return true
			}
		}
	}
	return false
}
func fmtType(e error) string {
	switch {
	case errors.Is(e, context.DeadlineExceeded):
		return "timeout"
	default:
		return "database_or_internal"
	}
}
func decode(c fiber.Ctx, v any) error {
	if !strings.HasPrefix(c.Get("Content-Type"), "application/json") {
		return fail(415, "content_type", "Требуется application/json")
	}
	d := json.NewDecoder(strings.NewReader(string(c.Body())))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("Некорректные данные запроса")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return bad("Некорректный JSON")
	}
	return nil
}
func oid(v string) (bson.ObjectID, error) {
	id, e := bson.ObjectIDFromHex(v)
	if e != nil {
		return id, bad("Некорректный идентификатор")
	}
	return id, nil
}
func current(c fiber.Ctx) models.User { return c.Locals("user").(models.User) }
func page(c fiber.Ctx) *options.FindOptionsBuilder {
	n, _ := strconv.Atoi(c.Query("limit", "50"))
	off, _ := strconv.Atoi(c.Query("offset", "0"))
	if n < 1 {
		n = 50
	}
	if n > 200 {
		n = 200
	}
	if off < 0 {
		off = 0
	}
	return options.Find().SetLimit(int64(n)).SetSkip(int64(off)).SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}})
}
func list[T any](ctx context.Context, db *mongo.Database, name string, filter any, opts ...options.Lister[options.FindOptions]) ([]T, error) {
	out := []T{}
	cur, e := db.Collection(name).Find(ctx, filter, opts...)
	if e != nil {
		return nil, e
	}
	defer cur.Close(ctx)
	e = cur.All(ctx, &out)
	return out, e
}
func (s *Server) audit(ctx context.Context, user bson.ObjectID, action, entity string, id bson.ObjectID, meta bson.M) error {
	_, e := s.DB.Collection("audit_logs").InsertOne(ctx, models.Audit{ID: bson.NewObjectID(), UserID: user, Action: action, Entity: entity, EntityID: id, Metadata: meta, CreatedAt: time.Now().UTC()})
	return e
}
