package models

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"time"
)

type Base struct {
	ID        bson.ObjectID `bson:"_id" json:"id"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt" json:"updatedAt"`
}

func NewBase() Base {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return Base{bson.NewObjectID(), now, now}
}

type User struct {
	Base         `bson:",inline"`
	Name         string `bson:"name" json:"name"`
	Login        string `bson:"login" json:"login"`
	PasswordHash string `bson:"passwordHash" json:"-"`
	Role         string `bson:"role" json:"role"`
	IsActive     bool   `bson:"isActive" json:"isActive"`
	Version      int    `bson:"version" json:"-"`
}
type Category struct {
	Base      `bson:",inline"`
	Name      string `bson:"name" json:"name"`
	SortOrder int    `bson:"sortOrder" json:"sortOrder"`
	IsActive  bool   `bson:"isActive" json:"isActive"`
}
type Product struct {
	Base            `bson:",inline"`
	CategoryID      bson.ObjectID `bson:"categoryId" json:"categoryId"`
	Name            string        `bson:"name" json:"name"`
	Description     string        `bson:"description" json:"description"`
	ImageURL        string        `bson:"imageUrl" json:"imageUrl"`
	Price           int64         `bson:"price" json:"price"`
	PreparationTime int           `bson:"preparationTime" json:"preparationTime"`
	IsAvailable     bool          `bson:"isAvailable" json:"isAvailable"`
}
type Table struct {
	Base        `bson:",inline"`
	Number      int    `bson:"number" json:"number"`
	TokenHash   string `bson:"tokenHash,omitempty" json:"-"`
	EncryptedQR string `bson:"encryptedQR,omitempty" json:"-"`
	IsActive    bool   `bson:"isActive" json:"isActive"`
	Status      string `bson:"status" json:"status"`
}
type Item struct {
	ProductID bson.ObjectID `bson:"productId" json:"productId"`
	Name      string        `bson:"productNameSnapshot" json:"productNameSnapshot"`
	Price     int64         `bson:"priceSnapshot" json:"priceSnapshot"`
	Quantity  int           `bson:"quantity" json:"quantity"`
	Subtotal  int64         `bson:"subtotal" json:"subtotal"`
}
type Order struct {
	Base             `bson:",inline"`
	PublicToken      string        `bson:"publicToken" json:"publicToken"`
	OrderNumber      int64         `bson:"orderNumber" json:"orderNumber"`
	TableID          bson.ObjectID `bson:"tableId" json:"tableId"`
	TableNumber      int           `bson:"tableNumber" json:"tableNumber"`
	TableSessionID   bson.ObjectID `bson:"tableSessionId" json:"tableSessionId"`
	Items            []Item        `bson:"items" json:"items"`
	TotalPrice       int64         `bson:"totalPrice" json:"totalPrice"`
	Status           string        `bson:"status" json:"status"`
	EstimatedMinutes int           `bson:"estimatedMinutes" json:"estimatedMinutes"`
	CustomerComment  string        `bson:"customerComment" json:"customerComment"`
	PaymentStatus    string        `bson:"paymentStatus" json:"paymentStatus"`
	ExpiresAt        time.Time     `bson:"expiresAt" json:"expiresAt"`
	AcceptedBy       bson.ObjectID `bson:"acceptedBy,omitempty" json:"-"`
	DeliveredBy      bson.ObjectID `bson:"deliveredBy,omitempty" json:"-"`
	IdempotencyKey   string        `bson:"idempotencyKey" json:"-"`
	RequestHash      string        `bson:"requestHash" json:"-"`
}
type Audit struct {
	ID        bson.ObjectID `bson:"_id" json:"id"`
	UserID    bson.ObjectID `bson:"userId" json:"userId"`
	Action    string        `bson:"action" json:"action"`
	Entity    string        `bson:"entity" json:"entity"`
	EntityID  bson.ObjectID `bson:"entityId" json:"entityId"`
	Metadata  bson.M        `bson:"metadata" json:"metadata"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
}

func CanTransition(from, to string) bool {
	switch from {
	case "pending_confirmation":
		return to == "accepted" || to == "cancelled"
	case "accepted":
		return to == "preparing" || to == "cancelled"
	case "preparing":
		return to == "ready"
	case "ready":
		return to == "delivering" || to == "delivered"
	case "delivering":
		return to == "delivered"
	case "delivered":
		return to == "paid"
	}
	return false
}
