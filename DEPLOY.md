# Production deployment

Backend и frontend публикуются как два независимых сервиса. Backend должен видеть MongoDB Atlas (или MongoDB replica set), а frontend обращается к нему через `/api` и `/ws`.

## Переменные окружения

В production задайте в настройках сервиса:

```dotenv
APP_ENV=production
APP_PORT=8080
MONGODB_URI=mongodb+srv://<user>:<password>@<cluster>/?retryWrites=true&w=majority
MONGODB_DATABASE=cafe_mvp
FRONTEND_URL=https://menu.example.com
JWT_SECRET=<random string, 32+ characters>
JWT_REFRESH_SECRET=<another random string, 32+ characters>
TABLE_TOKEN_SECRET=<another random string, 32+ characters>
ORDER_CONFIRMATION_TIMEOUT_MINUTES=5
ADMIN_EMAIL=admin@example.com
ADMIN_NAME=Administrator
ADMIN_PASSWORD=<long unique password>
```

На Render, Railway и похожих платформах можно не задавать `APP_PORT`: приложение использует автоматически переданный `PORT`. Не коммитьте `.env`; шаблон находится в `.env.example`. В Atlas добавьте исходящие IP-адреса сервиса в Network Access.

## Docker

```sh
docker build -t tamak-api .
docker run --rm --env-file .env -p 8080:8080 tamak-api
```

Для VPS с reverse proxy:

```sh
docker compose up --build -d
```

Проверьте `GET /healthz` (процесс) и `GET /readyz` (процесс + MongoDB). OpenAPI доступен по `/openapi.json`, интерфейс Swagger — `/swagger` или `/docs`. Прокси должен передавать WebSocket `/ws` и заголовки `X-Forwarded-Proto`, `X-Forwarded-For`.

## Первый администратор

Однократно выполните в том же production окружении:

```sh
go run ./cmd/admin
```

Команда использует `ADMIN_EMAIL`, `ADMIN_NAME` и `ADMIN_PASSWORD`, не перезаписывает существующего администратора и не должна запускаться на каждом деплое. `go run ./cmd/seed` в production заблокирован.

