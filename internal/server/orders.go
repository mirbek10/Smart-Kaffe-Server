package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"koffe/api/internal/database"
	"koffe/api/internal/models"
	"koffe/api/internal/security"
	"time"
	"unicode/utf8"
)

type orderInput struct {
	TableToken string `json:"tableToken"`
	Items      []struct {
		ProductID string `json:"productId"`
		Quantity  int    `json:"quantity"`
	} `json:"items"`
	CustomerComment string `json:"customerComment"`
}

func (s *Server) table(ctx context.Context, token string) (models.Table, error) {
	var t models.Table
	id, e := security.VerifyTable(token, s.Config.TableSecret)
	if e != nil {
		return t, fail(404, "invalid_table", "QR-код недействителен. Обратитесь к сотруднику кафе.")
	}
	obj, e := oid(id)
	if e != nil {
		return t, e
	}
	e = s.DB.Collection("tables").FindOne(ctx, bson.M{"_id": obj, "tokenHash": security.Hash(token), "isActive": true}).Decode(&t)
	if errors.Is(e, mongo.ErrNoDocuments) {
		e = fail(404, "invalid_table", "QR-код недействителен. Обратитесь к сотруднику кафе.")
	}
	return t, e
}
func (s *Server) publicCategories(c fiber.Ctx) error {
	v, e := list[models.Category](c.Context(), s.DB, "categories", bson.M{"isActive": true}, options.Find().SetSort(bson.D{{Key: "sortOrder", Value: 1}, {Key: "_id", Value: 1}}))
	if e != nil {
		return e
	}
	return c.JSON(v)
}
func (s *Server) publicProducts(c fiber.Ctx) error {
	v, e := s.menuProducts(c.Context())
	if e != nil {
		return e
	}
	return c.JSON(v)
}
func (s *Server) menuProducts(ctx context.Context) ([]models.Product, error) {
	cats, e := list[models.Category](ctx, s.DB, "categories", bson.M{"isActive": true})
	if e != nil {
		return nil, e
	}
	ids := []bson.ObjectID{}
	for _, c := range cats {
		ids = append(ids, c.ID)
	}
	return list[models.Product](ctx, s.DB, "products", bson.M{"categoryId": bson.M{"$in": ids}}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
}
func (s *Server) menu(c fiber.Ctx) error {
	t, e := s.table(c.Context(), c.Params("tableToken"))
	if e != nil {
		return e
	}
	cats, e := list[models.Category](c.Context(), s.DB, "categories", bson.M{"isActive": true}, options.Find().SetSort(bson.D{{Key: "sortOrder", Value: 1}}))
	if e != nil {
		return e
	}
	products, e := s.menuProducts(c.Context())
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"table": t, "categories": cats, "products": products})
}
func validateOrder(in orderInput, key string) error {
	if _, e := uuid.Parse(key); e != nil {
		return bad("Требуется UUID в Idempotency-Key")
	}
	if len(in.Items) < 1 || len(in.Items) > 50 || utf8.RuneCountInString(in.CustomerComment) > 500 || len(in.TableToken) > 256 {
		return bad("Проверьте состав заказа и комментарий")
	}
	seen := map[string]bool{}
	for _, i := range in.Items {
		id, e := oid(i.ProductID)
		if e != nil {
			return e
		}
		if i.Quantity < 1 || i.Quantity > 20 || seen[id.Hex()] {
			return bad("Количество должно быть от 1 до 20, без повторения позиций")
		}
		seen[id.Hex()] = true
	}
	return nil
}
func (s *Server) createOrder(c fiber.Ctx) error {
	var in orderInput
	if e := decode(c, &in); e != nil {
		return e
	}
	key := c.Get("Idempotency-Key")
	if e := validateOrder(in, key); e != nil {
		return e
	}
	key = uuid.MustParse(key).String()
	body, _ := json.Marshal(in)
	hash := security.Hash(string(body))
	var out models.Order
	replay := false
	_, e := database.Transaction(c.Context(), s.DB, func(ctx context.Context) (any, error) {
		replay = false
		e := s.DB.Collection("orders").FindOne(ctx, bson.M{"idempotencyKey": key}).Decode(&out)
		if e == nil {
			if out.RequestHash != hash {
				return nil, fail(409, "idempotency_conflict", "Ключ уже использован для другого заказа")
			}
			replay = true
			return nil, nil
		}
		if !errors.Is(e, mongo.ErrNoDocuments) {
			return nil, e
		}
		t, e := s.table(ctx, in.TableToken)
		if e != nil {
			return nil, e
		}
		// Write the table in every order transaction: serialize session creation, closure and QR rotation.
		r, e := s.DB.Collection("tables").UpdateOne(ctx, bson.M{"_id": t.ID, "isActive": true, "tokenHash": security.Hash(in.TableToken)}, bson.M{"$set": bson.M{"status": "awaiting_confirmation", "updatedAt": time.Now()}})
		if e != nil {
			return nil, e
		}
		if r.MatchedCount != 1 {
			return nil, fail(409, "table_changed", "QR-код изменился")
		}
		items := []models.Item{}
		var total int64
		for _, i := range in.Items {
			id, _ := oid(i.ProductID)
			var p models.Product
			if e = s.DB.Collection("products").FindOne(ctx, bson.M{"_id": id, "isAvailable": true}).Decode(&p); e != nil {
				return nil, fail(422, "product_unavailable", "Одно из блюд недоступно. Обновите меню.")
			}
			n, e := s.DB.Collection("categories").CountDocuments(ctx, bson.M{"_id": p.CategoryID, "isActive": true})
			if e != nil {
				return nil, e
			}
			if n != 1 {
				return nil, fail(422, "product_unavailable", "Категория блюда недоступна")
			}
			subtotal := p.Price * int64(i.Quantity)
			total += subtotal
			items = append(items, models.Item{ProductID: p.ID, Name: p.Name, Price: p.Price, Quantity: i.Quantity, Subtotal: subtotal})
		}
		var session struct {
			ID bson.ObjectID `bson:"_id"`
		}
		e = s.DB.Collection("table_sessions").FindOne(ctx, bson.M{"tableId": t.ID, "status": "active"}).Decode(&session)
		if errors.Is(e, mongo.ErrNoDocuments) {
			session.ID = bson.NewObjectID()
			_, e = s.DB.Collection("table_sessions").InsertOne(ctx, bson.M{"_id": session.ID, "tableId": t.ID, "status": "active", "startedAt": time.Now()})
		}
		if e != nil {
			return nil, e
		}
		var counter struct {
			Value int64 `bson:"value"`
		}
		e = s.DB.Collection("counters").FindOneAndUpdate(ctx, bson.M{"_id": "orders"}, bson.M{"$inc": bson.M{"value": 1}}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&counter)
		if e != nil {
			return nil, e
		}
		out = models.Order{Base: models.NewBase(), PublicToken: security.Random(), OrderNumber: counter.Value, TableID: t.ID, TableNumber: t.Number, TableSessionID: session.ID, Items: items, TotalPrice: total, Status: "pending_confirmation", PaymentStatus: "unpaid", CustomerComment: in.CustomerComment, ExpiresAt: time.Now().UTC().Truncate(time.Millisecond).Add(time.Duration(s.Config.ConfirmationMinutes) * time.Minute), IdempotencyKey: key, RequestHash: hash}
		if _, e = s.DB.Collection("orders").InsertOne(ctx, out); e != nil {
			return nil, e
		}
		if _, e = s.DB.Collection("order_expirations").InsertOne(ctx, bson.M{"_id": out.ID, "expiresAt": out.ExpiresAt}); e != nil {
			return nil, e
		}
		return nil, s.audit(ctx, bson.NilObjectID, "order.created", "orders", out.ID, bson.M{"tableNumber": t.Number, "totalPrice": total})
	})
	if mongo.IsDuplicateKeyError(e) {
		e = s.DB.Collection("orders").FindOne(c.Context(), bson.M{"idempotencyKey": key}).Decode(&out)
		if e == nil {
			if out.RequestHash != hash {
				return fail(409, "idempotency_conflict", "Ключ уже использован для другого заказа")
			}
			replay = true
		}
	}
	if e != nil {
		return e
	}
	if !replay {
		s.orderEvent("order.created", out)
		s.orderEvent("order.pending_confirmation", out)
		return c.Status(201).JSON(out)
	}
	return c.JSON(out)
}
func (s *Server) publicOrder(c fiber.Ctx) error {
	var o models.Order
	if len(c.Params("publicToken")) != 43 {
		return mongo.ErrNoDocuments
	}
	e := s.DB.Collection("orders").FindOne(c.Context(), bson.M{"publicToken": c.Params("publicToken")}).Decode(&o)
	if e != nil {
		return e
	}
	return c.JSON(o)
}
func (s *Server) cancelOrder(c fiber.Ctx) error {
	var o models.Order
	if e := s.DB.Collection("orders").FindOne(c.Context(), bson.M{"publicToken": c.Params("publicToken")}).Decode(&o); e != nil {
		return e
	}
	out, e := s.transition(c.Context(), o.ID, "cancelled", bson.NilObjectID, 0, true, false, "")
	if e != nil {
		return e
	}
	return c.JSON(out)
}
func (s *Server) orders(c fiber.Ctx) error {
	filter := bson.M{}
	if st := c.Query("status"); st != "" {
		filter["status"] = st
	}
	v, e := list[models.Order](c.Context(), s.DB, "orders", filter, page(c))
	if e != nil {
		return e
	}
	return c.JSON(v)
}
func (s *Server) changeOrder(c fiber.Ctx, action string) error {
	var in struct {
		EstimatedMinutes  int    `json:"estimatedMinutes"`
		PresenceConfirmed bool   `json:"presenceConfirmed"`
		PaymentConfirmed  bool   `json:"paymentConfirmed"`
		Reason            string `json:"reason"`
	}
	if e := decode(c, &in); e != nil {
		return e
	}
	id, e := oid(c.Params("id"))
	if e != nil {
		return e
	}
	to := ""
	switch action {
	case "confirm", "accept":
		if !in.PresenceConfirmed || in.EstimatedMinutes < 1 || in.EstimatedMinutes > 180 {
			return bad("Подтвердите присутствие клиента и укажите время от 1 до 180 минут")
		}
		to = "accepted"
	case "reject":
		to = "cancelled"
	case "preparing":
		to = "preparing"
	case "ready":
		to = "ready"
	case "delivering":
		to = "delivering"
	case "deliver":
		to = "delivered"
	case "pay":
		if !in.PaymentConfirmed {
			return bad("Подтвердите получение оплаты")
		}
		to = "paid"
	}
	if utf8.RuneCountInString(in.Reason) > 500 {
		return bad("Причина слишком длинная")
	}
	out, e := s.transition(c.Context(), id, to, current(c).ID, in.EstimatedMinutes, false, false, in.Reason)
	if e != nil {
		return e
	}
	return c.JSON(out)
}
func (s *Server) transition(ctx context.Context, id bson.ObjectID, to string, actor bson.ObjectID, minutes int, public, expired bool, reason string) (models.Order, error) {
	var out models.Order
	_, e := database.Transaction(ctx, s.DB, func(ctx context.Context) (any, error) {
		var old models.Order
		if e := s.DB.Collection("orders").FindOne(ctx, bson.M{"_id": id}).Decode(&old); e != nil {
			return nil, e
		}
		if !models.CanTransition(old.Status, to) || (public && old.Status != "pending_confirmation") || (expired && (old.Status != "pending_confirmation" || old.ExpiresAt.After(time.Now()))) {
			return nil, fail(409, "invalid_transition", "Статус заказа уже изменился. Обновите список.")
		}
		if to == "accepted" && !old.ExpiresAt.After(time.Now()) {
			return nil, fail(409, "order_expired", "Время подтверждения истекло")
		}
		set := bson.M{"status": to, "updatedAt": time.Now().UTC()}
		if to == "accepted" {
			set["estimatedMinutes"] = minutes
			set["acceptedBy"] = actor
		}
		if to == "delivered" {
			set["deliveredBy"] = actor
		}
		if to == "paid" {
			set["paymentStatus"] = "paid"
		}
		e := s.DB.Collection("orders").FindOneAndUpdate(ctx, bson.M{"_id": id, "status": old.Status}, bson.M{"$set": set}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&out)
		if errors.Is(e, mongo.ErrNoDocuments) {
			return nil, fail(409, "conflict", "Заказ уже обработан")
		}
		if e != nil {
			return nil, e
		}
		if _, e = s.DB.Collection("tables").UpdateOne(ctx, bson.M{"_id": old.TableID}, bson.M{"$set": bson.M{"updatedAt": time.Now()}}); e != nil {
			return nil, e
		}
		active, e := list[models.Order](ctx, s.DB, "orders", bson.M{"tableId": old.TableID, "status": bson.M{"$nin": []string{"paid", "cancelled"}}})
		if e != nil {
			return nil, e
		}
		status := "free"
		if len(active) > 0 {
			status = "waiting_payment"
			for _, o := range active {
				if o.Status == "pending_confirmation" {
					status = "awaiting_confirmation"
					break
				}
				if o.Status != "delivered" {
					status = "occupied"
				}
			}
		} else {
			if _, e = s.DB.Collection("table_sessions").UpdateMany(ctx, bson.M{"tableId": old.TableID, "status": "active"}, bson.M{"$set": bson.M{"status": "closed", "closedAt": time.Now()}}); e != nil {
				return nil, e
			}
		}
		if _, e = s.DB.Collection("tables").UpdateOne(ctx, bson.M{"_id": old.TableID}, bson.M{"$set": bson.M{"status": status}}); e != nil {
			return nil, e
		}
		if _, e = s.DB.Collection("order_expirations").DeleteOne(ctx, bson.M{"_id": id}); e != nil {
			return nil, e
		}
		return nil, s.audit(ctx, actor, "order."+to, "orders", id, bson.M{"from": old.Status, "to": to, "expired": expired, "reason": reason})
	})
	if e == nil {
		s.orderEvent("order."+to, out)
	}
	return out, e
}
func (s *Server) Expire(ctx context.Context) error {
	orders, e := list[models.Order](ctx, s.DB, "orders", bson.M{"status": "pending_confirmation", "expiresAt": bson.M{"$lte": time.Now()}}, options.Find().SetLimit(100))
	if e != nil {
		return e
	}
	for _, o := range orders {
		_, e = s.transition(ctx, o.ID, "cancelled", bson.NilObjectID, 0, false, true, "Confirmation deadline expired")
		var p *problem
		if e != nil && !(errors.As(e, &p) && p.status == 409) {
			return e
		}
	}
	return nil
}
func (s *Server) RunJobs(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 20*time.Second)
		e := s.Expire(job)
		cancel()
		if e != nil && ctx.Err() == nil {
			slogJobError()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
