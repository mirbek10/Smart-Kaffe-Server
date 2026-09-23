package main

import (
	"context"
	"koffe/api/internal/config"
	"koffe/api/internal/database"
	"log"
	"time"
)

func main() {
	c, e := config.Load()
	if e != nil {
		log.Fatal(e)
	}
	if c.Env == "production" {
		log.Fatal("demo seed is disabled in production")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, e := database.Open(ctx, c)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Client().Disconnect(ctx)
	if database.Migrate(ctx, db) != nil {
		log.Fatal("migration failed")
	}
	if database.Seed(ctx, db) != nil {
		log.Fatal("seed failed")
	}
	log.Print("Seed complete. Change all demo passwords immediately.")
}
