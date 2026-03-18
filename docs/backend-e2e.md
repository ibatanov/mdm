# Backend E2E Статус

Дата последнего полного прогона: `2026-03-17` (MSK)  
Доменный сценарий: автозапчасти (справочники брендов и номенклатуры запчастей)

## Покрытие тестирования
- Типы данных: `string`, `number`, `date`, `boolean`, `enum`, `reference`, multivalue (`is_multivalue=true`).
- CRUD dictionaries: create/list/get/update/delete.
- CRUD attributes: create/list/get/update/delete.
- CRUD dictionary schema: get/put.
- CRUD entries: create/list/get/update/delete.
- Поиск: `eq`, `ne`, `contains`, `prefix`, `in`, `lt`, `lte`, `gt`, `gte`, `range`.
- Поиск: сортировка и пагинация.
- Edge-cases: required-поля, type mismatch, enum violations, invalid/missing references.
- Edge-cases: unique-ограничения, неизвестные атрибуты, unsupported search operations.

## Итог прогона
- Всего шагов: `83`.
- Критических падений API-контракта: `0` (основной контракт работает).
- Выявлено аномалий: `2`.

## Зафиксированные аномалии
### 1. Сортировка числовых значений в search
- Шаг: `Поиск с сортировкой price DESC`.
- Ожидалось: `CLT-004,BRK-001,SHK-003,OIL-002`.
- Фактически: `SHK-003,OIL-002,CLT-004,BRK-001`.
- Комментарий: похоже на лексикографическую сортировку вместо числовой.

### 2. Ошибка audit-логирования при удалении справочника
- Шаг: `Удаление временного справочника`.
- HTTP-ответ: `204 No Content` (операция успешна).
- В серверном логе: `failed to write audit event ... violates foreign key constraint "audit_events_dictionary_fk"`.
- Комментарий: удаление данных выполняется, но есть техническая ошибка записи в аудит.

## Рекомендации по исправлению
1. Для сортировки по числовым атрибутам использовать приведение типа (`numeric`) или типизированную сортировку на основе метаданных схемы.
2. Для `dictionary.deleted` писать audit-событие до физического удаления справочника, либо ослабить FK-связь для `audit_events.dictionary_id` (например, `ON DELETE SET NULL`), если это допускается моделью аудита.

## Артефакты последнего прогона
- `.cache/e2e_auto_parts_full_report.md`
- `.cache/e2e_auto_parts_steps.json`
- `.cache/e2e_auto_parts_anomalies.json`
