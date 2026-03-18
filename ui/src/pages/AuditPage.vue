<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ChevronLeft, ChevronRight, Clock3, Filter, RefreshCw, X } from 'lucide-vue-next'

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

function toLocalInput(value: string): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  const yyyy = date.getFullYear()
  const mm = String(date.getMonth() + 1).padStart(2, '0')
  const dd = String(date.getDate()).padStart(2, '0')
  const hh = String(date.getHours()).padStart(2, '0')
  const min = String(date.getMinutes()).padStart(2, '0')
  return `${yyyy}-${mm}-${dd}T${hh}:${min}`
}

function toIso(value: string): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toISOString()
}

const occurredFromLocal = computed({
  get: () => toLocalInput(filters.occurred_from),
  set: (value: string) => {
    filters.occurred_from = toIso(value)
  },
})

const occurredToLocal = computed({
  get: () => toLocalInput(filters.occurred_to),
  set: (value: string) => {
    filters.occurred_to = toIso(value)
  },
})

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / filters.limit)))
const currentPage = computed(() => Math.min(pageCount.value, Math.floor(filters.offset / filters.limit) + 1))

type PaginationItem = number | 'ellipsis-left' | 'ellipsis-right'
const paginationItems = computed<PaginationItem[]>(() => {
  const totalPages = pageCount.value
  const page = currentPage.value

  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, index) => index + 1)
  }

  const items: PaginationItem[] = [1]
  const left = Math.max(2, page - 1)
  const right = Math.min(totalPages - 1, page + 1)

  if (left > 2) {
    items.push('ellipsis-left')
  }
  for (let current = left; current <= right; current += 1) {
    items.push(current)
  }
  if (right < totalPages - 1) {
    items.push('ellipsis-right')
  }
  items.push(totalPages)

  return items
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

function applyFilters(): void {
  filters.offset = 0
  void load()
}

function clearFilters(): void {
  filters.entity_type = ''
  filters.entity_id = ''
  filters.actor_external_id = ''
  filters.occurred_from = ''
  filters.occurred_to = ''
  filters.offset = 0
  void load()
}

function setLast24Hours(): void {
  const now = new Date()
  const from = new Date(now.getTime() - 24 * 60 * 60 * 1000)
  filters.occurred_from = from.toISOString()
  filters.occurred_to = now.toISOString()
  filters.offset = 0
  void load()
}

function applyPageSize(): void {
  filters.offset = 0
  void load()
}

function goToPage(page: number): void {
  if (page < 1 || page > pageCount.value || page === currentPage.value) {
    return
  }
  filters.offset = (page - 1) * filters.limit
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
      <button class="btn" :disabled="loading" @click="load">
        <RefreshCw class="btn-icon" :size="16" aria-hidden="true" />
        Обновить
      </button>
    </div>

    <p v-if="error" class="alert error">{{ error }}</p>

    <article class="card">
      <h3>Фильтры</h3>
      <form class="form-grid" @submit.prevent="applyFilters">
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
          События с
          <input v-model="occurredFromLocal" type="datetime-local" />
        </label>
        <label>
          События до
          <input v-model="occurredToLocal" type="datetime-local" />
        </label>
        <div class="form-actions">
          <button class="btn primary">
            <Filter class="btn-icon" :size="16" aria-hidden="true" />
            Применить
          </button>
          <button type="button" class="btn" @click="setLast24Hours">
            <Clock3 class="btn-icon" :size="16" aria-hidden="true" />
            Последние 24 часа
          </button>
          <button type="button" class="btn" @click="clearFilters">
            <X class="btn-icon" :size="16" aria-hidden="true" />
            Сбросить
          </button>
        </div>
      </form>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>События ({{ total }})</h3>
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

      <div class="table-pagination">
        <nav class="pagination-nav" role="navigation" aria-label="pagination">
          <ul class="pagination-list">
            <li>
              <button class="pagination-link pagination-edge" :disabled="filters.offset === 0" @click="prevPage">
                <ChevronLeft :size="16" aria-hidden="true" />
                <span class="pagination-edge-text">Назад</span>
              </button>
            </li>
            <li v-for="item in paginationItems" :key="String(item)">
              <span v-if="item === 'ellipsis-left' || item === 'ellipsis-right'" class="pagination-ellipsis">…</span>
              <button
                v-else
                class="pagination-link"
                :class="{ active: item === currentPage }"
                @click="goToPage(item)"
              >
                {{ item }}
              </button>
            </li>
            <li>
              <button class="pagination-link pagination-edge" :disabled="filters.offset + filters.limit >= total" @click="nextPage">
                <span class="pagination-edge-text">Вперед</span>
                <ChevronRight :size="16" aria-hidden="true" />
              </button>
            </li>
          </ul>
        </nav>
        <label class="pagination-size">
          На странице
          <select v-model.number="filters.limit" @change="applyPageSize">
            <option :value="20">20</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
            <option :value="200">200</option>
          </select>
        </label>
      </div>
    </article>
  </section>
</template>
