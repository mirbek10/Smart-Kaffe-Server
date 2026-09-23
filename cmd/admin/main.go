// admin provisions the first production administrator from process environment.
package main

import (
	"context"
	"go.mongodb.org/mongo-driver/v2/bson"
	"koffe/api/internal/config"
	"koffe/api/internal/database"
	"koffe/api/internal/models"
	"koffe/api/internal/security"
	"log"
	"net/mail"
	"os"
	"strings"
	"time"
)

func main() {
	c, e := config.Load()
	if e != nil {
		log.Fatal(e)
	}
	login := strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_LOGIN")))
	if email := strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))); email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			log.Fatal("ADMIN_EMAIL must be a valid email address")
		}
		login = email
	}
	password := os.Getenv("ADMIN_PASSWORD")
	name := strings.TrimSpace(os.Getenv("ADMIN_NAME"))
	if login == "" || name == "" || len(password) < 12 || len(password) > 128 {
		log.Fatal("Set ADMIN_EMAIL (or ADMIN_LOGIN), ADMIN_NAME and ADMIN_PASSWORD (12-128 characters) in environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, e := database.Open(ctx, c)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Client().Disconnect(ctx)
	if database.Migrate(ctx, db) != nil {
		log.Fatal("migration failed")
	}
	n, e := db.Collection("users").CountDocuments(ctx, bson.M{"role": "admin"})
	if e != nil {
		log.Fatal("could not check administrators")
	}
	if n > 0 {
		log.Fatal("administrator already exists; refusing to create another")
	}
	u := models.User{Base: models.NewBase(), Name: name, Login: login, PasswordHash: security.Password(password), Role: "admin", IsActive: true}
	if _, e = db.Collection("users").InsertOne(ctx, u); e != nil {
		log.Fatal("administrator creation failed")
	}
	log.Print("Administrator created")
}
