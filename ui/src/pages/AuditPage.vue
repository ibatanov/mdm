<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import JsonBox from '../components/JsonBox.vue'
import { type AuditEvent, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'

const items = ref<AuditEvent[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')

const filters = reactive({
  limit: 50,
  offset: 0,
  entity_type: '',
  entity_id: '',
  actor_external_id: '',
  occurred_from: '',
  occurred_to: '',
})

async function load(): Promise<void> {
  loading.value = true
  error.value = ''

  try {
    const result = await mdmApi.listAuditEvents({
      limit: filters.limit,
      offset: filters.offset,
      entity_type: filters.entity_type || undefined,
      entity_id: filters.entity_id || undefined,
      actor_external_id: filters.actor_external_id || undefined,
      occurred_from: filters.occurred_from || undefined,
      occurred_to: filters.occurred_to || undefined,
    })

    items.value = result.items
    total.value = result.total
  } catch (err) {
    error.value = formatError(err)
  } finally {
    loading.value = false
  }
}

function nextPage(): void {
  if (filters.offset + filters.limit >= total.value) {
    return
  }
  filters.offset += filters.limit
  void load()
}

function prevPage(): void {
  if (filters.offset === 0) {
    return
  }
  filters.offset = Math.max(0, filters.offset - filters.limit)
  void load()
}

onMounted(load)
</script>

<template>
  <section>
    <div class="section-head">
      <div>
        <h1>Аудит</h1>
        <p class="muted">Журнал изменений с фильтрами по сущности, actor и времени.</p>
      </div>
      <button class="btn" :disabled="loading" @click="load">Обновить</button>
    </div>

    <p v-if="error" class="alert error">{{ error }}</p>

    <article class="card">
      <h3>Фильтры</h3>
      <form class="form-grid" @submit.prevent="load">
        <label>
          entity_type
          <input v-model="filters.entity_type" placeholder="entry" />
        </label>
        <label>
          entity_id
          <input v-model="filters.entity_id" placeholder="UUID" />
        </label>
        <label>
          actor_external_id
          <input v-model="filters.actor_external_id" placeholder="100" />
        </label>
        <label>
          occurred_from (RFC3339)
          <input v-model="filters.occurred_from" placeholder="2026-03-17T00:00:00Z" />
        </label>
        <label>
          occurred_to (RFC3339)
          <input v-model="filters.occurred_to" placeholder="2026-03-17T23:59:59Z" />
        </label>
        <label>
          limit
          <input v-model.number="filters.limit" type="number" min="1" max="500" />
        </label>

        <div class="form-actions">
          <button class="btn primary">Применить</button>
        </div>
      </form>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>События ({{ total }})</h3>
        <div class="pager">
          <button class="btn" :disabled="filters.offset === 0" @click="prevPage">Назад</button>
          <span>{{ filters.offset + 1 }}-{{ Math.min(filters.offset + filters.limit, total) }}</span>
          <button class="btn" :disabled="filters.offset + filters.limit >= total" @click="nextPage">Вперед</button>
        </div>
      </div>

      <div class="audit-list">
        <article v-if="loading" class="audit-item">
          <p class="muted">Загрузка...</p>
        </article>

        <article v-for="event in items" :key="event.event_id" class="audit-item">
          <div class="audit-head">
            <p>
              <strong>{{ event.action }}</strong>
              <span class="muted">{{ event.entity_type }}</span>
            </p>
            <p class="muted">{{ event.occurred_at }}</p>
          </div>

          <p class="muted">
            actor={{ event.actor_external_id || 'n/a' }};
            entity_id={{ event.entity_id || 'n/a' }};
            dictionary_id={{ event.dictionary_id || 'n/a' }}
          </p>

          <div class="audit-json-grid">
            <JsonBox :value="event.before_state" label="before_state" />
            <JsonBox :value="event.after_state" label="after_state" />
          </div>
          <JsonBox :value="event.metadata" label="metadata" />
        </article>
      </div>
    </article>
  </section>
</template>
