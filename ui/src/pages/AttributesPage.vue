<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'

import { type Attribute, type Dictionary, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

const identity = useDevIdentityStore()

const list = ref<Attribute[]>([])
const dictionaries = ref<Dictionary[]>([])
const usageByAttribute = ref<Record<string, Dictionary[]>>({})

const total = ref(0)
const limit = ref(20)
const offset = ref(0)

const loading = ref(false)
const submitting = ref(false)
const message = ref('')
const error = ref('')

const dataTypes: Attribute['data_type'][] = ['string', 'number', 'date', 'boolean', 'enum', 'reference']

const createForm = reactive({
  code: '',
  name: '',
  description: '',
  data_type: 'string' as Attribute['data_type'],
  ref_dictionary_id: '',
})

const editForm = reactive({
  id: '',
  name: '',
  description: '',
})

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))

const dictionariesById = computed(() => {
  const map = new Map<string, Dictionary>()
  for (const dictionary of dictionaries.value) {
    map.set(dictionary.id, dictionary)
  }
  return map
})

function clearFeedback(): void {
  message.value = ''
  error.value = ''
}

function usageForAttribute(attributeId: string): Dictionary[] {
  return usageByAttribute.value[attributeId] ?? []
}

async function loadUsageForDictionaries(source: Dictionary[]): Promise<void> {
  const settled = await Promise.allSettled(
    source.map(async (dictionary) => {
      const result = await mdmApi.getDictionarySchema(dictionary.id)
      return {
        dictionary,
        schema: result.attributes,
      }
    }),
  )

  const nextUsage: Record<string, Dictionary[]> = {}
  for (const row of settled) {
    if (row.status !== 'fulfilled') {
      continue
    }
    for (const schemaAttribute of row.value.schema) {
      if (!nextUsage[schemaAttribute.attribute_id]) {
        nextUsage[schemaAttribute.attribute_id] = []
      }
      nextUsage[schemaAttribute.attribute_id].push(row.value.dictionary)
    }
  }

  usageByAttribute.value = nextUsage
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''

  try {
    const [attributesResult, dictionariesResult] = await Promise.all([
      mdmApi.listAttributes(limit.value, offset.value),
      mdmApi.listDictionaries(500, 0),
    ])

    list.value = attributesResult.items
    total.value = attributesResult.total
    dictionaries.value = dictionariesResult.items
    await loadUsageForDictionaries(dictionariesResult.items)
  } catch (err) {
    error.value = formatError(err)
  } finally {
    loading.value = false
  }
}

async function createAttribute(): Promise<void> {
  if (!canWrite.value) {
    return
  }

  submitting.value = true
  clearFeedback()

  try {
    await mdmApi.createAttribute({
      code: createForm.code,
      name: createForm.name,
      description: createForm.description || undefined,
      data_type: createForm.data_type,
      ref_dictionary_id:
        createForm.data_type === 'reference' && createForm.ref_dictionary_id ? createForm.ref_dictionary_id : undefined,
    })

    createForm.code = ''
    createForm.name = ''
    createForm.description = ''
    createForm.data_type = 'string'
    createForm.ref_dictionary_id = ''

    message.value = 'Атрибут создан'
    await load()
  } catch (err) {
    error.value = formatError(err)
  } finally {
    submitting.value = false
  }
}

function beginEdit(item: Attribute): void {
  editForm.id = item.id
  editForm.name = item.name
  editForm.description = item.description ?? ''
}

function cancelEdit(): void {
  editForm.id = ''
  editForm.name = ''
  editForm.description = ''
}

async function saveEdit(): Promise<void> {
  if (!editForm.id || !canWrite.value) {
    return
  }

  submitting.value = true
  clearFeedback()

  try {
    await mdmApi.updateAttribute(editForm.id, {
      name: editForm.name || undefined,
      description: editForm.description || undefined,
    })
    message.value = 'Атрибут обновлен'
    cancelEdit()
    await load()
  } catch (err) {
    error.value = formatError(err)
  } finally {
    submitting.value = false
  }
}

async function removeAttribute(id: string): Promise<void> {
  if (!canWrite.value) {
    return
  }
  if (!window.confirm('Удалить атрибут?')) {
    return
  }

  clearFeedback()

  try {
    await mdmApi.deleteAttribute(id)
    message.value = 'Атрибут удален'
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

onMounted(load)
</script>

<template>
  <section>
    <div class="section-head">
      <div>
        <h1>Атрибуты</h1>
        <p class="muted">Каталог атрибутов и их участие в справочниках.</p>
      </div>
      <button class="btn" :disabled="loading" @click="load">Обновить</button>
    </div>

    <p v-if="message" class="alert success">{{ message }}</p>
    <p v-if="error" class="alert error">{{ error }}</p>

    <article class="card" :class="{ 'is-disabled': !canWrite }">
      <h3>Создать атрибут</h3>
      <form class="form-grid" @submit.prevent="createAttribute">
        <label>
          Code
          <input v-model="createForm.code" placeholder="brand" :disabled="!canWrite || submitting" />
        </label>
        <label>
          Name
          <input v-model="createForm.name" placeholder="Бренд" :disabled="!canWrite || submitting" />
        </label>
        <label>
          Data Type
          <select v-model="createForm.data_type" :disabled="!canWrite || submitting">
            <option v-for="dataType in dataTypes" :key="dataType" :value="dataType">{{ dataType }}</option>
          </select>
        </label>
        <label>
          Ref Dictionary
          <select
            v-model="createForm.ref_dictionary_id"
            :disabled="!canWrite || submitting || createForm.data_type !== 'reference'"
          >
            <option value="">Выберите справочник</option>
            <option v-for="dictionary in dictionaries" :key="dictionary.id" :value="dictionary.id">
              {{ dictionary.code }}
            </option>
          </select>
        </label>
        <label class="full">
          Description
          <input v-model="createForm.description" placeholder="Описание" :disabled="!canWrite || submitting" />
        </label>

        <div class="form-actions">
          <button class="btn primary" :disabled="!canWrite || submitting">Создать</button>
        </div>
      </form>
      <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>Список ({{ total }})</h3>
        <div class="pager">
          <button class="btn" :disabled="offset === 0" @click="prevPage">Назад</button>
          <span>{{ offset + 1 }}-{{ Math.min(offset + limit, total) }}</span>
          <button class="btn" :disabled="offset + limit >= total" @click="nextPage">Вперед</button>
        </div>
      </div>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Code</th>
              <th>Name</th>
              <th>Type</th>
              <th>Ref dictionary</th>
              <th>Участвует в справочниках</th>
              <th class="actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="6" class="muted">Загрузка...</td>
            </tr>
            <tr v-for="item in list" :key="item.id">
              <td><code>{{ item.code }}</code></td>
              <td>{{ item.name }}</td>
              <td><span class="pill">{{ item.data_type }}</span></td>
              <td>
                <code>{{ item.ref_dictionary_id || '—' }}</code>
                <p v-if="item.ref_dictionary_id && dictionariesById.get(item.ref_dictionary_id)" class="muted">
                  {{ dictionariesById.get(item.ref_dictionary_id)?.code }}
                </p>
              </td>
              <td>
                <div class="chip-list">
                  <RouterLink
                    v-for="dictionary in usageForAttribute(item.id)"
                    :key="dictionary.id"
                    class="chip"
                    :to="`/dictionaries/${dictionary.id}`"
                  >
                    {{ dictionary.code }}
                  </RouterLink>
                  <span v-if="usageForAttribute(item.id).length === 0" class="muted">Не используется</span>
                </div>
              </td>
              <td class="actions-row">
                <button class="btn" :disabled="!canWrite" @click="beginEdit(item)">Edit</button>
                <button class="btn danger" :disabled="!canWrite" @click="removeAttribute(item.id)">Delete</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </article>

    <article v-if="editForm.id" class="card">
      <h3>Редактирование атрибута</h3>
      <form class="form-grid" @submit.prevent="saveEdit">
        <label>
          Name
          <input v-model="editForm.name" :disabled="submitting" />
        </label>
        <label>
          Description
          <input v-model="editForm.description" :disabled="submitting" />
        </label>
        <div class="form-actions">
          <button class="btn primary" :disabled="submitting">Сохранить</button>
          <button type="button" class="btn" :disabled="submitting" @click="cancelEdit">Отмена</button>
        </div>
      </form>
    </article>
  </section>
</template>
