package server

import (
	"github.com/gofiber/fiber/v3"
	"strings"
)

func containsPath(path, param string) bool { return strings.Contains(path, "{"+param+"}") }

const docsHTML = `<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>TAMAK API</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.32.2/swagger-ui.css"><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist@5.32.2/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/openapi.json',dom_id:'#swagger-ui',persistAuthorization:false})</script></html>`

func (s *Server) openapi(c fiber.Ctx) error { return c.JSON(OpenAPI()) }
func OpenAPI() map[string]any {
	paths := map[string]any{}
	add := func(path, method, summary string, auth bool, props map[string]any, required ...string) {
		op := map[string]any{"summary": summary, "responses": map[string]any{"200": map[string]any{"description": "Успешный ответ"}, "400": map[string]any{"description": "Ошибка валидации"}, "401": map[string]any{"description": "Требуется вход"}, "403": map[string]any{"description": "Недостаточно прав"}, "409": map[string]any{"description": "Конфликт состояния"}}}
		if auth {
			op["security"] = []any{map[string]any{"bearerAuth": []string{}}}
		}
		params := []any{}
		for _, p := range []string{"id", "publicToken", "tableToken"} {
			if containsPath(path, p) {
				params = append(params, map[string]any{"name": p, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		if method == "get" {
			for _, name := range []string{"limit", "offset"} {
				params = append(params, map[string]any{"name": name, "in": "query", "schema": map[string]any{"type": "integer", "minimum": 0}})
			}
		}
		if path == "/api/public/orders" && method == "post" {
			params = append(params, map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": map[string]any{"type": "string", "format": "uuid"}})
			op["responses"].(map[string]any)["201"] = map[string]any{"description": "Заказ создан"}
		}
		op["parameters"] = params
		if props != nil {
			schema := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
			if len(required) > 0 {
				schema["required"] = required
			}
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
		}
		if paths[path] == nil {
			paths[path] = map[string]any{}
		}
		paths[path].(map[string]any)[method] = op
	}
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	boolean := map[string]any{"type": "boolean"}
	add("/api/auth/login", "post", "Вход сотрудника", false, map[string]any{"login": str, "password": str}, "login", "password")
	add("/api/auth/refresh", "post", "Ротация HttpOnly refresh cookie", false, map[string]any{})
	add("/api/auth/logout", "post", "Выход", false, map[string]any{})
	add("/api/auth/me", "get", "Текущий сотрудник", true, nil)
	add("/api/auth/password", "patch", "Изменение пароля и отзыв сессий", true, map[string]any{"currentPassword": str, "newPassword": str}, "currentPassword", "newPassword")
	for _, p := range []string{"categories", "products", "menu/{tableToken}", "orders/{publicToken}"} {
		add("/api/public/"+p, "get", "Публичный доступ", false, nil)
	}
	add("/api/public/orders", "post", "Создание заказа, цены рассчитываются сервером", false, map[string]any{"tableToken": str, "customerComment": str, "items": map[string]any{"type": "array", "minItems": 1, "maxItems": 50, "items": map[string]any{"type": "object", "properties": map[string]any{"productId": str, "quantity": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"productId", "quantity"}}}}, "tableToken", "items")
	add("/api/public/orders/{publicToken}/cancel", "post", "Отмена ожидающего заказа", false, map[string]any{})
	resources := map[string]map[string]any{"categories": {"name": str, "sortOrder": num, "isActive": boolean}, "products": {"name": str, "categoryId": str, "description": str, "imageUrl": str, "price": num, "preparationTime": num, "isAvailable": boolean}, "tables": {"number": num, "isActive": boolean}, "employees": {"name": str, "login": str, "password": str, "role": map[string]any{"type": "string", "enum": []string{"cashier"}}, "isActive": boolean}}
	for kind, props := range resources {
		add("/api/admin/"+kind, "get", "Список: admin", true, nil)
		add("/api/admin/"+kind, "post", "Создание: admin", true, props)
		add("/api/admin/"+kind+"/{id}", "patch", "Изменение: admin", true, props)
		add("/api/admin/"+kind+"/{id}", "delete", "Деактивация: admin", true, nil)
	}
	for _, p := range []string{"admin/dashboard", "admin/orders", "admin/audit-log", "cashier/orders", "cashier/tables"} {
		add("/api/"+p, "get", "Данные для соответствующей роли; admin имеет доступ", true, nil)
	}
	add("/api/admin/tables/{id}/qr", "post", "Ротация подписанного QR; старый код отзывается", true, map[string]any{})
	add("/api/admin/tables/{id}/qr", "get", "Просмотр сохранённого QR без перевыпуска: admin", true, nil)
	add("/api/admin/tables/{id}/qr/restore", "post", "Сохранить действующую старую ссылку без перевыпуска: admin", true, map[string]any{"url": str}, "url")
	for _, action := range []string{"confirm", "accept", "reject", "preparing", "ready", "delivering", "deliver", "pay"} {
		props := map[string]any{}
		required := []string{}
		if action == "confirm" || action == "accept" {
			props["estimatedMinutes"] = num
			props["presenceConfirmed"] = boolean
			required = []string{"estimatedMinutes", "presenceConfirmed"}
		}
		if action == "reject" {
			props["reason"] = str
		}
		if action == "pay" {
			props["paymentConfirmed"] = boolean
			required = []string{"paymentConfirmed"}
		}
		add("/api/cashier/orders/{id}/"+action, "post", "Переход статуса: cashier", true, props, required...)
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": "TAMAK Cafe API", "version": "1.0.0", "description": "KGS minor units. Lists return arrays; errors: {error,code}. WebSocket /ws: first frame {type:authenticate,accessToken} or {type:authenticate,publicToken}. See CONTRACT.md for response schemas."}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}}}}
}
