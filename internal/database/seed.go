package database

import (
	"context"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"koffe/api/internal/models"
	"koffe/api/internal/security"
)

// Seed inserts missing demo records; it never resets existing passwords or menu edits.
func Seed(ctx context.Context, db *mongo.Database) error {
	for _, role := range []string{"admin", "cashier"} {
		n, e := db.Collection("users").CountDocuments(ctx, bson.M{"login": role})
		if e != nil {
			return e
		}
		if n == 0 {
			u := models.User{Base: models.NewBase(), Name: map[string]string{"admin": "Администратор", "cashier": "Кассир"}[role], Login: role, PasswordHash: security.Password("ChangeMe123!"), Role: role, IsActive: true}
			if _, e = db.Collection("users").InsertOne(ctx, u); e != nil && !mongo.IsDuplicateKeyError(e) {
				return e
			}
		}
	}
	n, e := db.Collection("categories").CountDocuments(ctx, bson.M{})
	if e != nil {
		return e
	}
	if n == 0 {
		_, e = Transaction(ctx, db, func(ctx context.Context) (any, error) {
			names := []string{"Завтраки", "Горячее", "Салаты", "Десерты", "Напитки"}
			ids := []bson.ObjectID{}
			for i, name := range names {
				cat := models.Category{Base: models.NewBase(), Name: name, SortOrder: i, IsActive: true}
				ids = append(ids, cat.ID)
				if _, e := db.Collection("categories").InsertOne(ctx, cat); e != nil {
					return nil, e
				}
			}
			recipes := []struct {
				name, description, image string
				category                 int
				price                    int64
				minutes                  int
			}{
				{"Паста с курицей", "Паста, куриное филе, сливочный соус, пармезан, зелень", "/images/pasta-hero.png", 1, 42000, 15},
				{"Салат Цезарь", "Куриное филе, лист салата, черри, пармезан, соус Цезарь", "", 2, 35000, 10},
				{"Бургер с говядиной", "Говяжья котлета, свежие овощи, сыр чеддер, фирменный соус", "", 1, 39000, 20},
				{"Сырники с ягодами", "Творог, свежие ягоды, сметана, мятный сироп", "", 0, 28000, 15},
				{"Чизкейк", "Нежный творожный чизкейк с ягодным соусом", "", 3, 24000, 10},
				{"Ягодный лимонад", "Свежие ягоды, лимон, мята, газированная вода", "", 4, 18000, 5},
			}
			for _, r := range recipes {
				p := models.Product{Base: models.NewBase(), Name: r.name, Description: r.description, ImageURL: r.image, CategoryID: ids[r.category], Price: r.price, PreparationTime: r.minutes, IsAvailable: true}
				if _, e := db.Collection("products").InsertOne(ctx, p); e != nil {
					return nil, e
				}
			}
			return nil, nil
		})
		if e != nil {
			return e
		}
	}
	for number := 1; number <= 12; number++ {
		n, e := db.Collection("tables").CountDocuments(ctx, bson.M{"number": number})
		if e != nil {
			return e
		}
		if n == 0 {
			if _, e = db.Collection("tables").InsertOne(ctx, models.Table{Base: models.NewBase(), Number: number, IsActive: true, Status: "free"}); e != nil && !mongo.IsDuplicateKeyError(e) {
				return e
			}
		}
	}
	return nil
}
