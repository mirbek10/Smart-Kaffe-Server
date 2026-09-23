# TAMAK backend

Самостоятельный Go-проект. Содержимое этой папки можно перенести в отдельный GitHub-репозиторий целиком. Frontend, Node.js и родительские файлы для работы API не нужны. MongoDB должна поддерживать транзакции: Atlas либо replica set.

## Запуск

Нужен Go 1.26.5+. На новом компьютере скопируйте `.env.example` в `.env` и заполните `MONGODB_URI`, `MONGODB_DATABASE`, `FRONTEND_URL`. В текущей рабочей копии `.env` и `.env.local` уже перенесены с прежними настройками и секретами.

```sh
go mod download
go run ./cmd/server
```

Или, если установлен npm: `npm run dev`. Команда `npm run api:dev` также поддерживается. `npm install` backend не требуется.

API: http://localhost:8080. Swagger: http://localhost:8080/swagger — ссылка выводится при запуске. Проверка базы: `/readyz`, OpenAPI: `/openapi.json`.

Конфигурация читается только из этого проекта: окружение процесса → `.env.local` → `.env`. Родительский `.env` не используется. В development недостающие ключи генерируются в `.env.local`. В production обязательны независимые `JWT_SECRET`, `JWT_REFRESH_SECRET`, `TABLE_TOKEN_SECRET` длиной не менее 32 символов.

## Администратор и роли

Для создания первого администратора заполните `ADMIN_EMAIL`, `ADMIN_NAME`, `ADMIN_PASSWORD` в `.env`, затем выполните:

```sh
go run ./cmd/admin
```

Email вводится в поле «Логин». Команда не заменяет существующего администратора. Для тестового наполнения пустой development-базы: `go run ./cmd/seed`. Seed создаёт admin/cashier с паролем `ChangeMe123!`; смените начальные пароли после первого входа. В production seed запрещён.

Есть гость без учётной записи, администратор и кассир. Только кассир изменяет статусы заказа, включая подачу и оплату. Администратор управляет меню, столами и кассирами, смотрит заказы, статистику и аудит. Роли официанта и кухни отсутствуют.

Подача: `ready → delivering → delivered → paid`. `POST /api/cashier/orders/:id/delivering` означает «Несут к столу», `/deliver` — «Подан». Прямой переход `ready → delivered` сохранён для совместимости. Изменения передаются гостю через WebSocket, без имитации статуса по таймеру.

Заказы создаются по подписанному QR и всегда требуют подтверждения присутствия гостя. Сервер считает цены, защищает отправку UUID-ключом и использует транзакции. Неподтверждённые заказы отменяются через 5 минут, история сохраняется. Данные и события API описаны в [CONTRACT.md](CONTRACT.md).

## Подключение отдельного frontend

Укажите точный origin сайта в `FRONTEND_URL`, например `http://localhost:5173`. На frontend настройте proxy на адрес этого API. REST использует `/api`, WebSocket — `/ws`. В production рекомендуется общий публичный origin через reverse proxy; проекты при этом собираются и размещаются отдельно. Секреты MongoDB/JWT нельзя передавать frontend.

## Проверки и Docker

```sh
go test ./...
go vet ./...
go test -tags=integration -count=1 ./internal/server
go build -o bin/ ./cmd/server
docker compose up --build -d
```

Интеграционные тесты требуют доступную MongoDB, создают отдельную `tamak_test_<random>` и удаляют только её. Compose запускает только API, подключаясь к базе из `.env`; локальная MongoDB в него не включена. Docker требует заполненные секреты в `.env`/`.env.local`. Docker-сборка в текущей среде не проверена.

`.env`, `.env.local`, `bin/` и логи исключены из Git. В репозиторий отправляйте только `.env.example`. Доступ Atlas по IP настраивается в Atlas Network Access, а не переменной `IP` в файле.
