package database

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
	"koffe/api/internal/config"
	"net"
	"os"
	"strings"
	"time"
)

func Open(ctx context.Context, c config.Config) (*mongo.Database, error) {
	if server := os.Getenv("MONGODB_DNS_SERVER"); server != "" {
		if net.ParseIP(server) == nil {
			return nil, errors.New("MONGODB_DNS_SERVER must be an IP address")
		}
		net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(server, "53"))
		}}
	}
	cl, e := mongo.Connect(options.Client().ApplyURI(c.MongoURI).SetMaxPoolSize(50).SetServerSelectionTimeout(10 * time.Second).SetTimeout(15 * time.Second))
	if e != nil {
		return nil, errors.New("invalid MongoDB connection configuration")
	}
	if e = cl.Ping(ctx, nil); e != nil {
		_ = cl.Disconnect(ctx)
		message := strings.ToLower(e.Error())
		reason := "network access, Atlas IP allowlist or TLS"
		if strings.Contains(message, "authentication") || strings.Contains(message, "auth error") {
			reason = "authentication: check username/password and authSource"
		}
		if strings.Contains(message, "no such host") || strings.Contains(message, "dns") || strings.Contains(message, "lookup ") {
			reason = "DNS resolution"
		}
		return nil, errors.New("MongoDB unavailable: " + reason)
	}
	return cl.Database(c.Database), nil
}
func Transaction(ctx context.Context, db *mongo.Database, fn func(context.Context) (any, error)) (any, error) {
	s, e := db.Client().StartSession()
	if e != nil {
		return nil, e
	}
	defer s.EndSession(ctx)
	return s.WithTransaction(ctx, fn, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
}
func Migrate(ctx context.Context, db *mongo.Database) error {
	unique := func(key string) mongo.IndexModel {
		return mongo.IndexModel{Keys: bson.D{{Key: key, Value: 1}}, Options: options.Index().SetUnique(true)}
	}
	idx := func(key string) mongo.IndexModel { return mongo.IndexModel{Keys: bson.D{{Key: key, Value: 1}}} }
	specs := map[string][]mongo.IndexModel{
		"users": {unique("login")}, "categories": {idx("sortOrder")}, "products": {idx("categoryId"), idx("isAvailable")},
		"tables":         {unique("number"), {Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"tokenHash": bson.M{"$type": "string"}})}},
		"orders":         {unique("publicToken"), unique("orderNumber"), unique("idempotencyKey"), idx("status"), idx("tableId"), idx("createdAt"), idx("expiresAt")},
		"table_sessions": {{Keys: bson.D{{Key: "tableId", Value: 1}}, Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "active"})}},
		"audit_logs":     {idx("createdAt")}, "refresh_sessions": {unique("tokenHash"), {Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
		"order_expirations": {{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
	}
	for name, indexes := range specs {
		if _, e := db.Collection(name).Indexes().CreateMany(ctx, indexes); e != nil {
			return e
		}
	}
	return nil
}
