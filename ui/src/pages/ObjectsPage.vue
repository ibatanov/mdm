<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  ChevronLeft,
  ChevronRight,
  Pencil,
  Plus,
  RefreshCw,
  Settings2,
  SlidersHorizontal,
  Trash2,
  X,
} from 'lucide-vue-next'

import {
  type Attribute,
  type Dictionary,
  type Entry,
  type SchemaAttribute,
  type SearchFilter,
  type SearchRequest,
  mdmApi,
} from '../lib/api'
import { extractValidationIssues, formatError } from '../lib/errors'
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
  refDictionaryId?: string
  required: boolean
  isUnique: boolean
  isMultivalue: boolean
  validators: Record<string, unknown>
}

interface ReferenceOption {
  id: string
  label: string
  searchIndex: string
  dataSignature: string
}

const SEARCH_OPS: Array<{ value: SearchOp; label: string }> = [
  { value: 'eq', label: '=' },
  { value: 'ne', label: '!=' },
  { value: 'lt', label: '<' },
  { value: 'lte', label: '<=' },
  { value: 'gt', label: '>' },
  { value: 'gte', label: '>=' },
  { value: 'in', label: 'IN' },
  { value: 'contains', label: 'содержит' },
  { value: 'prefix', label: 'начинается с' },
  { value: 'range', label: 'диапазон' },
]

const route = useRoute()
const identity = useDevIdentityStore()

const loading = ref(false)
const busy = ref(false)
const searching = ref(false)
const error = ref('')
const message = ref('')

const dictionaries = ref<Dictionary[]>([])
const attributes = ref<Attribute[]>([])
const selectedDictionaryId = ref('')
const currentSchema = ref<SchemaAttribute[]>([])

const rows = ref<Entry[]>([])
const rowsTotal = ref(0)
const pageLimit = ref(20)
const pageOffset = ref(0)

const createModalOpen = ref(false)
const createExternalKey = ref('')
const createValues = ref<Record<string, string>>({})
const createReferenceSearch = ref<Record<string, string>>({})
const createIssues = ref<string[]>([])

const editEntryId = ref('')
const editValues = ref<Record<string, string>>({})
const editInitialValues = ref<Record<string, string>>({})
const editReferenceSearch = ref<Record<string, string>>({})
const editReferenceResolvedLabels = ref<Record<string, string>>({})
const editIssues = ref<string[]>([])

const searchRows = ref<SearchRowDraft[]>([])
const searchSortAttribute = ref('')
const searchSortDirection = ref<'asc' | 'desc'>('asc')
const searchIssues = ref<string[]>([])
const filtersApplied = ref(false)

const columnVisibility = ref<Record<string, boolean>>({})
const columnModalOpen = ref(false)
const referenceOptionsByField = ref<Record<string, ReferenceOption[]>>({})
const referenceOptionCacheByField = ref<Record<string, Record<string, ReferenceOption>>>({})
const referenceLoadingByField = ref<Record<string, boolean>>({})
const referenceErrorsByField = ref<Record<string, string>>({})
const referenceSearchableFieldsByDictionary = ref<Record<string, string[]>>({})
const referenceSearchSeqByField = ref<Record<string, number>>({})

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))
const pageCount = computed(() => Math.max(1, Math.ceil(rowsTotal.value / pageLimit.value)))
const currentPage = computed(() => Math.min(pageCount.value, Math.floor(pageOffset.value / pageLimit.value) + 1))

const selectedDictionary = computed(() =>
  dictionaries.value.find((dictionary) => dictionary.id === selectedDictionaryId.value) ?? null,
)

const dictionariesById = computed(() => {
  const map = new Map<string, Dictionary>()
  for (const dictionary of dictionaries.value) {
    map.set(dictionary.id, dictionary)
  }
  return map
})

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
      refDictionaryId: attribute.ref_dictionary_id,
      required: item.required,
      isUnique: item.is_unique,
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

const visibleFields = computed(() =>
  fields.value.filter((field) => {
    const visible = columnVisibility.value[field.code]
    return visible === undefined ? true : visible
  }),
)

watch(
  fields,
  () => {
    initializeCreateValues()
    initializeReferenceSearchMaps()
    syncSearchRows()
    initializeColumnVisibility()
  },
  { immediate: true },
)

watch(selectedDictionaryId, () => {
  pageOffset.value = 0
  clearEntryEditor()
  resetFiltersState()
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

function syncSearchRows(): void {
  if (fields.value.length === 0) {
    searchRows.value = []
    return
  }
  const allowed = new Set(fields.value.map((field) => field.code))
  searchRows.value = searchRows.value.map((row) => ({
    ...row,
    attribute: allowed.has(row.attribute) ? row.attribute : '',
  }))
}

function resetFiltersState(): void {
  searchRows.value = []
  searchIssues.value = []
  filtersApplied.value = false
}

function initializeColumnVisibility(): void {
  const next: Record<string, boolean> = {}

  for (let index = 0; index < fields.value.length; index += 1) {
    const field = fields.value[index]
    if (Object.prototype.hasOwnProperty.call(columnVisibility.value, field.code)) {
      next[field.code] = Boolean(columnVisibility.value[field.code])
    } else {
      next[field.code] = index < 6
    }
  }

  if (fields.value.length > 0 && !Object.values(next).some(Boolean)) {
    next[fields.value[0].code] = true
  }

  columnVisibility.value = next
}

function restoreColumnDefaults(): void {
  const next: Record<string, boolean> = {}
  for (let index = 0; index < fields.value.length; index += 1) {
    next[fields.value[index].code] = index < 6
  }
  if (fields.value.length > 0 && !Object.values(next).some(Boolean)) {
    next[fields.value[0].code] = true
  }
  columnVisibility.value = next
}

function showAllColumns(): void {
  const next: Record<string, boolean> = {}
  for (const field of fields.value) {
    next[field.code] = true
  }
  columnVisibility.value = next
}

function searchValuePlaceholder(row: SearchRowDraft): string {
  if (row.op === 'contains') {
    return 'Содержит...'
  }
  if (row.op === 'prefix') {
    return 'Начинается с...'
  }
  if (row.op === 'in') {
    return 'Значения через запятую'
  }
  return 'Значение'
}

function hasAnyFilterCondition(): boolean {
  return searchRows.value.some((row) => {
    const hasAttribute = row.attribute.trim() !== ''
    const hasValue =
      row.value.trim() !== '' || row.values.trim() !== '' || row.from.trim() !== '' || row.to.trim() !== ''
    return hasAttribute && hasValue
  })
}

function objectKeyLabel(entry: Entry): string {
  const key = entry.external_key?.trim()
  if (key) {
    return key
  }
  return `ID ${entry.id.slice(0, 8)}`
}

type PaginationItem = number | 'ellipsis-left' | 'ellipsis-right'

const paginationItems = computed<PaginationItem[]>(() => {
  const total = pageCount.value
  const current = currentPage.value

  if (total <= 7) {
    return Array.from({ length: total }, (_, index) => index + 1)
  }

  const items: PaginationItem[] = [1]
  const left = Math.max(2, current - 1)
  const right = Math.min(total - 1, current + 1)

  if (left > 2) {
    items.push('ellipsis-left')
  }
  for (let page = left; page <= right; page += 1) {
    items.push(page)
  }
  if (right < total - 1) {
    items.push('ellipsis-right')
  }
  items.push(total)

  return items
})

function sortIconFor(fieldCode: string) {
  if (searchSortAttribute.value !== fieldCode) {
    return ArrowUpDown
  }
  return searchSortDirection.value === 'asc' ? ArrowUp : ArrowDown
}

function toggleSort(fieldCode: string): void {
  if (!selectedDictionaryId.value) {
    return
  }

  if (searchSortAttribute.value !== fieldCode) {
    searchSortAttribute.value = fieldCode
    searchSortDirection.value = 'asc'
  } else if (searchSortDirection.value === 'asc') {
    searchSortDirection.value = 'desc'
  } else {
    searchSortAttribute.value = ''
    searchSortDirection.value = 'asc'
    if (!hasAnyFilterCondition()) {
      filtersApplied.value = false
    }
  }

  pageOffset.value = 0
  void refreshRows()
}

function toInputString(value: unknown, field: DictionaryField): string {
  if (value === undefined || value === null) {
    return ''
  }
  if (field.dataType === 'reference') {
    if (field.isMultivalue) {
      const ids = extractReferenceIds(value, field)
      return ids.join('\n')
    }
    const id = inferReferenceId(value, field)
    return id ?? ''
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

function extractReferenceId(value: unknown): string | undefined {
  if (typeof value === 'string') {
    const trimmed = value.trim()
    return trimmed === '' ? undefined : trimmed
  }

  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return undefined
  }

  const candidate = (value as Record<string, unknown>).id
  if (typeof candidate !== 'string') {
    return undefined
  }

  const trimmed = candidate.trim()
  return trimmed === '' ? undefined : trimmed
}

function stableValueSignature(value: unknown): string {
  if (value === null) {
    return 'null'
  }

  const valueType = typeof value
  if (valueType === 'string') {
    return `s:${value as string}`
  }
  if (valueType === 'number' || valueType === 'boolean') {
    return `${valueType[0]}:${String(value)}`
  }
  if (Array.isArray(value)) {
    const items = value.map((item) => stableValueSignature(item))
    return `[${items.join('|')}]`
  }
  if (!value || valueType !== 'object') {
    return `u:${String(value)}`
  }

  const record = value as Record<string, unknown>
  const keys = Object.keys(record).sort()
  const parts: string[] = []
  for (const key of keys) {
    parts.push(`${key}=${stableValueSignature(record[key])}`)
  }
  return `{${parts.join('|')}}`
}

function inferReferenceId(value: unknown, field: DictionaryField): string | undefined {
  const direct = extractReferenceId(value)
  if (direct) {
    return direct
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return undefined
  }

  const targetSignature = stableValueSignature(value)
  const knownOptions = referenceOptionsFor(field)
  const cached = Object.values(referenceOptionCacheByField.value[field.code] ?? {})
  const allOptions: ReferenceOption[] = []
  const seen = new Set<string>()
  for (const option of [...knownOptions, ...cached]) {
    if (seen.has(option.id)) {
      continue
    }
    seen.add(option.id)
    allOptions.push(option)
  }
  const matches = allOptions.filter((option) => option.dataSignature === targetSignature)
  if (matches.length === 1) {
    return matches[0].id
  }
  if (matches.length > 1) {
    return matches[0].id
  }

  return undefined
}

function extractReferenceIds(value: unknown, field: DictionaryField): string[] {
  if (!Array.isArray(value)) {
    return []
  }

  const ids: string[] = []
  for (const item of value) {
    const id = inferReferenceId(item, field)
    if (id) {
      ids.push(id)
    }
  }
  return ids
}

function referenceDisplayLabel(value: unknown): string {
  if (value === null || value === undefined) {
    return '—'
  }

  if (typeof value === 'string') {
    const trimmed = value.trim()
    return trimmed === '' ? '—' : trimmed
  }

  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return String(value)
  }

  const record = value as Record<string, unknown>
  const externalKey = typeof record.external_key === 'string' ? record.external_key.trim() : ''
  const id = typeof record.id === 'string' ? record.id.trim() : ''
  const code = typeof record.code === 'string' ? record.code.trim() : ''
  const name = typeof record.name === 'string' ? record.name.trim() : ''

  if (externalKey !== '' && id !== '') {
    return `${externalKey} (${id})`
  }
  if (externalKey !== '') {
    return externalKey
  }
  if (name !== '') {
    return name
  }
  if (code !== '') {
    return code
  }
  if (id !== '') {
    return id
  }

  try {
    const serialized = JSON.stringify(record)
    if (!serialized) {
      return '[object]'
    }
    return serialized.length > 120 ? `${serialized.slice(0, 117)}...` : serialized
  } catch {
    return '[object]'
  }
}

function formatEntryValue(value: unknown, field: DictionaryField): string {
  if (value === undefined || value === null) {
    return '—'
  }
  if (field.isMultivalue && Array.isArray(value)) {
    if (field.dataType === 'reference') {
      const tokens = value.map((item) => referenceDisplayLabel(item)).filter((item) => item.trim() !== '')
      return tokens.length > 0 ? tokens.join(', ') : '—'
    }
    return value.map((item) => String(item)).join(', ')
  }
  if (field.dataType === 'reference') {
    return referenceDisplayLabel(value)
  }
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false'
  }
  if (typeof value === 'object') {
    return JSON.stringify(value)
  }
  const text = String(value)
  return text.length > 120 ? `${text.slice(0, 117)}...` : text
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

function selectedReferenceIds(raw: string): string[] {
  return raw
    .split(/\n|,/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

function createSelectedReferenceIds(fieldCode: string): string[] {
  return selectedReferenceIds(createValues.value[fieldCode] ?? '')
}

function editSelectedReferenceIds(fieldCode: string): string[] {
  return selectedReferenceIds(editValues.value[fieldCode] ?? '')
}

function setReferenceSelection(target: 'create' | 'edit', fieldCode: string, values: string[]): void {
  const normalized = Array.from(new Set(values.map((item) => item.trim()).filter((item) => item !== '')))
  const serialized = normalized.join('\n')
  if (target === 'create') {
    createValues.value[fieldCode] = serialized
    return
  }
  editValues.value[fieldCode] = serialized
}

function toggleReferenceSelection(
  target: 'create' | 'edit',
  fieldCode: string,
  optionId: string,
  checked: boolean,
): void {
  const current = target === 'create' ? createSelectedReferenceIds(fieldCode) : editSelectedReferenceIds(fieldCode)
  if (checked) {
    setReferenceSelection(target, fieldCode, [...current, optionId])
    return
  }
  setReferenceSelection(
    target,
    fieldCode,
    current.filter((item) => item !== optionId),
  )
}

function isReferenceSelected(target: 'create' | 'edit', fieldCode: string, optionId: string): boolean {
  const current = target === 'create' ? createSelectedReferenceIds(fieldCode) : editSelectedReferenceIds(fieldCode)
  return current.includes(optionId)
}

function onCreateReferenceSearchInput(fieldCode: string): void {
  void searchReferenceOptionsForField(fieldCode, createReferenceSearch.value[fieldCode] ?? '')
}

function onEditReferenceSearchInput(fieldCode: string): void {
  void searchReferenceOptionsForField(fieldCode, editReferenceSearch.value[fieldCode] ?? '')
}

function initializeCreateValues(): void {
  const next: Record<string, string> = {}
  for (const field of fields.value) {
    next[field.code] = ''
  }
  createValues.value = next
  createIssues.value = []
}

function initializeReferenceSearchMaps(): void {
  const createSearch: Record<string, string> = {}
  const editSearch: Record<string, string> = {}
  for (const field of fields.value) {
    createSearch[field.code] = createReferenceSearch.value[field.code] ?? ''
    editSearch[field.code] = editReferenceSearch.value[field.code] ?? ''
  }
  createReferenceSearch.value = createSearch
  editReferenceSearch.value = editSearch
}

function makeReferenceOption(entry: Entry): ReferenceOption {
  const externalKey = entry.external_key?.trim() ?? ''
  const summary = referenceDisplayLabel(entry.data)
  const labelPrefix = externalKey !== '' ? externalKey : entry.id.slice(0, 8)
  const label = summary === '—' ? `${labelPrefix} (${entry.id})` : `${labelPrefix} (${entry.id}) — ${summary}`
  return {
    id: entry.id,
    label,
    searchIndex: `${entry.id} ${externalKey} ${summary}`.toLowerCase(),
    dataSignature: stableValueSignature(entry.data),
  }
}

function referenceOptionsFor(field: DictionaryField): ReferenceOption[] {
  return referenceOptionsByField.value[field.code] ?? []
}

function cacheReferenceOptions(fieldCode: string, options: ReferenceOption[]): void {
  if (!referenceOptionCacheByField.value[fieldCode]) {
    referenceOptionCacheByField.value[fieldCode] = {}
  }

  for (const option of options) {
    referenceOptionCacheByField.value[fieldCode][option.id] = option
  }
}

function cachedOrFallbackReferenceOption(fieldCode: string, optionId: string): ReferenceOption {
  const cache = referenceOptionCacheByField.value[fieldCode]
  if (cache?.[optionId]) {
    return cache[optionId]
  }

  return {
    id: optionId,
    label: optionId,
    searchIndex: optionId.toLowerCase(),
    dataSignature: '',
  }
}

function visibleReferenceOptionsFor(target: 'create' | 'edit', field: DictionaryField): ReferenceOption[] {
  const fieldCode = field.code
  const currentOptions = referenceOptionsFor(field)

  const selectedIds = target === 'create' ? createSelectedReferenceIds(fieldCode) : editSelectedReferenceIds(fieldCode)
  const selectedOptions = selectedIds.map((optionId) => {
    return currentOptions.find((option) => option.id === optionId) ?? cachedOrFallbackReferenceOption(fieldCode, optionId)
  })

  const merged = [...selectedOptions, ...currentOptions]
  const deduplicated: ReferenceOption[] = []
  const seen = new Set<string>()
  for (const option of merged) {
    if (seen.has(option.id)) {
      continue
    }
    seen.add(option.id)
    deduplicated.push(option)
  }

  return deduplicated
}

function referenceDictionaryLabel(field: DictionaryField): string {
  const dictionaryId = field.refDictionaryId
  if (!dictionaryId) {
    return 'не указан'
  }
  const dictionary = dictionariesById.value.get(dictionaryId)
  if (!dictionary) {
    return dictionaryId
  }
  return `${dictionary.code} — ${dictionary.name}`
}

function referenceLoading(field: DictionaryField): boolean {
  return Boolean(referenceLoadingByField.value[field.code])
}

function referenceError(field: DictionaryField): string {
  return referenceErrorsByField.value[field.code] ?? ''
}

function currentResolvedReference(fieldCode: string): string {
  return editReferenceResolvedLabels.value[fieldCode] ?? ''
}

function openCreateModal(): void {
  if (!selectedDictionaryId.value || !canWrite.value) {
    return
  }
  createExternalKey.value = ''
  initializeCreateValues()
  const nextSearch: Record<string, string> = {}
  for (const field of fields.value) {
    nextSearch[field.code] = ''
  }
  createReferenceSearch.value = nextSearch
  createIssues.value = []
  createModalOpen.value = true
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

  const nextValues: Record<string, string> = {}
  const nextSearch: Record<string, string> = {}
  const nextResolvedLabels: Record<string, string> = {}
  for (const field of fields.value) {
    nextValues[field.code] = toInputString(entry.data[field.code], field)
    nextSearch[field.code] = ''
    if (field.dataType === 'reference' && !field.isMultivalue) {
      const hasKnownId = nextValues[field.code].trim() !== ''
      if (!hasKnownId && entry.data[field.code] !== undefined && entry.data[field.code] !== null) {
        nextResolvedLabels[field.code] = referenceDisplayLabel(entry.data[field.code])
      }
    }
  }
  editValues.value = nextValues
  editInitialValues.value = { ...nextValues }
  editReferenceSearch.value = nextSearch
  editReferenceResolvedLabels.value = nextResolvedLabels
  editIssues.value = []
}

function clearEntryEditor(): void {
  editEntryId.value = ''
  editValues.value = {}
  editInitialValues.value = {}
  editReferenceSearch.value = {}
  editReferenceResolvedLabels.value = {}
  editIssues.value = []
}

function buildEditPatch(): { patch: Record<string, unknown>; issues: string[] } {
  const patch: Record<string, unknown> = {}
  const issues: string[] = []

  for (const field of fields.value) {
    const raw = editValues.value[field.code] ?? ''
    const initialRaw = editInitialValues.value[field.code] ?? ''
    if (raw === initialRaw) {
      continue
    }

    try {
      const parsed = parseFieldValue(raw, field)
      if (parsed === undefined) {
        if (field.required) {
          issues.push(`Поле "${field.name}" обязательно`)
          continue
        }
        patch[field.code] = null
        continue
      }
      patch[field.code] = parsed
    } catch (err) {
      issues.push(formatError(err))
    }
  }

  return { patch, issues }
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

function validatorHints(field: DictionaryField): string[] {
  const hints: string[] = []

  hints.push(`Тип: ${field.dataType}${field.isMultivalue ? ', несколько значений' : ''}`)
  if (field.required) {
    hints.push('Обязательное')
  }
  if (field.isUnique) {
    hints.push('Уникальное')
  }

  const pushNumberHint = (key: string, label: string) => {
    const value = field.validators[key]
    if (typeof value === 'number' && Number.isFinite(value)) {
      hints.push(`${label}: ${value}`)
    }
  }
  const pushStringHint = (key: string, label: string) => {
    const value = field.validators[key]
    if (typeof value === 'string' && value.trim() !== '') {
      hints.push(`${label}: ${value.trim()}`)
    }
  }

  pushNumberHint('min', 'Мин')
  pushNumberHint('max', 'Макс')
  pushNumberHint('min_length', 'Мин. длина')
  pushNumberHint('max_length', 'Макс. длина')
  pushNumberHint('min_items', 'Мин. элементов')
  pushNumberHint('max_items', 'Макс. элементов')
  pushStringHint('min_date', 'Дата от')
  pushStringHint('max_date', 'Дата до')
  pushStringHint('pattern', 'Формат')

  const allowed = field.validators.allowed_values
  if (Array.isArray(allowed)) {
    const values = allowed
      .filter((item): item is string => typeof item === 'string')
      .map((item) => item.trim())
      .filter((item) => item.length > 0)
    if (values.length > 0) {
      hints.push(`Допустимо: ${values.join(', ')}`)
    }
  }

  return hints
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

    const rowEmpty =
      row.attribute.trim() === '' &&
      row.value.trim() === '' &&
      row.values.trim() === '' &&
      row.from.trim() === '' &&
      row.to.trim() === ''

    if (rowEmpty) {
      continue
    }

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
            issues.push(`Фильтр ${rowNumber}: для диапазона заполните поля «От» и «До»`)
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
      limit: pageLimit.value,
      offset: pageOffset.value,
    },
  }

  return { body, issues }
}

async function ensureReferenceSearchableFields(referenceDictionaryId: string): Promise<string[]> {
  const cached = referenceSearchableFieldsByDictionary.value[referenceDictionaryId]
  if (cached) {
    return cached
  }

  const schemaResult = await mdmApi.getDictionarySchema(referenceDictionaryId)
  const searchable = schemaResult.attributes
    .map((row) => attributesById.value.get(row.attribute_id))
    .filter((attribute): attribute is Attribute => Boolean(attribute))
    .filter((attribute) => attribute.data_type === 'string' || attribute.data_type === 'enum')
    .map((attribute) => attribute.code)

  referenceSearchableFieldsByDictionary.value[referenceDictionaryId] = searchable
  return searchable
}

function pickReferenceSearchAttribute(searchableFields: string[]): string | null {
  if (searchableFields.length === 0) {
    return null
  }

  const preferred = ['name', 'title', 'code', 'article']
  for (const key of preferred) {
    if (searchableFields.includes(key)) {
      return key
    }
  }
  return searchableFields[0]
}

async function searchReferenceOptionsForField(fieldCode: string, query: string): Promise<void> {
  const field = fieldsByCode.value.get(fieldCode)
  if (!field || field.dataType !== 'reference' || !field.refDictionaryId) {
    referenceOptionsByField.value[fieldCode] = []
    return
  }

  const normalizedQuery = query.trim()
  const seq = (referenceSearchSeqByField.value[fieldCode] ?? 0) + 1
  referenceSearchSeqByField.value[fieldCode] = seq
  referenceLoadingByField.value[fieldCode] = true
  referenceErrorsByField.value[fieldCode] = ''

  try {
    let items: Entry[] = []
    if (normalizedQuery === '') {
      const result = await mdmApi.listEntries(field.refDictionaryId, 30, 0)
      items = result.items
    } else {
      const searchableFields = await ensureReferenceSearchableFields(field.refDictionaryId)
      if (searchableFields.length === 0) {
        const result = await mdmApi.listEntries(field.refDictionaryId, 30, 0)
        items = result.items.filter((entry) => {
          const option = makeReferenceOption(entry)
          return option.searchIndex.includes(normalizedQuery.toLowerCase())
        })
      } else {
        const searchAttribute = pickReferenceSearchAttribute(searchableFields)
        if (searchAttribute) {
          const result = await mdmApi.searchEntries(field.refDictionaryId, {
            filters: [{ attribute: searchAttribute, op: 'contains', value: normalizedQuery }],
            page: { limit: 40, offset: 0 },
          })
          items = result.items
        }
      }
    }

    if (referenceSearchSeqByField.value[fieldCode] !== seq) {
      return
    }
    const options = items.map(makeReferenceOption)
    cacheReferenceOptions(fieldCode, options)
    referenceOptionsByField.value[fieldCode] = options
    referenceErrorsByField.value[fieldCode] = ''
  } catch (err) {
    if (referenceSearchSeqByField.value[fieldCode] !== seq) {
      return
    }
    referenceOptionsByField.value[fieldCode] = []
    referenceErrorsByField.value[fieldCode] = formatError(err)
  } finally {
    if (referenceSearchSeqByField.value[fieldCode] === seq) {
      referenceLoadingByField.value[fieldCode] = false
    }
  }
}

async function refreshReferenceLookupsForCurrentSchema(): Promise<void> {
  const referenceFields = fields.value.filter((field) => field.dataType === 'reference' && Boolean(field.refDictionaryId))
  const nextOptions: Record<string, ReferenceOption[]> = {}
  const nextCache: Record<string, Record<string, ReferenceOption>> = {}
  const nextLoading: Record<string, boolean> = {}
  const nextErrors: Record<string, string> = {}
  const nextSeq: Record<string, number> = {}

  for (const field of referenceFields) {
    nextOptions[field.code] = referenceOptionsByField.value[field.code] ?? []
    nextCache[field.code] = referenceOptionCacheByField.value[field.code] ?? {}
    nextLoading[field.code] = false
    nextErrors[field.code] = ''
    nextSeq[field.code] = referenceSearchSeqByField.value[field.code] ?? 0
  }

  referenceOptionsByField.value = nextOptions
  referenceOptionCacheByField.value = nextCache
  referenceLoadingByField.value = nextLoading
  referenceErrorsByField.value = nextErrors
  referenceSearchSeqByField.value = nextSeq

  await Promise.all(
    referenceFields.map((field) => searchReferenceOptionsForField(field.code, '')),
  )
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
    const targetDictionaryId =
      exists ? queryDictionaryId : selectedDictionaryId.value || dictionaries.value[0]?.id || ''

    if (!targetDictionaryId) {
      await loadDictionaryWorkspace()
      return
    }

    if (selectedDictionaryId.value !== targetDictionaryId) {
      selectedDictionaryId.value = targetDictionaryId
      return
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
    rows.value = []
    rowsTotal.value = 0
    referenceOptionsByField.value = {}
    referenceOptionCacheByField.value = {}
    referenceLoadingByField.value = {}
    referenceErrorsByField.value = {}
    referenceSearchSeqByField.value = {}
    return
  }

  try {
    const schemaResult = await mdmApi.getDictionarySchema(selectedDictionaryId.value)
    currentSchema.value = schemaResult.attributes
    await refreshReferenceLookupsForCurrentSchema()
    await refreshRows()
  } catch (err) {
    error.value = formatError(err)
  }
}

async function loadRowsList(): Promise<void> {
  if (!selectedDictionaryId.value) {
    rows.value = []
    rowsTotal.value = 0
    return
  }

  const result = await mdmApi.listEntries(selectedDictionaryId.value, pageLimit.value, pageOffset.value)
  rows.value = result.items
  rowsTotal.value = result.total
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
    rows.value = result.items
    rowsTotal.value = result.total
    filtersApplied.value = (built.body.filters?.length ?? 0) > 0
  } catch (err) {
    error.value = formatError(err)
  } finally {
    searching.value = false
  }
}

async function refreshRows(): Promise<void> {
  if (filtersApplied.value || Boolean(searchSortAttribute.value)) {
    await runSearch()
    return
  }
  await loadRowsList()
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

    createModalOpen.value = false
    createExternalKey.value = ''
    initializeCreateValues()
    await refreshRows()
    message.value = 'Объект создан'
  } catch (err) {
    const validationIssues = extractValidationIssues(err)
    if (validationIssues.length > 0) {
      createIssues.value = validationIssues
      return
    }
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
    const built = buildEditPatch()
    if (built.issues.length > 0) {
      editIssues.value = built.issues
      return
    }

    const patch = built.patch
    if (Object.keys(patch).length === 0) {
      message.value = 'Изменений нет'
      return
    }

    await mdmApi.updateEntry(selectedDictionaryId.value, editEntryId.value, patch)
    clearEntryEditor()
    await refreshRows()
    message.value = 'Объект обновлен'
  } catch (err) {
    const validationIssues = extractValidationIssues(err)
    if (validationIssues.length > 0) {
      editIssues.value = validationIssues
      return
    }
    error.value = formatError(err)
  } finally {
    busy.value = false
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
    if (editEntryId.value === entryId) {
      clearEntryEditor()
    }
    await refreshRows()
    message.value = 'Объект удален'
  } catch (err) {
    error.value = formatError(err)
  } finally {
    busy.value = false
  }
}

function prevPage(): void {
  if (pageOffset.value === 0) {
    return
  }
  pageOffset.value = Math.max(0, pageOffset.value - pageLimit.value)
  void refreshRows()
}

function nextPage(): void {
  if (pageOffset.value + pageLimit.value >= rowsTotal.value) {
    return
  }
  pageOffset.value += pageLimit.value
  void refreshRows()
}

function goToPage(page: number): void {
  if (page < 1 || page > pageCount.value || page === currentPage.value) {
    return
  }
  pageOffset.value = (page - 1) * pageLimit.value
  void refreshRows()
}

function applyPageSize(): void {
  pageOffset.value = 0
  void refreshRows()
}

function addSearchRow(): void {
  searchRows.value.push(makeSearchRow())
}

function removeSearchRow(rowId: string): void {
  searchRows.value = searchRows.value.filter((row) => row.row_id !== rowId)
}

function applyFilters(): void {
  pageOffset.value = 0
  void runSearch()
}

function resetFilters(): void {
  pageOffset.value = 0
  resetFiltersState()
  void loadRowsList()
}

onMounted(loadBootData)
</script>

<template>
  <section>
    <div class="section-head">
      <div>
        <h1>Объекты</h1>
        <p class="muted">Создание, поиск, редактирование и отображение объектов выбранного справочника.</p>
      </div>
    </div>

    <p v-if="message" class="alert success">{{ message }}</p>
    <p v-if="error" class="alert error">{{ error }}</p>

    <article class="card">
      <div class="workspace-row workspace-row-compact">
        <select v-model="selectedDictionaryId" class="workspace-select-control" aria-label="Справочник">
          <option value="">Выберите справочник</option>
          <option v-for="dictionary in dictionaries" :key="dictionary.id" :value="dictionary.id">
            {{ dictionary.code }} — {{ dictionary.name }}
          </option>
        </select>

        <div class="workspace-actions workspace-actions-compact">
          <RouterLink
            v-if="selectedDictionary"
            class="btn btn-icon-only"
            :to="`/dictionaries/${selectedDictionary.id}`"
            title="Настроить справочник"
          >
            <Settings2 :size="16" aria-hidden="true" />
            <span class="sr-only">Настроить справочник</span>
          </RouterLink>
          <button
            class="btn primary btn-icon-only"
            title="Создать объект"
            :disabled="!selectedDictionaryId || !canWrite"
            @click="openCreateModal"
          >
            <Plus :size="16" aria-hidden="true" />
            <span class="sr-only">Создать объект</span>
          </button>
        </div>
      </div>
    </article>

    <article class="card">
      <div class="card-title-line objects-header-line">
        <h3>Объекты выбранного справочника ({{ rowsTotal }})</h3>
        <div class="inline-actions compact-actions objects-header-tools">
          <button class="btn btn-icon-only" title="Добавить фильтр" :disabled="!selectedDictionaryId" @click="addSearchRow">
            <Plus :size="16" aria-hidden="true" />
            <span class="sr-only">Добавить фильтр</span>
          </button>
          <button class="btn primary btn-icon-only" title="Применить фильтры" :disabled="!selectedDictionaryId || searching" @click="applyFilters">
            <RefreshCw :size="16" aria-hidden="true" />
            <span class="sr-only">Применить фильтры</span>
          </button>
          <button class="btn btn-icon-only" title="Сбросить фильтры" :disabled="!selectedDictionaryId" @click="resetFilters">
            <X :size="16" aria-hidden="true" />
            <span class="sr-only">Сбросить фильтры</span>
          </button>
          <button class="btn btn-icon-only" title="Колонки таблицы" :disabled="!selectedDictionaryId" @click="columnModalOpen = true">
            <SlidersHorizontal :size="16" aria-hidden="true" />
            <span class="sr-only">Колонки таблицы</span>
          </button>
        </div>
      </div>

      <div class="filters-area">
        <ul v-if="searchIssues.length > 0" class="issue-list">
          <li v-for="issue in searchIssues" :key="issue">{{ issue }}</li>
        </ul>

        <div v-if="searchRows.length > 0" class="search-rows">
          <div v-for="row in searchRows" :key="row.row_id" class="search-row search-row-inline">
            <select v-model="row.attribute" :disabled="!selectedDictionaryId" aria-label="Атрибут фильтра">
              <option value="">Выберите атрибут</option>
              <option v-for="field in fields" :key="field.code" :value="field.code">{{ field.code }}</option>
            </select>

            <select v-model="row.op" :disabled="!selectedDictionaryId" aria-label="Оператор фильтра">
              <option v-for="option in SEARCH_OPS" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>

            <template v-if="row.op === 'range'">
              <div class="search-range">
                <input v-model="row.from" placeholder="От" :disabled="!selectedDictionaryId" aria-label="Значение от" />
                <input v-model="row.to" placeholder="До" :disabled="!selectedDictionaryId" aria-label="Значение до" />
              </div>
            </template>
            <template v-else>
              <input
                v-if="row.op === 'in'"
                v-model="row.values"
                placeholder="Например: a,b,c"
                :disabled="!selectedDictionaryId"
                aria-label="Значения фильтра"
              />
              <input
                v-else
                v-model="row.value"
                :placeholder="searchValuePlaceholder(row)"
                :disabled="!selectedDictionaryId"
                aria-label="Значение фильтра"
              />
            </template>

            <button
              class="btn danger btn-icon-only"
              title="Удалить фильтр"
              :disabled="!selectedDictionaryId"
              @click="removeSearchRow(row.row_id)"
            >
              <Trash2 :size="16" aria-hidden="true" />
              <span class="sr-only">Удалить фильтр</span>
            </button>
          </div>
        </div>
      </div>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Ключ объекта</th>
              <th v-for="field in visibleFields" :key="field.attributeId">
                <button class="th-sort" :disabled="!selectedDictionaryId" @click="toggleSort(field.code)">
                  {{ field.name }}
                  <component :is="sortIconFor(field.code)" :size="14" aria-hidden="true" />
                </button>
              </th>
              <th>Версия</th>
              <th class="actions">Действия</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading || searching">
              <td :colspan="visibleFields.length + 3" class="muted">Загрузка...</td>
            </tr>
            <tr v-if="!loading && !searching && rows.length === 0">
              <td :colspan="visibleFields.length + 3" class="muted">Нет объектов для отображения.</td>
            </tr>
            <tr v-for="entry in rows" :key="entry.id">
              <td>
                <span :title="entry.external_key || `Системный ID: ${entry.id}`">{{ objectKeyLabel(entry) }}</span>
              </td>
              <td v-for="field in visibleFields" :key="`${entry.id}:${field.attributeId}`">{{ formatEntryValue(entry.data[field.code], field) }}</td>
              <td>{{ entry.version }}</td>
              <td class="actions-row">
                <button
                  class="btn btn-icon-only"
                  title="Изменить объект"
                  :disabled="!canWrite || !selectedDictionaryId"
                  @click="startEditEntry(entry)"
                >
                  <Pencil :size="16" aria-hidden="true" />
                  <span class="sr-only">Изменить объект</span>
                </button>
                <button
                  class="btn danger btn-icon-only"
                  title="Удалить объект"
                  :disabled="!canWrite || !selectedDictionaryId"
                  @click="removeEntry(entry.id)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                  <span class="sr-only">Удалить объект</span>
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="table-pagination objects-pagination">
        <nav class="pagination-nav objects-pagination-nav" role="navigation" aria-label="pagination">
          <ul class="pagination-list">
            <li>
              <button class="pagination-link pagination-edge" :disabled="pageOffset === 0" @click="prevPage">
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
              <button class="pagination-link pagination-edge" :disabled="pageOffset + pageLimit >= rowsTotal" @click="nextPage">
                <span class="pagination-edge-text">Вперед</span>
                <ChevronRight :size="16" aria-hidden="true" />
              </button>
            </li>
          </ul>
        </nav>

        <label class="pagination-size objects-pagination-size">
          На странице
          <select v-model.number="pageLimit" @change="applyPageSize" :disabled="!selectedDictionaryId">
            <option :value="20">20</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
          </select>
        </label>
      </div>
    </article>

    <div v-if="columnModalOpen" class="modal-backdrop">
      <article class="modal-card modal-card-compact">
        <div class="card-title-line">
          <h3>Колонки таблицы</h3>
          <button type="button" class="btn btn-icon-only" title="Закрыть" @click="columnModalOpen = false">
            <X :size="16" aria-hidden="true" />
            <span class="sr-only">Закрыть</span>
          </button>
        </div>

        <div class="inline-actions">
          <button type="button" class="btn" @click="showAllColumns">Показать все</button>
          <button type="button" class="btn" @click="restoreColumnDefaults">По умолчанию</button>
        </div>

        <div class="column-grid column-grid-modal">
          <label v-for="field in fields" :key="field.attributeId" class="check">
            <input v-model="columnVisibility[field.code]" type="checkbox" />
            {{ field.name }} ({{ field.code }})
          </label>
        </div>
      </article>
    </div>

    <div v-if="createModalOpen" class="modal-backdrop">
      <article class="modal-card" :class="{ 'is-disabled': !canWrite }">
        <div class="card-title-line">
          <h3>Создание объекта</h3>
          <button type="button" class="btn btn-icon-only" title="Закрыть" :disabled="busy" @click="createModalOpen = false">
            <X :size="16" aria-hidden="true" />
            <span class="sr-only">Закрыть</span>
          </button>
        </div>

        <ul v-if="createIssues.length > 0" class="issue-list">
          <li v-for="issue in createIssues" :key="issue">{{ issue }}</li>
        </ul>

        <form class="entry-grid" @submit.prevent="createEntryFromForm">
          <label>
            Внешний ключ (external_key)
            <input v-model="createExternalKey" placeholder="SKU-001" :disabled="!selectedDictionaryId || !canWrite || busy" />
          </label>

          <div v-for="field in fields" :key="field.attributeId" class="entry-field">
            <label>
              <span class="field-title">
                {{ field.name }} <span class="muted">({{ field.code }})</span>
                <span v-if="field.required" class="required-mark">*</span>
              </span>
              <template v-if="field.dataType === 'reference' && field.isMultivalue">
                <div class="reference-control">
                  <input
                    v-model="createReferenceSearch[field.code]"
                    placeholder="Поиск по id / external_key / данным"
                    :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)"
                    @input="onCreateReferenceSearchInput(field.code)"
                  />
                  <div class="reference-options-list">
                    <label
                      v-for="option in visibleReferenceOptionsFor('create', field)"
                      :key="option.id"
                      class="check reference-option"
                    >
                      <input
                        type="checkbox"
                        :checked="isReferenceSelected('create', field.code, option.id)"
                        :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)"
                        @change="
                          toggleReferenceSelection(
                            'create',
                            field.code,
                            option.id,
                            ($event.target as HTMLInputElement).checked,
                          )
                        "
                      />
                      {{ option.label }}
                    </label>
                    <span
                      v-if="visibleReferenceOptionsFor('create', field).length === 0"
                      class="muted"
                    >
                      Нет значений по фильтру
                    </span>
                  </div>
                </div>
                <span class="validator-hints reference-inline-hint">
                  Выбрано: {{ createSelectedReferenceIds(field.code).length }}
                </span>
                <span v-if="referenceLoading(field)" class="validator-hints reference-inline-hint">
                  Загрузка ссылочных значений ({{ referenceDictionaryLabel(field) }})...
                </span>
                <span v-else-if="referenceError(field)" class="validator-hints reference-inline-hint">
                  Не удалось загрузить ссылочные значения: {{ referenceError(field) }}
                </span>
              </template>
              <template v-else-if="field.isMultivalue">
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
              <template v-else-if="field.dataType === 'reference'">
                <div class="reference-control">
                  <input
                    v-model="createReferenceSearch[field.code]"
                    placeholder="Поиск по id / external_key / данным"
                    :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)"
                    @input="onCreateReferenceSearchInput(field.code)"
                  />
                  <select v-model="createValues[field.code]" :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)">
                    <option value="">—</option>
                    <option
                      v-for="option in visibleReferenceOptionsFor('create', field)"
                      :key="option.id"
                      :value="option.id"
                    >
                      {{ option.label }}
                    </option>
                  </select>
                </div>
                <span v-if="referenceLoading(field)" class="validator-hints reference-inline-hint">
                  Загрузка ссылочных значений ({{ referenceDictionaryLabel(field) }})...
                </span>
                <span v-else-if="referenceError(field)" class="validator-hints reference-inline-hint">
                  Не удалось загрузить ссылочные значения: {{ referenceError(field) }}
                </span>
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
            <p class="validator-hints">{{ validatorHints(field).join(' · ') }}</p>
          </div>

          <div class="form-actions">
            <button class="btn primary" :disabled="!selectedDictionaryId || !canWrite || busy || fields.length === 0">
              <Plus class="btn-icon" :size="16" aria-hidden="true" />
              Создать объект
            </button>
            <button type="button" class="btn" :disabled="busy" @click="createModalOpen = false">
              <X class="btn-icon" :size="16" aria-hidden="true" />
              Отмена
            </button>
          </div>
        </form>
      </article>
    </div>

    <div v-if="editEntryId" class="modal-backdrop">
      <article class="modal-card" :class="{ 'is-disabled': !canWrite }">
        <div class="card-title-line">
          <h3>Редактирование объекта</h3>
          <div class="inline-actions">
            <span class="pill"><code>{{ editEntryId }}</code></span>
            <button type="button" class="btn btn-icon-only" title="Закрыть" :disabled="busy" @click="clearEntryEditor">
              <X :size="16" aria-hidden="true" />
              <span class="sr-only">Закрыть</span>
            </button>
          </div>
        </div>

        <ul v-if="editIssues.length > 0" class="issue-list">
          <li v-for="issue in editIssues" :key="issue">{{ issue }}</li>
        </ul>
        <p class="muted modal-note">
          Чтобы очистить необязательное поле, оставьте его пустым и сохраните изменения.
        </p>

        <form class="entry-grid" @submit.prevent="saveEntryEdit">
          <div v-for="field in fields" :key="field.attributeId" class="entry-field">
            <label>
              <span class="field-title">
                {{ field.name }} <span class="muted">({{ field.code }})</span>
                <span v-if="field.required" class="required-mark">*</span>
              </span>
              <template v-if="field.dataType === 'reference' && field.isMultivalue">
                <div class="reference-control">
                  <input
                    v-model="editReferenceSearch[field.code]"
                    placeholder="Поиск по id / external_key / данным"
                    :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)"
                    @input="onEditReferenceSearchInput(field.code)"
                  />
                  <div class="reference-options-list">
                    <label
                      v-for="option in visibleReferenceOptionsFor('edit', field)"
                      :key="option.id"
                      class="check reference-option"
                    >
                      <input
                        type="checkbox"
                        :checked="isReferenceSelected('edit', field.code, option.id)"
                        :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)"
                        @change="
                          toggleReferenceSelection(
                            'edit',
                            field.code,
                            option.id,
                            ($event.target as HTMLInputElement).checked,
                          )
                        "
                      />
                      {{ option.label }}
                    </label>
                    <span
                      v-if="visibleReferenceOptionsFor('edit', field).length === 0"
                      class="muted"
                    >
                      Нет значений по фильтру
                    </span>
                  </div>
                </div>
                <span class="validator-hints reference-inline-hint">
                  Выбрано: {{ editSelectedReferenceIds(field.code).length }}
                </span>
                <span v-if="referenceLoading(field)" class="validator-hints reference-inline-hint">
                  Загрузка ссылочных значений ({{ referenceDictionaryLabel(field) }})...
                </span>
                <span v-else-if="referenceError(field)" class="validator-hints reference-inline-hint">
                  Не удалось загрузить ссылочные значения: {{ referenceError(field) }}
                </span>
              </template>
              <template v-else-if="field.isMultivalue">
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
              <template v-else-if="field.dataType === 'reference'">
                <div class="reference-control">
                  <input
                    v-model="editReferenceSearch[field.code]"
                    placeholder="Поиск по id / external_key / данным"
                    :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)"
                    @input="onEditReferenceSearchInput(field.code)"
                  />
                  <select v-model="editValues[field.code]" :disabled="!selectedDictionaryId || !canWrite || busy || referenceLoading(field)">
                    <option value="">—</option>
                    <option
                      v-for="option in visibleReferenceOptionsFor('edit', field)"
                      :key="option.id"
                      :value="option.id"
                    >
                      {{ option.label }}
                    </option>
                  </select>
                </div>
                <span
                  v-if="currentResolvedReference(field.code) && (editValues[field.code] ?? '').trim() === ''"
                  class="validator-hints reference-inline-hint"
                >
                  Текущее значение: {{ currentResolvedReference(field.code) }}
                </span>
                <span v-if="referenceLoading(field)" class="validator-hints reference-inline-hint">
                  Загрузка ссылочных значений ({{ referenceDictionaryLabel(field) }})...
                </span>
                <span v-else-if="referenceError(field)" class="validator-hints reference-inline-hint">
                  Не удалось загрузить ссылочные значения: {{ referenceError(field) }}
                </span>
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
            <p class="validator-hints">{{ validatorHints(field).join(' · ') }}</p>
          </div>

          <div class="form-actions">
            <button class="btn primary" :disabled="!selectedDictionaryId || !canWrite || busy">
              <Pencil class="btn-icon" :size="16" aria-hidden="true" />
              Сохранить изменения
            </button>
            <button type="button" class="btn" :disabled="busy" @click="clearEntryEditor">
              <X class="btn-icon" :size="16" aria-hidden="true" />
              Отмена
            </button>
          </div>
        </form>
      </article>
    </div>
  </section>
</template>
