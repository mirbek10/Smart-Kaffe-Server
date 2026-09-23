//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"io"
	"koffe/api/internal/config"
	"koffe/api/internal/database"
	"koffe/api/internal/models"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRealMongoWorkflow(t *testing.T) {
	cfg, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	cfg.Database = "tamak_test_" + bson.NewObjectID().Hex()
	cfg.Env = "development"
	cfg.FrontendURL = "http://localhost:5173"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, e := database.Open(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Client().Disconnect(context.Background())
	defer db.Drop(context.Background())
	if e = database.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	if e = database.Seed(ctx, db); e != nil {
		t.Fatal(e)
	}
	s := New(db, cfg)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	request := func(method, path, token string, body any, headers map[string]string) (int, []byte, http.Header) {
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, reader)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Error(e)
			return 0, nil, nil
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b, r.Header
	}
	must := func(expected int, method, path, token string, body any) []byte {
		status, b, _ := request(method, path, token, body, nil)
		if status != expected {
			t.Fatalf("%s %s: got %d wanted %d: %s", method, path, status, expected, b)
		}
		return b
	}
	login := func(role string) (string, http.Header) {
		status, b, h := request("POST", "/api/auth/login", "", map[string]any{"login": role, "password": "ChangeMe123!"}, nil)
		if status != 200 {
			t.Fatalf("login %d: %s", status, b)
		}
		var v struct {
			AccessToken string `json:"accessToken"`
		}
		_ = json.Unmarshal(b, &v)
		return v.AccessToken, h
	}
	admin, _ := login("admin")
	cashier, ch := login("cashier")
	must(403, "GET", "/api/admin/employees", cashier, nil)
	must(401, "GET", "/api/admin/tables", "", nil)
	var tables []models.Table
	_ = json.Unmarshal(must(200, "GET", "/api/admin/tables", admin, nil), &tables)
	var qr struct {
		TableToken string `json:"tableToken"`
	}
	_ = json.Unmarshal(must(200, "POST", "/api/admin/tables/"+tables[0].ID.Hex()+"/qr", admin, map[string]any{}), &qr)
	qrPath := "/api/admin/tables/" + tables[0].ID.Hex() + "/qr"
	var savedQR struct {
		TableToken string `json:"tableToken"`
		URL        string `json:"url"`
	}
	_ = json.Unmarshal(must(200, "GET", qrPath, admin, nil), &savedQR)
	if savedQR.TableToken != qr.TableToken {
		t.Fatal("viewing QR rotated its token")
	}
	must(403, "GET", qrPath, cashier, nil)
	must(401, "GET", qrPath, "", nil)
	// Legacy tables keep only the hash. Restore a known valid link without rotating it.
	if _, e = db.Collection("tables").UpdateOne(ctx, bson.M{"_id": tables[0].ID}, bson.M{"$unset": bson.M{"encryptedQR": ""}}); e != nil {
		t.Fatal(e)
	}
	must(404, "GET", qrPath, admin, nil)
	must(200, "POST", qrPath+"/restore", admin, map[string]any{"url": savedQR.URL})
	var restoredQR struct {
		TableToken string `json:"tableToken"`
		URL        string `json:"url"`
	}
	_ = json.Unmarshal(must(200, "GET", qrPath, admin, nil), &restoredQR)
	if restoredQR != savedQR {
		t.Fatal("restoring changed QR")
	}
	must(200, "GET", "/api/public/menu/"+qr.TableToken, "", nil)
	must(404, "GET", "/api/public/menu/"+qr.TableToken+"x", "", nil)
	var products []models.Product
	_ = json.Unmarshal(must(200, "GET", "/api/public/products", "", nil), &products)
	payload := map[string]any{"tableToken": qr.TableToken, "items": []any{map[string]any{"productId": products[0].ID.Hex(), "quantity": 2}}, "customerComment": "Без лука"}
	key := uuid.NewString()
	status, b, _ := request("POST", "/api/public/orders", "", payload, map[string]string{"Idempotency-Key": key})
	if status != 201 {
		t.Fatalf("create %d %s", status, b)
	}
	var order models.Order
	_ = json.Unmarshal(b, &order)
	if order.TotalPrice != products[0].Price*2 || order.Status != "pending_confirmation" {
		t.Fatal("server pricing or confirmation failed")
	}
	status, replayed, _ := request("POST", "/api/public/orders", "", payload, map[string]string{"Idempotency-Key": key})
	if status != 200 || !bytes.Equal(b, replayed) {
		t.Fatal("idempotency replay failed")
	}
	payload["customerComment"] = "different"
	status, _, _ = request("POST", "/api/public/orders", "", payload, map[string]string{"Idempotency-Key": key})
	if status != 409 {
		t.Fatal("changed replay accepted")
	}
	// Public WebSocket receives only the order identified by its capability.
	wsURL := "ws" + srv.URL[4:] + "/ws"
	ws, _, e := websocket.Dial(ctx, wsURL, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	if e = wsjson.Write(ctx, ws, map[string]any{"type": "authenticate", "publicToken": order.PublicToken}); e != nil {
		t.Fatal(e)
	}
	var event Event
	if e = wsjson.Read(ctx, ws, &event); e != nil || event.Type != "connected" {
		t.Fatal("WS authentication failed")
	}
	s.Hub.Publish(Event{Type: "order.ready", PublicToken: "another-order-capability", Status: "ready", Data: map[string]any{"orderNumber": 999}})
	must(409, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/ready", cashier, map[string]any{})
	must(400, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/confirm", cashier, map[string]any{"estimatedMinutes": 15})
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _, _ := request("POST", "/api/cashier/orders/"+order.ID.Hex()+"/confirm", cashier, map[string]any{"estimatedMinutes": 15, "presenceConfirmed": true}, nil)
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	accepted, conflicts := 0, 0
	for code := range codes {
		if code == 200 {
			accepted++
		}
		if code == 409 {
			conflicts++
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("concurrent acceptance: %d accepted %d conflicts", accepted, conflicts)
	}
	readCtx, done := context.WithTimeout(ctx, 5*time.Second)
	if e = wsjson.Read(readCtx, ws, &event); e != nil || event.Type != "order.accepted" {
		t.Fatal("missing accepted event", e)
	}
	done()
	must(409, "POST", "/api/public/orders/"+order.PublicToken+"/cancel", "", map[string]any{})
	for _, action := range []string{"preparing", "ready", "delivering"} {
		must(200, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/"+action, cashier, map[string]any{})
	}
	must(403, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/deliver", admin, map[string]any{})
	must(409, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/pay", cashier, map[string]any{"paymentConfirmed": true})
	must(200, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/deliver", cashier, map[string]any{})
	must(400, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/pay", cashier, map[string]any{})
	must(200, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/pay", cashier, map[string]any{"paymentConfirmed": true})
	must(409, "POST", "/api/cashier/orders/"+order.ID.Hex()+"/pay", cashier, map[string]any{"paymentConfirmed": true})
	var table models.Table
	_ = db.Collection("tables").FindOne(ctx, bson.M{"_id": order.TableID}).Decode(&table)
	if table.Status != "free" {
		t.Fatal("table not released")
	}
	var dashboard map[string]any
	_ = json.Unmarshal(must(200, "GET", "/api/admin/dashboard", admin, nil), &dashboard)
	if dashboard["revenueToday"] != float64(order.TotalPrice) {
		t.Fatal("dashboard revenue incorrect")
	}
	payload["customerComment"] = "expire"
	status, b, _ = request("POST", "/api/public/orders", "", payload, map[string]string{"Idempotency-Key": uuid.NewString()})
	if status != 201 {
		t.Fatal("second order", status, string(b))
	}
	var exp models.Order
	_ = json.Unmarshal(b, &exp)
	_, e = db.Collection("orders").UpdateOne(ctx, bson.M{"_id": exp.ID}, bson.M{"$set": bson.M{"expiresAt": time.Now().Add(-time.Minute)}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Expire(ctx); e != nil {
		t.Fatal(e)
	}
	_ = db.Collection("orders").FindOne(ctx, bson.M{"_id": exp.ID}).Decode(&exp)
	if exp.Status != "cancelled" {
		t.Fatal("expiration did not preserve cancelled history")
	}
	must(200, "POST", "/api/admin/tables/"+tables[0].ID.Hex()+"/qr", admin, map[string]any{})
	must(409, "POST", qrPath+"/restore", admin, map[string]any{"url": savedQR.URL})
	must(404, "GET", "/api/public/menu/"+qr.TableToken, "", nil)
	// Refresh rotation is single-use; a blocked employee loses existing access immediately.
	cookies := (&http.Response{Header: ch}).Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatal("missing HttpOnly refresh cookie")
	}
	headers := map[string]string{"Cookie": cookies[0].Name + "=" + cookies[0].Value}
	status, _, _ = request("POST", "/api/auth/refresh", "", map[string]any{}, headers)
	if status != 200 {
		t.Fatal("refresh failed")
	}
	status, _, _ = request("POST", "/api/auth/refresh", "", map[string]any{}, headers)
	if status != 401 {
		t.Fatal("refresh reuse accepted")
	}
	var employees []models.User
	_ = json.Unmarshal(must(200, "GET", "/api/admin/employees", admin, nil), &employees)
	for _, u := range employees {
		if u.Role == "cashier" {
			must(200, "DELETE", "/api/admin/employees/"+u.ID.Hex(), admin, nil)
		}
	}
	must(401, "GET", "/api/cashier/orders", cashier, nil)
	t.Log(fmt.Sprintf("Real MongoDB transaction workflow passed (%s)", cfg.Database))
}
