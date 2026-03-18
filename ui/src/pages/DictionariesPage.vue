<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { BookOpen, ChevronLeft, ChevronRight, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'

import { type Dictionary, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

const identity = useDevIdentityStore()

const list = ref<Dictionary[]>([])
const schemaSizeByDictionary = ref<Record<string, number>>({})
const total = ref(0)
const limit = ref(20)
const offset = ref(0)

const loading = ref(false)
const submitting = ref(false)
const message = ref('')
const error = ref('')
const searchText = ref('')

const createForm = reactive({
  code: '',
  name: '',
  description: '',
})

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / limit.value)))
const currentPage = computed(() => Math.min(pageCount.value, Math.floor(offset.value / limit.value) + 1))

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

const filteredList = computed(() => {
  const query = searchText.value.trim().toLowerCase()
  if (!query) {
    return list.value
  }

  return list.value.filter((item) => {
    const haystack = `${item.code} ${item.name} ${item.description ?? ''}`.toLowerCase()
    return haystack.includes(query)
  })
})

async function load(): Promise<void> {
  loading.value = true
  error.value = ''

  try {
    const result = await mdmApi.listDictionaries(limit.value, offset.value)
    list.value = result.items
    total.value = result.total

    const settled = await Promise.allSettled(
      result.items.map(async (dictionary) => {
        const schema = await mdmApi.getDictionarySchema(dictionary.id)
        return {
          dictionaryId: dictionary.id,
          size: schema.attributes.length,
        }
      }),
    )

    const next: Record<string, number> = {}
    for (const row of settled) {
      if (row.status === 'fulfilled') {
        next[row.value.dictionaryId] = row.value.size
      }
    }
    schemaSizeByDictionary.value = next
  } catch (err) {
    error.value = formatError(err)
  } finally {
    loading.value = false
  }
}

async function createDictionary(): Promise<void> {
  if (!canWrite.value) {
    return
  }

  submitting.value = true
  error.value = ''
  message.value = ''

  try {
    await mdmApi.createDictionary({
      code: createForm.code,
      name: createForm.name,
      description: createForm.description || undefined,
    })

    createForm.code = ''
    createForm.name = ''
    createForm.description = ''
    message.value = 'Справочник создан'
    await load()
  } catch (err) {
    error.value = formatError(err)
  } finally {
    submitting.value = false
  }
}

async function removeDictionary(id: string): Promise<void> {
  if (!canWrite.value) {
    return
  }
  if (!window.confirm('Удалить справочник?')) {
    return
  }

  error.value = ''
  message.value = ''

  try {
    await mdmApi.deleteDictionary(id)
    message.value = 'Справочник удален'

    if (offset.value >= total.value - 1 && offset.value > 0) {
      offset.value = Math.max(0, offset.value - limit.value)
    }
    await load()
  } catch (err) {
    error.value = formatError(err)
  }
}

function nextPage(): void {
  if (offset.value + limit.value >= total.value) {
    return
  }
  offset.value += limit.value
  void load()
}

function prevPage(): void {
  if (offset.value === 0) {
    return
  }
  offset.value = Math.max(0, offset.value - limit.value)
  void load()
}

function applyPageSize(): void {
  offset.value = 0
  void load()
}

function goToPage(page: number): void {
  if (page < 1 || page > pageCount.value || page === currentPage.value) {
    return
  }
  offset.value = (page - 1) * limit.value
  void load()
}

onMounted(load)
</script>

<template>
  <section>
    <div class="section-head">
      <div>
        <h1>Справочники</h1>
        <p class="muted">Создание справочников и переход к настройке их атрибутов.</p>
      </div>
      <button class="btn" :disabled="loading" @click="load">
        <RefreshCw class="btn-icon" :size="16" aria-hidden="true" />
        Обновить
      </button>
    </div>

    <p v-if="message" class="alert success">{{ message }}</p>
    <p v-if="error" class="alert error">{{ error }}</p>

    <article class="card" :class="{ 'is-disabled': !canWrite }">
      <h3>Создать справочник</h3>
      <form class="form-grid" @submit.prevent="createDictionary">
        <label>
          Code
          <input v-model="createForm.code" placeholder="products" :disabled="!canWrite || submitting" />
        </label>
        <label>
          Name
          <input v-model="createForm.name" placeholder="Товары" :disabled="!canWrite || submitting" />
        </label>
        <label class="full">
          Description
          <input v-model="createForm.description" placeholder="Описание" :disabled="!canWrite || submitting" />
        </label>

        <div class="form-actions">
          <button class="btn primary" :disabled="!canWrite || submitting">
            <Plus class="btn-icon" :size="16" aria-hidden="true" />
            Создать
          </button>
        </div>
      </form>
      <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>Список ({{ total }})</h3>
      </div>
      <div class="table-toolbar">
        <label>
          Быстрый поиск
          <input v-model="searchText" placeholder="Код, имя или описание" />
        </label>
      </div>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Код</th>
              <th>Название</th>
              <th>Описание</th>
              <th>Версия схемы</th>
              <th>Атрибутов в схеме</th>
              <th class="actions">Действия</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="6" class="muted">Загрузка...</td>
            </tr>
            <tr v-if="!loading && filteredList.length === 0">
              <td colspan="6" class="muted">По фильтру ничего не найдено.</td>
            </tr>
            <tr v-for="item in filteredList" :key="item.id">
              <td><code>{{ item.code }}</code></td>
              <td>{{ item.name }}</td>
              <td>{{ item.description || '—' }}</td>
              <td>{{ item.schema_version }}</td>
              <td>{{ schemaSizeByDictionary[item.id] ?? '—' }}</td>
              <td class="actions-row">
                <RouterLink class="btn btn-icon-only" title="Открыть и редактировать справочник" :to="`/dictionaries/${item.id}`">
                  <BookOpen :size="16" aria-hidden="true" />
                  <span class="sr-only">Открыть и редактировать справочник</span>
                </RouterLink>
                <button class="btn danger btn-icon-only" title="Удалить справочник" :disabled="!canWrite" @click="removeDictionary(item.id)">
                  <Trash2 :size="16" aria-hidden="true" />
                  <span class="sr-only">Удалить справочник</span>
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="table-pagination">
        <nav class="pagination-nav" role="navigation" aria-label="pagination">
          <ul class="pagination-list">
            <li>
              <button class="pagination-link pagination-edge" :disabled="offset === 0" @click="prevPage">
                <ChevronLeft :size="16" aria-hidden="true" />
                <span class="pagination-edge-text">Назад</span>
              </button>
            </li>
            <li v-for="item in paginationItems" :key="String(item)">
              <span v-if="item === 'ellipsis-left' || item === 'ellipsis-right'" class="pagination-ellipsis">…</span>
              <button v-else class="pagination-link" :class="{ active: item === currentPage }" @click="goToPage(item)">
                {{ item }}
              </button>
            </li>
            <li>
              <button class="pagination-link pagination-edge" :disabled="offset + limit >= total" @click="nextPage">
                <span class="pagination-edge-text">Вперед</span>
                <ChevronRight :size="16" aria-hidden="true" />
              </button>
            </li>
          </ul>
        </nav>
        <label class="pagination-size">
          На странице
          <select v-model.number="limit" @change="applyPageSize">
            <option :value="20">20</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
          </select>
        </label>
      </div>
    </article>
  </section>
</template>
