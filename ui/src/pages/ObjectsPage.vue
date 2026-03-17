<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import JsonBox from '../components/JsonBox.vue'
import {
  type Attribute,
  type Dictionary,
  type Entry,
  type SchemaAttribute,
  type SearchFilter,
  type SearchRequest,
  mdmApi,
} from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

type SearchOp = 'eq' | 'ne' | 'lt' | 'lte' | 'gt' | 'gte' | 'in' | 'contains' | 'prefix' | 'range'

interface SearchRowDraft {
  row_id: string
  attribute: string
  op: SearchOp
  value: string
  values: string
  from: string
  to: string
}

interface DictionaryField {
  attributeId: string
  code: string
  name: string
  dataType: Attribute['data_type']
  required: boolean
  isMultivalue: boolean
  validators: Record<string, unknown>
}

interface GlobalObjectRow {
  dictionary: Dictionary
  entry: Entry
}

const SEARCH_OPS: Array<{ value: SearchOp; label: string }> = [
  { value: 'eq', label: '=' },
  { value: 'ne', label: '!=' },
  { value: 'lt', label: '<' },
  { value: 'lte', label: '<=' },
  { value: 'gt', label: '>' },
  { value: 'gte', label: '>=' },
  { value: 'in', label: 'IN' },
  { value: 'contains', label: 'contains' },
  { value: 'prefix', label: 'prefix' },
  { value: 'range', label: 'range' },
]

const route = useRoute()
const identity = useDevIdentityStore()

const loading = ref(false)
const busy = ref(false)
const searching = ref(false)
const globalSearching = ref(false)
const error = ref('')
const message = ref('')

const dictionaries = ref<Dictionary[]>([])
const attributes = ref<Attribute[]>([])
const selectedDictionaryId = ref('')
const currentSchema = ref<SchemaAttribute[]>([])

const entries = ref<Entry[]>([])
const entriesTotal = ref(0)
const entriesLimit = ref(20)
const entriesOffset = ref(0)

const createExternalKey = ref('')
const createValues = ref<Record<string, string>>({})
const createIssues = ref<string[]>([])

const editEntryId = ref('')
const editValues = ref<Record<string, string>>({})
const editClear = ref<Record<string, boolean>>({})
const editOriginal = ref<Record<string, unknown>>({})
const editIssues = ref<string[]>([])

const searchRows = ref<SearchRowDraft[]>([])
const searchResult = ref<Entry[]>([])
const searchTotal = ref(0)
const searchLimit = ref(20)
const searchOffset = ref(0)
const searchSortAttribute = ref('')
const searchSortDirection = ref<'asc' | 'desc'>('asc')
const searchIssues = ref<string[]>([])

const globalQuery = ref('')
const globalLimitPerDictionary = ref(30)
const globalRows = ref<GlobalObjectRow[]>([])
const globalTotal = ref(0)
const globalIssues = ref<string[]>([])

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))

const selectedDictionary = computed(() =>
  dictionaries.value.find((dictionary) => dictionary.id === selectedDictionaryId.value) ?? null,
)

const attributesById = computed(() => {
  const map = new Map<string, Attribute>()
  for (const attribute of attributes.value) {
    map.set(attribute.id, attribute)
  }
  return map
})

const fields = computed<DictionaryField[]>(() => {
  const result: DictionaryField[] = []
  const sorted = [...currentSchema.value].sort((a, b) => a.position - b.position)

  for (const item of sorted) {
    const attribute = attributesById.value.get(item.attribute_id)
    if (!attribute) {
      continue
    }
    result.push({
      attributeId: item.attribute_id,
      code: attribute.code,
      name: attribute.name,
      dataType: attribute.data_type,
      required: item.required,
      isMultivalue: item.is_multivalue,
      validators: asRecord(item.validators),
    })
  }

  return result
})

const fieldsByCode = computed(() => {
  const map = new Map<string, DictionaryField>()
  for (const field of fields.value) {
    map.set(field.code, field)
  }
  return map
})

watch(
  fields,
  () => {
    initializeCreateValues()
    syncSearchDefaults()
  },
  { immediate: true },
)

watch(selectedDictionaryId, () => {
  entriesOffset.value = 0
  clearEntryEditor()
  searchResult.value = []
  searchTotal.value = 0
  searchIssues.value = []
  void loadDictionaryWorkspace()
})

watch(
  () => route.query.dictionaryId,
  (next) => {
    const id = String(next ?? '').trim()
    if (!id) {
      return
    }
    const exists = dictionaries.value.some((dictionary) => dictionary.id === id)
    if (exists && selectedDictionaryId.value !== id) {
      selectedDictionaryId.value = id
    }
  },
)

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function hasOwn(data: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(data, key)
}

function clearFeedback(): void {
  error.value = ''
  message.value = ''
}

function makeSearchRow(partial?: Partial<SearchRowDraft>): SearchRowDraft {
  return {
    row_id: crypto.randomUUID(),
    attribute: fields.value[0]?.code ?? '',
    op: 'eq',
    value: '',
    values: '',
    from: '',
    to: '',
    ...partial,
  }
}

function syncSearchDefaults(): void {
  if (searchRows.value.length === 0 && fields.value.length > 0) {
    searchRows.value = [makeSearchRow({ attribute: fields.value[0].code, op: 'eq' })]
  }
  if (!searchSortAttribute.value && fields.value.length > 0) {
    searchSortAttribute.value = fields.value[0].code
  }
}

function toInputString(value: unknown, field: DictionaryField): string {
  if (value === undefined || value === null) {
    return ''
  }
  if (field.isMultivalue) {
    if (!Array.isArray(value)) {
      return ''
    }
    return value.map((item) => String(item)).join('\n')
  }
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false'
  }
  if (typeof value === 'number') {
    return String(value)
  }
  return String(value)
}

function parseSingleValue(raw: string, field: DictionaryField): unknown {
  const value = raw.trim()

  switch (field.dataType) {
    case 'number': {
      const parsed = Number(value)
      if (!Number.isFinite(parsed)) {
        throw new Error(`Поле "${field.name}" должно быть числом`)
      }
      return parsed
    }
    case 'boolean': {
      if (value === 'true') {
        return true
      }
      if (value === 'false') {
        return false
      }
      throw new Error(`Поле "${field.name}" должно быть true/false`)
    }
    case 'date': {
      if (Number.isNaN(Date.parse(value))) {
        throw new Error(`Поле "${field.name}" должно быть валидной датой`)
      }
      return value
    }
    case 'enum':
    case 'reference':
    case 'string':
      return value
  }
}

function parseFieldValue(raw: string, field: DictionaryField): unknown | undefined {
  const trimmed = raw.trim()
  if (trimmed === '') {
    return undefined
  }

  if (!field.isMultivalue) {
    return parseSingleValue(trimmed, field)
  }

  const tokens = trimmed
    .split(/\n|,/)
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)

  if (tokens.length === 0) {
    return undefined
  }

  return tokens.map((entry) => parseSingleValue(entry, field))
}

function initializeCreateValues(): void {
  const next: Record<string, string> = {}
  for (const field of fields.value) {
    next[field.code] = ''
  }
  createValues.value = next
  createIssues.value = []
}

function buildCreateData(): { data: Record<string, unknown>; issues: string[] } {
  const data: Record<string, unknown> = {}
  const issues: string[] = []

  for (const field of fields.value) {
    const raw = createValues.value[field.code] ?? ''
    try {
      const parsed = parseFieldValue(raw, field)
      if (parsed === undefined) {
        if (field.required) {
          issues.push(`Поле "${field.name}" обязательно`)
        }
        continue
      }
      data[field.code] = parsed
    } catch (err) {
      issues.push(formatError(err))
    }
  }

  return { data, issues }
}

function startEditEntry(entry: Entry): void {
  editEntryId.value = entry.id
  editOriginal.value = JSON.parse(JSON.stringify(entry.data)) as Record<string, unknown>

  const nextValues: Record<string, string> = {}
  const nextClear: Record<string, boolean> = {}
  for (const field of fields.value) {
    nextValues[field.code] = toInputString(entry.data[field.code], field)
    nextClear[field.code] = false
  }
  editValues.value = nextValues
  editClear.value = nextClear
  editIssues.value = []
}

function clearEntryEditor(): void {
  editEntryId.value = ''
  editValues.value = {}
  editClear.value = {}
  editOriginal.value = {}
  editIssues.value = []
}

function deepEqual(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

function buildEditFinalData(): { final: Record<string, unknown>; issues: string[] } {
  const finalData: Record<string, unknown> = JSON.parse(JSON.stringify(editOriginal.value)) as Record<string, unknown>
  const issues: string[] = []

  for (const field of fields.value) {
    if (editClear.value[field.code]) {
      delete finalData[field.code]
      continue
    }

    const raw = editValues.value[field.code] ?? ''
    try {
      const parsed = parseFieldValue(raw, field)
      if (parsed !== undefined) {
        finalData[field.code] = parsed
      }
    } catch (err) {
      issues.push(formatError(err))
    }
  }

  for (const field of fields.value) {
    if (field.required && !hasOwn(finalData, field.code)) {
      issues.push(`Поле "${field.name}" обязательно`)
    }
  }

  return { final: finalData, issues }
}

function buildPatchDelta(original: Record<string, unknown>, finalData: Record<string, unknown>): Record<string, unknown> {
  const patch: Record<string, unknown> = {}
  const keys = new Set<string>([...Object.keys(original), ...Object.keys(finalData)])

  for (const key of keys) {
    const originalHas = hasOwn(original, key)
    const finalHas = hasOwn(finalData, key)

    if (originalHas && !finalHas) {
      patch[key] = null
      continue
    }

    if (finalHas) {
      const originalValue = originalHas ? original[key] : undefined
      const finalValue = finalData[key]
      if (!originalHas || !deepEqual(originalValue, finalValue)) {
        patch[key] = finalValue
      }
    }
  }

  return patch
}

function enumOptions(field: DictionaryField): string[] {
  const raw = field.validators.allowed_values
  if (!Array.isArray(raw)) {
    return []
  }
  return raw
    .filter((item): item is string => typeof item === 'string')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

function coerceSearchAtom(field: DictionaryField | undefined, raw: string): unknown {
  const trimmed = raw.trim()
  if (trimmed === '') {
    throw new Error('Значение фильтра не должно быть пустым')
  }

  if (!field) {
    return trimmed
  }

  if (field.dataType === 'number') {
    const parsed = Number(trimmed)
    if (!Number.isFinite(parsed)) {
      throw new Error(`Фильтр по полю "${field.name}" ожидает число`)
    }
    return parsed
  }

  if (field.dataType === 'boolean') {
    if (trimmed === 'true') {
      return true
    }
    if (trimmed === 'false') {
      return false
    }
    throw new Error(`Фильтр по полю "${field.name}" ожидает true/false`)
  }

  return trimmed
}

function buildSearchBody(): { body: SearchRequest; issues: string[] } {
  const issues: string[] = []
  const filters: SearchFilter[] = []

  for (let index = 0; index < searchRows.value.length; index += 1) {
    const row = searchRows.value[index]
    const rowNumber = index + 1
    if (!row.attribute) {
      issues.push(`Фильтр ${rowNumber}: выберите атрибут`)
      continue
    }

    const field = fieldsByCode.value.get(row.attribute)

    try {
      switch (row.op) {
        case 'in': {
          const items = row.values
            .split(/\n|,/)
            .map((item) => item.trim())
            .filter((item) => item.length > 0)
            .map((item) => coerceSearchAtom(field, item))

          if (items.length === 0) {
            issues.push(`Фильтр ${rowNumber}: укажите значения для IN`)
            break
          }

          filters.push({ attribute: row.attribute, op: row.op, values: items })
          break
        }
        case 'range': {
          if (row.from.trim() === '' || row.to.trim() === '') {
            issues.push(`Фильтр ${rowNumber}: для range заполните from/to`)
            break
          }
          filters.push({
            attribute: row.attribute,
            op: row.op,
            from: coerceSearchAtom(field, row.from),
            to: coerceSearchAtom(field, row.to),
          })
          break
        }
        case 'contains':
        case 'prefix': {
          if (row.value.trim() === '') {
            issues.push(`Фильтр ${rowNumber}: значение не должно быть пустым`)
            break
          }
          filters.push({ attribute: row.attribute, op: row.op, value: row.value.trim() })
          break
        }
        default: {
          if (row.value.trim() === '') {
            issues.push(`Фильтр ${rowNumber}: значение не должно быть пустым`)
            break
          }
          filters.push({
            attribute: row.attribute,
            op: row.op,
            value: coerceSearchAtom(field, row.value),
          })
          break
        }
      }
    } catch (err) {
      issues.push(`Фильтр ${rowNumber}: ${formatError(err)}`)
    }
  }

  const body: SearchRequest = {
    filters,
    sort: searchSortAttribute.value
      ? [{ attribute: searchSortAttribute.value, direction: searchSortDirection.value }]
      : undefined,
    page: {
      limit: searchLimit.value,
      offset: searchOffset.value,
    },
  }

  return { body, issues }
}

async function loadBootData(): Promise<void> {
  loading.value = true
  clearFeedback()

  try {
    const [dictionariesResult, attributesResult] = await Promise.all([
      mdmApi.listDictionaries(500, 0),
      mdmApi.listAttributes(500, 0),
    ])

    dictionaries.value = dictionariesResult.items
    attributes.value = attributesResult.items

    const queryDictionaryId = String(route.query.dictionaryId ?? '').trim()
    const exists = dictionaries.value.some((item) => item.id === queryDictionaryId)

    if (exists) {
      selectedDictionaryId.value = queryDictionaryId
    } else if (!selectedDictionaryId.value && dictionaries.value.length > 0) {
      selectedDictionaryId.value = dictionaries.value[0].id
    }

    await loadDictionaryWorkspace()
  } catch (err) {
    error.value = formatError(err)
  } finally {
    loading.value = false
  }
}

async function loadDictionaryWorkspace(): Promise<void> {
  if (!selectedDictionaryId.value) {
    currentSchema.value = []
    entries.value = []
    entriesTotal.value = 0
    return
  }

  try {
    const [schemaResult, entriesResult] = await Promise.all([
      mdmApi.getDictionarySchema(selectedDictionaryId.value),
      mdmApi.listEntries(selectedDictionaryId.value, entriesLimit.value, entriesOffset.value),
    ])

    currentSchema.value = schemaResult.attributes
    entries.value = entriesResult.items
    entriesTotal.value = entriesResult.total
  } catch (err) {
    error.value = formatError(err)
  }
}

async function createEntryFromForm(): Promise<void> {
  if (!selectedDictionaryId.value || !canWrite.value) {
    return
  }

  busy.value = true
  clearFeedback()
  createIssues.value = []

  try {
    const built = buildCreateData()
    if (built.issues.length > 0) {
      createIssues.value = built.issues
      return
    }

    await mdmApi.createEntry(selectedDictionaryId.value, {
      external_key: createExternalKey.value.trim() || undefined,
      data: built.data,
    })

    createExternalKey.value = ''
    initializeCreateValues()
    await loadDictionaryWorkspace()
    message.value = 'Объект создан'
  } catch (err) {
    error.value = formatError(err)
  } finally {
    busy.value = false
  }
}

async function saveEntryEdit(): Promise<void> {
  if (!selectedDictionaryId.value || !editEntryId.value || !canWrite.value) {
    return
  }

  busy.value = true
  clearFeedback()
  editIssues.value = []

  try {
    const built = buildEditFinalData()
    if (built.issues.length > 0) {
      editIssues.value = built.issues
      return
    }

    const patch = buildPatchDelta(editOriginal.value, built.final)
    if (Object.keys(patch).length === 0) {
      message.value = 'Изменений нет'
      return
    }

    await mdmApi.updateEntry(selectedDictionaryId.value, editEntryId.value, patch)
    clearEntryEditor()
    await loadDictionaryWorkspace()
    message.value = 'Объект обновлен'
  } catch (err) {
    error.value = formatError(err)
  } finally {
    busy.value = false
  }
}

async function refreshEntry(entryId: string): Promise<void> {
  if (!selectedDictionaryId.value) {
    return
  }

  clearFeedback()
  try {
    const fresh = await mdmApi.getEntry(selectedDictionaryId.value, entryId)
    const index = entries.value.findIndex((item) => item.id === entryId)
    if (index >= 0) {
      entries.value[index] = fresh
    }
    if (editEntryId.value === entryId) {
      startEditEntry(fresh)
    }
  } catch (err) {
    error.value = formatError(err)
  }
}

async function removeEntry(entryId: string): Promise<void> {
  if (!selectedDictionaryId.value || !canWrite.value) {
    return
  }
  if (!window.confirm('Удалить объект?')) {
    return
  }

  busy.value = true
  clearFeedback()

  try {
    await mdmApi.deleteEntry(selectedDictionaryId.value, entryId)
    await loadDictionaryWorkspace()
    if (editEntryId.value === entryId) {
      clearEntryEditor()
    }
    message.value = 'Объект удален'
  } catch (err) {
    error.value = formatError(err)
  } finally {
    busy.value = false
  }
}

function prevPage(): void {
  if (entriesOffset.value === 0) {
    return
  }
  entriesOffset.value = Math.max(0, entriesOffset.value - entriesLimit.value)
  void loadDictionaryWorkspace()
}

function nextPage(): void {
  if (entriesOffset.value + entriesLimit.value >= entriesTotal.value) {
    return
  }
  entriesOffset.value += entriesLimit.value
  void loadDictionaryWorkspace()
}

function addSearchRow(): void {
  searchRows.value.push(makeSearchRow())
}

function removeSearchRow(rowId: string): void {
  searchRows.value = searchRows.value.filter((row) => row.row_id !== rowId)
}

async function runSearch(): Promise<void> {
  if (!selectedDictionaryId.value) {
    return
  }

  searching.value = true
  clearFeedback()
  searchIssues.value = []

  try {
    const built = buildSearchBody()
    if (built.issues.length > 0) {
      searchIssues.value = built.issues
      return
    }
    const result = await mdmApi.searchEntries(selectedDictionaryId.value, built.body)
    searchResult.value = result.items
    searchTotal.value = result.total
  } catch (err) {
    error.value = formatError(err)
  } finally {
    searching.value = false
  }
}

function prevSearchPage(): void {
  if (searchOffset.value === 0) {
    return
  }
  searchOffset.value = Math.max(0, searchOffset.value - searchLimit.value)
  void runSearch()
}

function nextSearchPage(): void {
  if (searchOffset.value + searchLimit.value >= searchTotal.value) {
    return
  }
  searchOffset.value += searchLimit.value
  void runSearch()
}

async function runGlobalSearch(): Promise<void> {
  globalSearching.value = true
  clearFeedback()
  globalIssues.value = []

  try {
    if (dictionaries.value.length === 0) {
      globalRows.value = []
      globalTotal.value = 0
      return
    }

    const perDictionaryLimit = Math.max(1, Math.min(200, Number(globalLimitPerDictionary.value) || 30))
    globalLimitPerDictionary.value = perDictionaryLimit
    const query = globalQuery.value.trim().toLowerCase()

    const settled = await Promise.allSettled(
      dictionaries.value.map(async (dictionary) => {
        const result = await mdmApi.listEntries(dictionary.id, perDictionaryLimit, 0)
        return {
          dictionary,
          entries: result.items,
        }
      }),
    )

    const rows: GlobalObjectRow[] = []
    for (const result of settled) {
      if (result.status === 'rejected') {
        globalIssues.value.push(formatError(result.reason))
        continue
      }

      for (const entry of result.value.entries) {
        if (query.length > 0) {
          const text = `${result.value.dictionary.code} ${result.value.dictionary.name} ${
            entry.external_key || ''
          } ${JSON.stringify(entry.data)}`.toLowerCase()
          if (!text.includes(query)) {
            continue
          }
        }
        rows.push({
          dictionary: result.value.dictionary,
          entry,
        })
      }
    }

    rows.sort((left, right) => {
      const byDictionary = left.dictionary.code.localeCompare(right.dictionary.code, 'ru')
      if (byDictionary !== 0) {
        return byDictionary
      }
      return (left.entry.external_key || left.entry.id).localeCompare(right.entry.external_key || right.entry.id, 'ru')
    })

    globalRows.value = rows
    globalTotal.value = rows.length
  } catch (err) {
    error.value = formatError(err)
  } finally {
    globalSearching.value = false
  }
}

async function openGlobalEntry(row: GlobalObjectRow): Promise<void> {
  try {
    if (selectedDictionaryId.value !== row.dictionary.id) {
      selectedDictionaryId.value = row.dictionary.id
      await loadDictionaryWorkspace()
    }
    const fresh = await mdmApi.getEntry(row.dictionary.id, row.entry.id)
    startEditEntry(fresh)
  } catch (err) {
    error.value = formatError(err)
  }
}

onMounted(loadBootData)
</script>

<template>
  <section>
    <div class="section-head">
      <div>
        <h1>Объекты</h1>
        <p class="muted">Создание, просмотр, редактирование и поиск объектов по настроенным схемам справочников.</p>
      </div>
      <button class="btn" :disabled="loading || busy" @click="loadBootData">Обновить</button>
    </div>

    <p v-if="message" class="alert success">{{ message }}</p>
    <p v-if="error" class="alert error">{{ error }}</p>

    <article class="card">
      <div class="form-inline">
        <label>
          Справочник для работы
          <select v-model="selectedDictionaryId">
            <option value="">Выберите справочник</option>
            <option v-for="dictionary in dictionaries" :key="dictionary.id" :value="dictionary.id">
              {{ dictionary.code }} — {{ dictionary.name }}
            </option>
          </select>
        </label>
        <RouterLink v-if="selectedDictionary" class="btn" :to="`/dictionaries/${selectedDictionary.id}`">
          Настроить справочник
        </RouterLink>
      </div>
      <p v-if="!selectedDictionary" class="muted">Сначала выберите справочник.</p>
      <p v-else class="muted">
        Активный справочник: <strong>{{ selectedDictionary.name }}</strong> ({{ selectedDictionary.code }})
      </p>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>Поиск по всем объектам</h3>
        <button class="btn primary" :disabled="globalSearching" @click="runGlobalSearch">Искать по всем</button>
      </div>
      <p class="muted">
        Поиск проходит по объектам всех справочников (по `dictionary code/name`, `external_key`, `data`).
      </p>
      <div class="form-grid">
        <label>
          Текст поиска
          <input v-model="globalQuery" placeholder="например: Acme, SKU-001, brand" />
        </label>
        <label>
          Лимит объектов на справочник
          <input v-model.number="globalLimitPerDictionary" type="number" min="1" max="200" />
        </label>
      </div>

      <ul v-if="globalIssues.length > 0" class="issue-list">
        <li v-for="issue in globalIssues" :key="issue">{{ issue }}</li>
      </ul>

      <div class="card-title-line">
        <h3>Результаты ({{ globalTotal }})</h3>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Справочник</th>
              <th>external_key / id</th>
              <th>version</th>
              <th>data</th>
              <th class="actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="globalSearching">
              <td colspan="5" class="muted">Поиск...</td>
            </tr>
            <tr v-if="!globalSearching && globalRows.length === 0">
              <td colspan="5" class="muted">Нет результатов. Нажмите «Искать по всем».</td>
            </tr>
            <tr v-for="row in globalRows" :key="`${row.dictionary.id}:${row.entry.id}`">
              <td>
                <RouterLink class="link" :to="`/dictionaries/${row.dictionary.id}`">{{ row.dictionary.code }}</RouterLink>
              </td>
              <td>{{ row.entry.external_key || row.entry.id }}</td>
              <td>{{ row.entry.version }}</td>
              <td><JsonBox :value="row.entry.data" label="data" /></td>
              <td class="actions-row">
                <button class="btn" @click="openGlobalEntry(row)">Открыть в редакторе</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </article>

    <article class="card" :class="{ 'is-disabled': !canWrite }">
      <div class="card-title-line">
        <h3>Создать объект</h3>
      </div>
      <p class="muted">Форма строится автоматически по схеме выбранного справочника.</p>

      <ul v-if="createIssues.length > 0" class="issue-list">
        <li v-for="issue in createIssues" :key="issue">{{ issue }}</li>
      </ul>

      <form class="entry-grid" @submit.prevent="createEntryFromForm">
        <label>
          external_key
          <input v-model="createExternalKey" placeholder="SKU-001" :disabled="!selectedDictionaryId || !canWrite || busy" />
        </label>

        <div v-for="field in fields" :key="field.attributeId" class="entry-field">
          <label :class="{ required: field.required }">
            {{ field.name }} <span class="muted">({{ field.code }})</span>
            <template v-if="field.isMultivalue">
              <textarea
                v-model="createValues[field.code]"
                class="code-area code-area-compact"
                :placeholder="`каждое значение с новой строки (${field.dataType})`"
                :disabled="!selectedDictionaryId || !canWrite || busy"
              ></textarea>
            </template>
            <template v-else-if="field.dataType === 'boolean'">
              <select v-model="createValues[field.code]" :disabled="!selectedDictionaryId || !canWrite || busy">
                <option value="">—</option>
                <option value="true">true</option>
                <option value="false">false</option>
              </select>
            </template>
            <template v-else-if="field.dataType === 'enum' && enumOptions(field).length > 0">
              <select v-model="createValues[field.code]" :disabled="!selectedDictionaryId || !canWrite || busy">
                <option value="">—</option>
                <option v-for="option in enumOptions(field)" :key="option" :value="option">
                  {{ option }}
                </option>
              </select>
            </template>
            <template v-else>
              <input
                v-model="createValues[field.code]"
                :placeholder="field.dataType"
                :disabled="!selectedDictionaryId || !canWrite || busy"
              />
            </template>
          </label>
        </div>

        <div class="form-actions">
          <button class="btn primary" :disabled="!selectedDictionaryId || !canWrite || busy || fields.length === 0">
            Создать объект
          </button>
        </div>
      </form>
      <p v-if="fields.length === 0" class="muted">Для выбранного справочника не настроены атрибуты в схеме.</p>
      <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>Объекты выбранного справочника ({{ entriesTotal }})</h3>
        <div class="pager">
          <button class="btn" :disabled="entriesOffset === 0" @click="prevPage">Назад</button>
          <span>{{ entriesOffset + 1 }}-{{ Math.min(entriesOffset + entriesLimit, entriesTotal) }}</span>
          <button class="btn" :disabled="entriesOffset + entriesLimit >= entriesTotal" @click="nextPage">Вперед</button>
        </div>
      </div>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>external_key</th>
              <th>version</th>
              <th class="actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="4" class="muted">Загрузка...</td>
            </tr>
            <tr v-for="entry in entries" :key="entry.id">
              <td><code>{{ entry.id }}</code></td>
              <td>{{ entry.external_key || '—' }}</td>
              <td>{{ entry.version }}</td>
              <td class="actions-row">
                <button class="btn" :disabled="!selectedDictionaryId" @click="refreshEntry(entry.id)">Get</button>
                <button class="btn" :disabled="!canWrite || !selectedDictionaryId" @click="startEditEntry(entry)">Edit</button>
                <button class="btn danger" :disabled="!canWrite || !selectedDictionaryId" @click="removeEntry(entry.id)">
                  Delete
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </article>

    <article v-if="editEntryId" class="card" :class="{ 'is-disabled': !canWrite }">
      <div class="card-title-line">
        <h3>Редактирование объекта</h3>
        <span class="pill"><code>{{ editEntryId }}</code></span>
      </div>

      <ul v-if="editIssues.length > 0" class="issue-list">
        <li v-for="issue in editIssues" :key="issue">{{ issue }}</li>
      </ul>

      <form class="entry-grid" @submit.prevent="saveEntryEdit">
        <div v-for="field in fields" :key="field.attributeId" class="entry-field">
          <label :class="{ required: field.required }">
            {{ field.name }} <span class="muted">({{ field.code }})</span>
            <template v-if="field.isMultivalue">
              <textarea
                v-model="editValues[field.code]"
                class="code-area code-area-compact"
                :placeholder="`каждое значение с новой строки (${field.dataType})`"
                :disabled="!selectedDictionaryId || !canWrite || busy"
              ></textarea>
            </template>
            <template v-else-if="field.dataType === 'boolean'">
              <select v-model="editValues[field.code]" :disabled="!selectedDictionaryId || !canWrite || busy">
                <option value="">—</option>
                <option value="true">true</option>
                <option value="false">false</option>
              </select>
            </template>
            <template v-else-if="field.dataType === 'enum' && enumOptions(field).length > 0">
              <select v-model="editValues[field.code]" :disabled="!selectedDictionaryId || !canWrite || busy">
                <option value="">—</option>
                <option v-for="option in enumOptions(field)" :key="option" :value="option">
                  {{ option }}
                </option>
              </select>
            </template>
            <template v-else>
              <input
                v-model="editValues[field.code]"
                :placeholder="field.dataType"
                :disabled="!selectedDictionaryId || !canWrite || busy"
              />
            </template>
          </label>
          <label class="check">
            <input v-model="editClear[field.code]" type="checkbox" :disabled="!selectedDictionaryId || !canWrite || busy" />
            Удалить поле из объекта
          </label>
        </div>

        <div class="form-actions">
          <button class="btn primary" :disabled="!selectedDictionaryId || !canWrite || busy">Сохранить изменения</button>
          <button type="button" class="btn" :disabled="busy" @click="clearEntryEditor">Отмена</button>
        </div>
      </form>
    </article>

    <article class="card">
      <div class="card-title-line">
        <h3>Фильтруемый поиск по выбранному справочнику</h3>
        <div class="inline-actions">
          <button class="btn" :disabled="!selectedDictionaryId" @click="addSearchRow">Добавить фильтр</button>
          <button class="btn primary" :disabled="!selectedDictionaryId || searching" @click="runSearch">Искать</button>
        </div>
      </div>

      <ul v-if="searchIssues.length > 0" class="issue-list">
        <li v-for="issue in searchIssues" :key="issue">{{ issue }}</li>
      </ul>

      <div class="search-rows">
        <div v-for="row in searchRows" :key="row.row_id" class="search-row">
          <label>
            Атрибут
            <select v-model="row.attribute" :disabled="!selectedDictionaryId">
              <option value="">Выберите атрибут</option>
              <option v-for="field in fields" :key="field.code" :value="field.code">
                {{ field.code }}
              </option>
            </select>
          </label>

          <label>
            Оператор
            <select v-model="row.op" :disabled="!selectedDictionaryId">
              <option v-for="option in SEARCH_OPS" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>

          <label v-if="row.op === 'in'" class="full">
            Values (через запятую/новую строку)
            <textarea v-model="row.values" class="code-area code-area-compact" :disabled="!selectedDictionaryId"></textarea>
          </label>

          <template v-else-if="row.op === 'range'">
            <label>
              From
              <input v-model="row.from" :disabled="!selectedDictionaryId" />
            </label>
            <label>
              To
              <input v-model="row.to" :disabled="!selectedDictionaryId" />
            </label>
          </template>

          <label v-else class="full">
            Value
            <input v-model="row.value" :disabled="!selectedDictionaryId" />
          </label>

          <div class="inline-actions">
            <button class="btn danger" :disabled="!selectedDictionaryId" @click="removeSearchRow(row.row_id)">Удалить</button>
          </div>
        </div>
      </div>

      <div class="search-toolbar">
        <label>
          Sort by
          <select v-model="searchSortAttribute" :disabled="!selectedDictionaryId">
            <option value="">Без сортировки</option>
            <option v-for="field in fields" :key="field.code" :value="field.code">
              {{ field.code }}
            </option>
          </select>
        </label>
        <label>
          Direction
          <select v-model="searchSortDirection" :disabled="!selectedDictionaryId">
            <option value="asc">asc</option>
            <option value="desc">desc</option>
          </select>
        </label>
        <label>
          Limit
          <input v-model.number="searchLimit" type="number" min="1" max="500" :disabled="!selectedDictionaryId" />
        </label>
      </div>

      <div class="card-title-line">
        <h3>Результаты поиска ({{ searchTotal }})</h3>
        <div class="pager">
          <button class="btn" :disabled="searchOffset === 0" @click="prevSearchPage">Назад</button>
          <span>{{ searchOffset + 1 }}-{{ Math.min(searchOffset + searchLimit, searchTotal) }}</span>
          <button class="btn" :disabled="searchOffset + searchLimit >= searchTotal" @click="nextSearchPage">Вперед</button>
        </div>
      </div>

      <div v-if="searchResult.length === 0" class="muted">Результатов пока нет.</div>
      <div v-for="entry in searchResult" :key="entry.id" class="result-item">
        <p><strong>{{ entry.external_key || entry.id }}</strong> <span class="muted">(v{{ entry.version }})</span></p>
        <JsonBox :value="entry.data" label="data" />
      </div>
    </article>
  </section>
</template>
