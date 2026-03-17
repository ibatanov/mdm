# MDM UI (`ui`)

Веб-интерфейс для MDM backend, реализован на `Vue 3 + TypeScript + Vite`.

## Быстрый старт

```bash
cd ui
cp .env.example .env
npm install
npm run dev
```

UI по умолчанию стартует на `http://localhost:5173`.

## Скрипты

```bash
npm run dev        # локальная разработка
npm run typecheck  # проверка TypeScript
npm run build      # production-сборка
npm run preview    # просмотр собранного бандла
```

## Переменные окружения

- `VITE_API_BASE_URL` — базовый путь MDM API (по умолчанию `/api/v1`).
- `VITE_ROOT_API_BASE_URL` — базовый путь для root-эндпоинтов (`/healthz`, `/readyz`), обычно пустой.
- `VITE_DEV_USER_ID` — mock `X-User-Id` для dev.
- `VITE_DEV_USER_ROLES` — mock `X-User-Role` для dev, через запятую.

## Dev-эмуляция пользователя/ролей

До подключения gateway+Keycloak в development режиме UI добавляет заголовки:

- `X-User-Id`
- `X-User-Role`

Есть preset-профили:

- `admin`: `100 / mdm_admin`
- `editor`: `101 / mdm_editor`
- `viewer`: `102 / mdm_viewer`
- `auditor`: `103 / mdm_auditor`

Или можно выбрать `custom` профиль в верхней панели.

В `production` эмуляция отключена.

## Покрытие API (MVP)

- `/healthz`, `/readyz`
- `/api/v1/dictionaries` (CRUD)
- `/api/v1/attributes` (CRUD)
- `/api/v1/dictionaries/{dictionary_id}/schema` (GET/PUT)
- `/api/v1/dictionaries/{dictionary_id}/entries` (CRUD + search)
- `/api/v1/audit/events`

## Разделы UI

- `Справочники` — список + карточка справочника с настройкой схемы атрибутов.
- `Атрибуты` — каталог атрибутов и в каких справочниках они участвуют.
- `Объекты` — CRUD по выбранному справочнику и поиск по всем объектам.
- `Аудит` — журнал событий.
- `Health` — `/healthz` и `/readyz`.

## Структура

- `src/lib` — API клиент, обработка ошибок, RBAC, dev identity.
- `src/stores` — Pinia store для dev identity.
- `src/layout` — оболочка приложения.
- `src/pages` — страницы разделов.
- `src/components` — переиспользуемые UI-компоненты.
