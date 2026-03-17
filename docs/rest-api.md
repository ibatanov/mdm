# Черновик REST API

Базовый путь: `/api/v1`

## Общие правила
- Формат времени: ISO 8601 (UTC).
- Все ответы содержат `request_id`.
- Для списков используется пагинация.
- MDM backend работает за доверенным gateway, интегрированным с Keycloak.
- Аутентификация выполняется на gateway; в MDM передаются пользовательские заголовки.
- Авторизация: доступ проверяется по ролям (RBAC) на каждую операцию.

### Заголовки аутентификации от gateway
- `X-User-Id` — id пользователя, например `X-User-Id: 100`.
- `X-User-Role` — роли через запятую, например `X-User-Role: mdm_admin,mdm_editor`.

### Трассировка запроса
- `X-Request-Id` обязателен для всех запросов к `/api/v1/*`.
- Значение `X-Request-Id` должно быть валидным UUID.
- При отсутствии/невалидном значении возвращается `400 invalid_request`.

### Роли (MVP)
- `mdm_admin` — полный административный доступ, включая управление ролями.
- `mdm_editor` — изменение справочников, атрибутов, схемы и объектов.
- `mdm_viewer` — чтение справочников, атрибутов, схемы и объектов.
- `mdm_auditor` — чтение аудита.

### Формат успешного ответа
```json
{
  "request_id": "uuid",
  "data": { }
}
```

### Формат ошибки
```json
{
  "request_id": "uuid",
  "error": {
    "code": "validation_failed",
    "message": "Attribute price is required",
    "details": {
      "field": "price"
    }
  }
}
```

### Коды ошибок
- `unauthorized` (401)
- `forbidden` (403)
- `invalid_request` (400)
- `not_found` (404)
- `conflict` (409)
- `validation_failed` (422)
- `internal_error` (500)

Типовые сценарии:
- `409 conflict` — нарушение уникальности (`code`, `external_key`) и другие конкурентные конфликты.
- `422 validation_failed` — данные не проходят бизнес-валидацию.
- `500 internal_error` — необработанная серверная ошибка.

### Пагинация
Для списков используется `limit` и `offset`.
Поля ответа: `items`, `total`, `limit`, `offset`.

Пример:
```
GET /dictionaries?limit=50&offset=0
```

Ответ:
```json
{
  "request_id": "uuid",
  "data": {
    "items": [],
    "total": 1000,
    "limit": 50,
    "offset": 0
  }
}
```

## Справочники
- `POST /dictionaries`
- `GET /dictionaries`
- `GET /dictionaries/{dictionary_id}`
- `PATCH /dictionaries/{dictionary_id}`
- `DELETE /dictionaries/{dictionary_id}`

Доступ:
- чтение: `mdm_viewer`, `mdm_editor`, `mdm_admin`
- изменение: `mdm_editor`, `mdm_admin`

Пример запроса (создание):
```json
{
  "code": "products",
  "name": "Товары",
  "description": "Единый каталог товаров"
}
```

## Атрибуты
- `POST /attributes`
- `GET /attributes`
- `GET /attributes/{attribute_id}`
- `PATCH /attributes/{attribute_id}`
- `DELETE /attributes/{attribute_id}`

Доступ:
- чтение: `mdm_viewer`, `mdm_editor`, `mdm_admin`
- изменение: `mdm_editor`, `mdm_admin`

Пример запроса (создание):
```json
{
  "code": "brand",
  "name": "Бренд",
  "data_type": "string",
  "description": "Название бренда"
}
```

Дополнительно:
- `data_type` поддерживает значения: `string`, `number`, `date`, `boolean`, `enum`, `reference`.
- Для `data_type=reference` поле `ref_dictionary_id` обязательно и должно быть UUID.

## Схема справочника (разрешенные атрибуты)
- `PUT /dictionaries/{dictionary_id}/schema`
- `GET /dictionaries/{dictionary_id}/schema`

Доступ:
- чтение: `mdm_viewer`, `mdm_editor`, `mdm_admin`
- изменение: `mdm_editor`, `mdm_admin`

Пример обновления схемы:
```json
{
  "attributes": [
    {
      "attribute_id": "uuid",
      "required": true,
      "default_value": "Неизвестно",
      "validators": {
        "min_length": 2,
        "max_length": 128
      },
      "is_unique": false,
      "is_multivalue": false,
      "position": 10
    }
  ]
}
```

Примечание:
- Поле `attributes` в `PUT /dictionaries/{dictionary_id}/schema` является обязательным (может быть пустым массивом для очистки схемы).

## Объекты справочника
- `POST /dictionaries/{dictionary_id}/entries`
- `GET /dictionaries/{dictionary_id}/entries`
- `GET /dictionaries/{dictionary_id}/entries/{entry_id}`
- `PATCH /dictionaries/{dictionary_id}/entries/{entry_id}`
- `DELETE /dictionaries/{dictionary_id}/entries/{entry_id}`

Доступ:
- чтение: `mdm_viewer`, `mdm_editor`, `mdm_admin`
- изменение: `mdm_editor`, `mdm_admin`

Пример создания объекта:
```json
{
  "external_key": "SKU-001",
  "data": {
    "brand": "Acme",
    "price": 99.9,
    "release_date": "2025-05-01",
    "category_ref": "uuid-of-category-entry"
  }
}
```

## Поиск и фильтрация
Для динамических фильтров по атрибутам используется отдельный endpoint:

- `POST /dictionaries/{dictionary_id}/entries/search`

Пример запроса:
```json
{
  "filters": [
    { "attribute": "brand", "op": "eq", "value": "Acme" },
    { "attribute": "price", "op": "gte", "value": 50 },
    { "attribute": "price", "op": "lte", "value": 200 }
  ],
  "sort": [
    { "attribute": "price", "direction": "asc" }
  ],
  "page": { "limit": 50, "offset": 0 }
}
```

Поддерживаемые операторы (MVP):
- `eq`, `ne`
- `lt`, `lte`, `gt`, `gte`
- `in`
- `contains`, `prefix`
- `range`

## Аудит
- `GET /audit/events`

Доступ:
- `mdm_auditor`, `mdm_admin`

Фильтры:
- `entity_type`, `entity_id`
- `actor_user_id`
- `occurred_from`, `occurred_to`
- пагинация `limit`, `offset`

Пример ответа:
```json
{
  "request_id": "uuid",
  "data": {
    "items": [
      {
        "event_id": "uuid",
        "request_id": "uuid",
        "actor_user_id": "uuid",
        "action": "entry.updated",
        "entity_type": "entry",
        "entity_id": "uuid",
        "occurred_at": "2026-03-17T12:00:00Z",
        "before_state": { "price": 99.9 },
        "after_state": { "price": 89.9 },
        "metadata": { "source_ip": "10.0.0.10" }
      }
    ],
    "total": 1,
    "limit": 50,
    "offset": 0
  }
}
```

## Управление ролями (админ)
- `GET /users/{user_id}/roles`
- `PUT /users/{user_id}/roles`

Доступ:
- `mdm_admin`

Пример обновления ролей:
```json
{
  "role_codes": ["mdm_editor", "mdm_auditor"]
}
```

## События (внутренние)
Предложение по топикам Kafka:
- `mdm.dictionary.events`
- `mdm.attribute.events`
- `mdm.entry.events`

Пример события:
```json
{
  "event_id": "uuid",
  "event_type": "entry.updated",
  "dictionary_id": "uuid",
  "entry_id": "uuid",
  "payload": { "changed": ["price"] },
  "occurred_at": "2026-03-17T12:00:00Z"
}
```
