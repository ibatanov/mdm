# MDM Core (Go)

Минимальный стартовый каркас backend на Go для дальнейшей реализации MDM API.

## Что уже есть
- HTTP сервер на `:8080` (или `HTTP_PORT`).
- Проверки: `GET /healthz` (liveness), `GET /readyz` (readiness на основе конфигурации Postgres/Kafka).
- Автомиграции БД при старте сервиса.
- На текущем этапе используется единая SQL-миграция: `core/internal/migrations/0001_init.sql`.
- Базовый CRUD `dictionaries`:
- `POST /api/v1/dictionaries`
- `GET /api/v1/dictionaries`
- `GET /api/v1/dictionaries/{dictionary_id}`
- `PATCH /api/v1/dictionaries/{dictionary_id}`
- `DELETE /api/v1/dictionaries/{dictionary_id}`
- Базовый CRUD `attributes`:
- `POST /api/v1/attributes`
- `GET /api/v1/attributes`
- `GET /api/v1/attributes/{attribute_id}`
- `PATCH /api/v1/attributes/{attribute_id}`
- `DELETE /api/v1/attributes/{attribute_id}`
- Управление схемой справочника:
- `GET /api/v1/dictionaries/{dictionary_id}/schema`
- `PUT /api/v1/dictionaries/{dictionary_id}/schema`
- Базовый CRUD `entries`:
- `POST /api/v1/dictionaries/{dictionary_id}/entries`
- `GET /api/v1/dictionaries/{dictionary_id}/entries`
- `GET /api/v1/dictionaries/{dictionary_id}/entries/{entry_id}`
- `PATCH /api/v1/dictionaries/{dictionary_id}/entries/{entry_id}`
- `DELETE /api/v1/dictionaries/{dictionary_id}/entries/{entry_id}`
- Поиск по объектам:
- `POST /api/v1/dictionaries/{dictionary_id}/entries/search`
- Аудит:
- `GET /api/v1/audit/events`
- Контракт аутентификации через gateway заголовки:
- `X-User-Id: 100`
- `X-User-Role: mdm_admin,mdm_editor`
- Контракт трассировки запросов:
- `X-Request-Id: <uuid>` (опционален; при отсутствии/невалидном значении сервер сгенерирует UUID)

## Быстрый запуск
1. Поднять инфраструктуру:
```bash
docker compose up -d
```
Данные Docker сохраняются в каталоге проекта: `infra/volumes/postgres` и `infra/volumes/kafka`.
Kafka UI доступен по адресу: `http://localhost:8088`.
Через Kafka UI можно:
- смотреть топики, consumer groups и сообщения;
- создавать/удалять топики;
- менять параметры топиков (например `retention.ms`, `cleanup.policy`, число партиций).

2. Запустить сервис:
```bash
cd core
go run ./cmd/mdm
```

3. Пример запроса:
```bash
curl -X POST http://localhost:8080/api/v1/dictionaries \
  -H 'Content-Type: application/json' \
  -H 'X-User-Id: 100' \
  -H 'X-User-Role: mdm_admin,mdm_editor' \
  -d '{"code":"products","name":"Товары","description":"Единый каталог"}'
```

## Тесты
- Unit тесты (без БД):
```bash
make test
```
- Integration тесты (реальный Postgres в Docker):
```bash
make test-integration
```

`make test-integration` делает полный цикл:
- поднимает `docker-compose.test.yml`;
- запускает `go test ./...` в `core` с установленным `TEST_DATABASE_DSN`;
- всегда останавливает контейнеры и удаляет тестовые volumes (`down -v`).

Тестовые данные хранятся внутри проекта: `infra/volumes/postgres-test`.

### Защита от запуска на production БД
Integration тесты читают только `TEST_DATABASE_DSN`.
В коде есть fail-fast проверка:
- DSN должен быть в формате `postgres://` или `postgresql://`;
- хост только `localhost`, `127.0.0.1` или `postgres-test`;
- имя БД обязательно заканчивается на `_test`.

Можно переопределить DSN при запуске:
```bash
TEST_DATABASE_DSN='postgres://mdm_test:mdm_test@localhost:55432/mdm_test?sslmode=disable' make test-integration
```

## Переменные окружения
- `HTTP_PORT` (default: `8080`)
- `POSTGRES_DSN` (если не задан, собирается из параметров ниже)
- `POSTGRES_HOST` (default: `localhost`)
- `POSTGRES_PORT` (default: `5432`)
- `POSTGRES_DB` (default: `mdm`)
- `POSTGRES_USER` (default: `mdm`)
- `POSTGRES_PASSWORD` (default: `mdm`)
- `KAFKA_BROKERS` (default: `localhost:9092`)
