package server

import (
	"context"
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"koffe/api/internal/database"
	"koffe/api/internal/models"
	"koffe/api/internal/security"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) listAdmin(c fiber.Ctx, kind string) error {
	filter := bson.M{}
	switch kind {
	case "categories":
		v, e := list[models.Category](c.Context(), s.DB, kind, filter, page(c).SetSort(bson.D{{Key: "sortOrder", Value: 1}, {Key: "_id", Value: 1}}))
		if e != nil {
			return e
		}
		return c.JSON(v)
	case "products":
		if id := c.Query("categoryId"); id != "" {
			o, e := oid(id)
			if e != nil {
				return e
			}
			filter["categoryId"] = o
		}
		if q := strings.TrimSpace(c.Query("search")); q != "" {
			if len(q) > 200 {
				return bad("Слишком длинный поиск")
			}
			filter["name"] = bson.Regex{Pattern: regexp.QuoteMeta(q), Options: "i"}
		}
		v, e := list[models.Product](c.Context(), s.DB, kind, filter, page(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	case "tables":
		v, e := list[models.Table](c.Context(), s.DB, kind, filter, page(c).SetSort(bson.D{{Key: "number", Value: 1}}))
		if e != nil {
			return e
		}
		return c.JSON(v)
	case "employees":
		filter["role"] = bson.M{"$in": []string{"admin", "cashier"}}
		v, e := list[models.User](c.Context(), s.DB, "users", filter, page(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	}
	return mongo.ErrNoDocuments
}

type adminInput struct {
	Name            *string `json:"name"`
	SortOrder       *int    `json:"sortOrder"`
	IsActive        *bool   `json:"isActive"`
	CategoryID      *string `json:"categoryId"`
	Description     *string `json:"description"`
	ImageURL        *string `json:"imageUrl"`
	Price           *int64  `json:"price"`
	PreparationTime *int    `json:"preparationTime"`
	IsAvailable     *bool   `json:"isAvailable"`
	Number          *int    `json:"number"`
	Login           *string `json:"login"`
	Password        *string `json:"password"`
	Role            *string `json:"role"`
}

func (s *Server) writeAdmin(c fiber.Ctx, kind string, create, remove bool) error {
	var in adminInput
	if !remove {
		if e := decode(c, &in); e != nil {
			return e
		}
	}
	collection := kind
	if kind == "employees" {
		collection = "users"
	}
	id := bson.NewObjectID()
	if !create {
		var e error
		id, e = oid(c.Params("id"))
		if e != nil {
			return e
		}
	}
	set := bson.M{"updatedAt": time.Now().UTC()}
	if create {
		set["_id"] = id
		set["createdAt"] = time.Now().UTC()
	}
	if in.Name != nil {
		v := strings.TrimSpace(*in.Name)
		if utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 120 {
			return bad("Название должно содержать 1–120 символов")
		}
		set["name"] = v
	}
	if in.IsActive != nil {
		set["isActive"] = *in.IsActive
	}
	if create && kind != "products" {
		if _, ok := set["isActive"]; !ok {
			set["isActive"] = true
		}
	}
	switch kind {
	case "categories":
		if create && in.Name == nil {
			return bad("Введите название категории")
		}
		if in.SortOrder != nil {
			if *in.SortOrder < 0 || *in.SortOrder > 10000 {
				return bad("Некорректный порядок")
			}
			set["sortOrder"] = *in.SortOrder
		} else if create {
			set["sortOrder"] = 0
		}
	case "products":
		if create && (in.Name == nil || in.CategoryID == nil || in.Price == nil) {
			return bad("Укажите название, категорию и цену")
		}
		if in.CategoryID != nil {
			cat, e := oid(*in.CategoryID)
			if e != nil {
				return e
			}
			set["categoryId"] = cat
		}
		if in.Price != nil {
			if *in.Price < 1 || *in.Price > 100000000 {
				return bad("Цена должна быть от 0,01 до 1 000 000 сом")
			}
			set["price"] = *in.Price
		}
		if in.Description != nil {
			if utf8.RuneCountInString(*in.Description) > 2000 {
				return bad("Описание слишком длинное")
			}
			set["description"] = *in.Description
		} else if create {
			set["description"] = ""
		}
		if in.ImageURL != nil {
			if !validImage(*in.ImageURL) {
				return bad("Используйте HTTPS-ссылку на изображение")
			}
			set["imageUrl"] = *in.ImageURL
		} else if create {
			set["imageUrl"] = ""
		}
		if in.PreparationTime != nil {
			if *in.PreparationTime < 1 || *in.PreparationTime > 180 {
				return bad("Время приготовления: 1–180 минут")
			}
			set["preparationTime"] = *in.PreparationTime
		} else if create {
			set["preparationTime"] = 15
		}
		if in.IsAvailable != nil {
			set["isAvailable"] = *in.IsAvailable
		} else if create {
			set["isAvailable"] = true
		}
	case "tables":
		if create && in.Number == nil {
			return bad("Введите номер стола")
		}
		if in.Number != nil {
			if *in.Number < 1 || *in.Number > 9999 {
				return bad("Номер стола: 1–9999")
			}
			set["number"] = *in.Number
		}
		if create {
			set["status"] = "free"
		}
	case "employees":
		if create && (in.Name == nil || in.Login == nil || in.Password == nil || in.Role == nil) {
			return bad("Заполните имя, логин, роль и пароль")
		}
		if !create && in.Login != nil {
			return bad("Логин нельзя изменить")
		}
		if in.Login != nil {
			v := strings.ToLower(strings.TrimSpace(*in.Login))
			if !regexp.MustCompile(`^[a-z0-9._-]{3,80}$`).MatchString(v) {
				return bad("Логин: 3–80 латинских букв, цифр, точек, дефисов")
			}
			set["login"] = v
		}
		if in.Role != nil {
			if *in.Role != "cashier" {
				return bad("Допустимая роль сотрудника: cashier")
			}
			set["role"] = *in.Role
		}
		if in.Password != nil {
			if len(*in.Password) < 12 || len(*in.Password) > 128 {
				return bad("Пароль должен содержать 12–128 символов")
			}
			set["passwordHash"] = security.Password(*in.Password)
		}
		if create {
			set["version"] = 0
		}
	}
	// Reject fields that belong to a different resource instead of silently accepting them.
	allowed := map[string]map[string]bool{"categories": {"name": true, "sortOrder": true, "isActive": true}, "products": {"name": true, "categoryId": true, "description": true, "imageUrl": true, "price": true, "preparationTime": true, "isAvailable": true}, "tables": {"number": true, "isActive": true}, "employees": {"name": true, "login": true, "password": true, "role": true, "isActive": true}}
	if !remove {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(c.Body(), &fields)
		for k := range fields {
			if !allowed[kind][k] {
				return bad("Недопустимое поле: " + k)
			}
		}
	}
	if remove {
		if kind == "products" {
			set["isAvailable"] = false
		} else {
			set["isActive"] = false
		}
	}
	var result any
	_, e := database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		if kind == "employees" && !create {
			var u models.User
			if e := s.DB.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&u); e != nil {
				return nil, e
			}
			if u.Role == "admin" {
				return nil, fail(403, "admin_protected", "Изменение администратора недоступно")
			}
		}
		if cat, ok := set["categoryId"]; ok {
			n, e := s.DB.Collection("categories").CountDocuments(ctx, bson.M{"_id": cat})
			if e != nil {
				return nil, e
			}
			if n != 1 {
				return nil, bad("Категория не существует")
			}
		}
		if kind == "tables" && !create {
			n, e := s.DB.Collection("orders").CountDocuments(ctx, bson.M{"tableId": id, "status": bson.M{"$nin": []string{"paid", "cancelled"}}})
			if e != nil {
				return nil, e
			}
			if n > 0 {
				return nil, fail(409, "table_busy", "Сначала закройте заказы этого стола")
			}
		}
		if create {
			if _, e := s.DB.Collection(collection).InsertOne(ctx, set); e != nil {
				return nil, e
			}
		} else {
			update := bson.M{"$set": set}
			if kind == "employees" {
				update["$inc"] = bson.M{"version": 1}
			}
			r, e := s.DB.Collection(collection).UpdateOne(ctx, bson.M{"_id": id}, update)
			if e != nil {
				return nil, e
			}
			if r.MatchedCount != 1 {
				return nil, mongo.ErrNoDocuments
			}
			if kind == "employees" {
				if _, e = s.DB.Collection("refresh_sessions").DeleteMany(ctx, bson.M{"userId": id}); e != nil {
					return nil, e
				}
			}
		}
		switch kind {
		case "categories":
			var v models.Category
			if e := s.DB.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&v); e != nil {
				return nil, e
			}
			result = v
		case "products":
			var v models.Product
			if e := s.DB.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&v); e != nil {
				return nil, e
			}
			result = v
		case "tables":
			var v models.Table
			if e := s.DB.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&v); e != nil {
				return nil, e
			}
			result = v
		case "employees":
			var v models.User
			if e := s.DB.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&v); e != nil {
				return nil, e
			}
			result = v
		}
		action := "updated"
		if create {
			action = "created"
		}
		if remove {
			action = "disabled"
		}
		return nil, s.audit(ctx, current(c).ID, kind+"."+action, collection, id, bson.M{})
	})
	if e != nil {
		return e
	}
	if kind == "products" || kind == "categories" {
		s.Hub.Publish(Event{Type: "menu.updated", Data: fiber.Map{"id": id}})
	}
	if kind == "tables" {
		s.Hub.Publish(Event{Type: "table.updated", Data: result})
	}
	if create {
		return c.Status(201).JSON(result)
	}
	return c.JSON(result)
}
func validImage(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > 2048 || strings.ContainsAny(v, "\r\n\t\\") {
		return false
	}
	if strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") {
		return true
	}
	u, e := url.Parse(v)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}
func (s *Server) rotateQR(c fiber.Ctx) error {
	id, e := oid(c.Params("id"))
	if e != nil {
		return e
	}
	token := security.SignTable(id.Hex(), s.Config.TableSecret)
	qrURL := strings.TrimSpace(strings.Split(s.Config.FrontendURL, ",")[0]) + "/menu/table/" + token
	encrypted, e := security.EncryptQR(qrURL, id.Hex(), s.Config.TableSecret)
	if e != nil {
		return e
	}
	_, e = database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		r, e := s.DB.Collection("tables").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"tokenHash": security.Hash(token), "encryptedQR": encrypted, "updatedAt": time.Now()}})
		if e != nil {
			return nil, e
		}
		if r.MatchedCount != 1 {
			return nil, mongo.ErrNoDocuments
		}
		return nil, s.audit(ctx, current(c).ID, "table.qr_rotated", "tables", id, bson.M{})
	})
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"tableToken": token, "url": qrURL})
}

func (s *Server) getQR(c fiber.Ctx) error {
	id, e := oid(c.Params("id"))
	if e != nil {
		return e
	}
	var table models.Table
	if e = s.DB.Collection("tables").FindOne(c.Context(), bson.M{"_id": id}).Decode(&table); e != nil {
		return e
	}
	if table.EncryptedQR == "" {
		return fail(404, "qr_not_saved", "QR-код не сохранён для повторного просмотра. Вставьте действующую ссылку или выпустите новый код.")
	}
	raw, e := security.DecryptQR(table.EncryptedQR, id.Hex(), s.Config.TableSecret)
	if e != nil {
		return fail(409, "qr_unreadable", "Не удалось прочитать сохранённый QR. Проверьте TABLE_TOKEN_SECRET.")
	}
	token, e := s.qrToken(raw, id)
	if e != nil {
		return e
	}
	if security.Hash(token) != table.TokenHash {
		return fail(409, "qr_changed", "QR-код был заменён. Обновите страницу.")
	}
	return c.JSON(fiber.Map{"tableToken": token, "url": raw})
}

func (s *Server) qrToken(raw string, id bson.ObjectID) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/menu/table/") {
		return "", bad("Вставьте полную ссылку QR-кода стола")
	}
	token := strings.TrimPrefix(u.Path, "/menu/table/")
	tableID, e := security.VerifyTable(token, s.Config.TableSecret)
	if e != nil || tableID != id.Hex() {
		return "", bad("Этот QR-код не принадлежит выбранному столу")
	}
	return token, nil
}

func (s *Server) restoreQR(c fiber.Ctx) error {
	id, e := oid(c.Params("id"))
	if e != nil {
		return e
	}
	var input struct {
		URL string `json:"url"`
	}
	if e = decode(c, &input); e != nil {
		return e
	}
	input.URL = strings.TrimSpace(input.URL)
	token, e := s.qrToken(input.URL, id)
	if e != nil {
		return e
	}
	encrypted, e := security.EncryptQR(input.URL, id.Hex(), s.Config.TableSecret)
	if e != nil {
		return e
	}
	_, e = database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		r, e := s.DB.Collection("tables").UpdateOne(ctx, bson.M{"_id": id, "tokenHash": security.Hash(token)}, bson.M{"$set": bson.M{"encryptedQR": encrypted}})
		if e != nil {
			return nil, e
		}
		if r.MatchedCount != 1 {
			return nil, fail(409, "qr_revoked", "Ссылка недействительна или QR-код уже был заменён")
		}
		return nil, s.audit(ctx, current(c).ID, "table.qr_restored", "tables", id, bson.M{})
	})
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"tableToken": token, "url": input.URL})
}
func (s *Server) auditList(c fiber.Ctx) error {
	v, e := list[models.Audit](c.Context(), s.DB, "audit_logs", bson.M{}, page(c))
	if e != nil {
		return e
	}
	return c.JSON(v)
}
func (s *Server) dashboard(c fiber.Ctx) error {
	loc := time.FixedZone("Asia/Bishkek", 6*3600)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	start := today.AddDate(0, 0, -6)
	orders, e := list[models.Order](c.Context(), s.DB, "orders", bson.M{"createdAt": bson.M{"$gte": start}})
	if e != nil {
		return e
	}
	recent, e := list[models.Order](c.Context(), s.DB, "orders", bson.M{}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(8))
	if e != nil {
		return e
	}
	var revenue, count, paid int64
	popular := map[string]bson.M{}
	daily := []bson.M{}
	for i := 0; i < 7; i++ {
		daily = append(daily, bson.M{"date": start.AddDate(0, 0, i).Format("2006-01-02"), "revenue": int64(0), "orders": int64(0)})
	}
	for _, o := range orders {
		date := o.CreatedAt.In(loc).Format("2006-01-02")
		if !o.CreatedAt.Before(today) {
			count++
		}
		for _, d := range daily {
			if d["date"] == date {
				d["orders"] = d["orders"].(int64) + 1
				if o.Status == "paid" {
					d["revenue"] = d["revenue"].(int64) + o.TotalPrice
				}
			}
		}
		if o.Status != "paid" {
			continue
		}
		if !o.CreatedAt.Before(today) {
			revenue += o.TotalPrice
			paid++
		}
		for _, i := range o.Items {
			p := popular[i.Name]
			if p == nil {
				p = bson.M{"name": i.Name, "quantity": int64(0), "revenue": int64(0)}
				popular[i.Name] = p
			}
			p["quantity"] = p["quantity"].(int64) + int64(i.Quantity)
			p["revenue"] = p["revenue"].(int64) + i.Subtotal
		}
	}
	active, e := s.DB.Collection("orders").CountDocuments(c.Context(), bson.M{"status": bson.M{"$nin": []string{"paid", "cancelled"}}})
	if e != nil {
		return e
	}
	tables, e := s.DB.Collection("tables").CountDocuments(c.Context(), bson.M{"isActive": true})
	if e != nil {
		return e
	}
	occupied, e := s.DB.Collection("tables").CountDocuments(c.Context(), bson.M{"isActive": true, "status": bson.M{"$nin": []string{"free", "closed"}}})
	if e != nil {
		return e
	}
	products := []bson.M{}
	for _, p := range popular {
		products = append(products, p)
	}
	sortPopular(products)
	if len(products) > 5 {
		products = products[:5]
	}
	average := int64(0)
	if paid > 0 {
		average = revenue / paid
	}
	return c.JSON(fiber.Map{"revenueToday": revenue, "ordersToday": count, "activeOrders": active, "averageOrder": average, "occupiedTables": occupied, "totalTables": tables, "recentOrders": recent, "popularProducts": products, "dailyRevenue": daily})
}
