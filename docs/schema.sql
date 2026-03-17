-- Postgres DDL для MVP

-- Расширения
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Пользователи (акторы API)
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  external_id TEXT NOT NULL UNIQUE,
  email TEXT,
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Роли доступа (RBAC)
CREATE TABLE IF NOT EXISTS roles (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Привязка ролей к пользователям
CREATE TABLE IF NOT EXISTS user_roles (
  user_id UUID NOT NULL,
  role_id UUID NOT NULL,
  granted_by UUID,
  granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, role_id),
  CONSTRAINT user_roles_user_fk
    FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT user_roles_role_fk
    FOREIGN KEY (role_id) REFERENCES roles (id),
  CONSTRAINT user_roles_granted_by_fk
    FOREIGN KEY (granted_by) REFERENCES users (id)
);

CREATE INDEX IF NOT EXISTS user_roles_role_idx
  ON user_roles (role_id);

-- Справочники
CREATE TABLE IF NOT EXISTS dictionaries (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT,
  schema_version INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Атрибуты
CREATE TABLE IF NOT EXISTS attributes (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT,
  data_type TEXT NOT NULL,
  ref_dictionary_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT attributes_data_type_chk
    CHECK (data_type IN (
      'string','number','date','boolean','enum','reference'
    )),
  CONSTRAINT attributes_ref_dictionary_fk
    FOREIGN KEY (ref_dictionary_id) REFERENCES dictionaries (id)
);

-- Схема справочника
CREATE TABLE IF NOT EXISTS dictionary_attributes (
  dictionary_id UUID NOT NULL,
  attribute_id UUID NOT NULL,
  required BOOLEAN NOT NULL DEFAULT false,
  default_value JSONB,
  validators JSONB,
  is_unique BOOLEAN NOT NULL DEFAULT false,
  is_multivalue BOOLEAN NOT NULL DEFAULT false,
  position INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (dictionary_id, attribute_id),
  CONSTRAINT dictionary_attributes_dictionary_fk
    FOREIGN KEY (dictionary_id) REFERENCES dictionaries (id),
  CONSTRAINT dictionary_attributes_attribute_fk
    FOREIGN KEY (attribute_id) REFERENCES attributes (id)
);

-- Объекты справочника
CREATE TABLE IF NOT EXISTS entries (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  dictionary_id UUID NOT NULL,
  external_key TEXT,
  data JSONB NOT NULL,
  version INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT entries_dictionary_fk
    FOREIGN KEY (dictionary_id) REFERENCES dictionaries (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS entries_dictionary_external_key_uq
  ON entries (dictionary_id, external_key)
  WHERE external_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS entries_dictionary_idx
  ON entries (dictionary_id);

CREATE INDEX IF NOT EXISTS entries_data_gin_idx
  ON entries USING GIN (data);

-- Нормализованные значения атрибутов для динамических фильтров
CREATE TABLE IF NOT EXISTS entry_values (
  entry_id UUID NOT NULL,
  dictionary_id UUID NOT NULL,
  attribute_id UUID NOT NULL,
  value_text TEXT,
  value_num NUMERIC,
  value_date DATE,
  value_bool BOOLEAN,
  value_ref UUID,
  value_json JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT entry_values_entry_fk
    FOREIGN KEY (entry_id) REFERENCES entries (id),
  CONSTRAINT entry_values_dictionary_fk
    FOREIGN KEY (dictionary_id) REFERENCES dictionaries (id),
  CONSTRAINT entry_values_attribute_fk
    FOREIGN KEY (attribute_id) REFERENCES attributes (id),
  CONSTRAINT entry_values_one_value_chk
    CHECK (
      (value_text IS NOT NULL)::int +
      (value_num IS NOT NULL)::int +
      (value_date IS NOT NULL)::int +
      (value_bool IS NOT NULL)::int +
      (value_ref IS NOT NULL)::int +
      (value_json IS NOT NULL)::int = 1
    )
);

CREATE INDEX IF NOT EXISTS entry_values_dictionary_attribute_idx
  ON entry_values (dictionary_id, attribute_id);

CREATE INDEX IF NOT EXISTS entry_values_text_idx
  ON entry_values (dictionary_id, attribute_id, value_text);

CREATE INDEX IF NOT EXISTS entry_values_num_idx
  ON entry_values (dictionary_id, attribute_id, value_num);

CREATE INDEX IF NOT EXISTS entry_values_date_idx
  ON entry_values (dictionary_id, attribute_id, value_date);

CREATE INDEX IF NOT EXISTS entry_values_bool_idx
  ON entry_values (dictionary_id, attribute_id, value_bool);

CREATE INDEX IF NOT EXISTS entry_values_ref_idx
  ON entry_values (dictionary_id, attribute_id, value_ref);

-- Аудит изменений
CREATE TABLE IF NOT EXISTS audit_events (
  id BIGSERIAL PRIMARY KEY,
  event_id UUID NOT NULL DEFAULT uuid_generate_v4(),
  request_id UUID,
  actor_user_id UUID,
  actor_type TEXT NOT NULL DEFAULT 'user',
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id UUID,
  dictionary_id UUID,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  before_state JSONB,
  after_state JSONB,
  metadata JSONB,
  CONSTRAINT audit_events_actor_type_chk
    CHECK (actor_type IN ('user', 'service')),
  CONSTRAINT audit_events_actor_user_fk
    FOREIGN KEY (actor_user_id) REFERENCES users (id),
  CONSTRAINT audit_events_dictionary_fk
    FOREIGN KEY (dictionary_id) REFERENCES dictionaries (id)
);

CREATE INDEX IF NOT EXISTS audit_events_occurred_idx
  ON audit_events (occurred_at DESC);

CREATE INDEX IF NOT EXISTS audit_events_entity_idx
  ON audit_events (entity_type, entity_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS audit_events_actor_idx
  ON audit_events (actor_user_id, occurred_at DESC);

-- Outbox для доставки событий в Kafka
CREATE TABLE IF NOT EXISTS outbox_events (
  id BIGSERIAL PRIMARY KEY,
  event_id UUID NOT NULL DEFAULT uuid_generate_v4(),
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS outbox_unpublished_idx
  ON outbox_events (occurred_at)
  WHERE published_at IS NULL;

-- Примечание по партиционированию:
-- Для 100 млн+ объектов рассмотрите партиционирование entries по хэшу dictionary_id.
-- Аналогично можно партиционировать entry_values по dictionary_id.
