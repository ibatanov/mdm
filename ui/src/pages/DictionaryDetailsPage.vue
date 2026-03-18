<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Pencil, Plus, RefreshCw, Save, Settings2, Trash2, X } from 'lucide-vue-next'

import { type Attribute, type Dictionary, type SchemaAttribute, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

interface SchemaDraftRow {
    row_id: string
    attribute_id: string
    required: boolean
    is_unique: boolean
    is_multivalue: boolean
    position: number
    default_value?: unknown
    validators?: Record<string, unknown>
}

type EditorMode = 'create' | 'edit'

interface ReferenceDefaultOption {
    id: string
    label: string
}

const route = useRoute()
const identity = useDevIdentityStore()

const loading = ref(false)
const busy = ref(false)
const error = ref('')
const message = ref('')

const dictionary = ref<Dictionary | null>(null)
const detailsForm = reactive({
    name: '',
    description: '',
})

const attributes = ref<Attribute[]>([])
const currentSchema = ref<SchemaAttribute[]>([])
const schemaRows = ref<SchemaDraftRow[]>([])

const editorOpen = ref(false)
const editorIssues = ref<string[]>([])
const editor = reactive({
    mode: 'create' as EditorMode,
    row_id: '',
    attribute_id: '',
    required: false,
    is_unique: false,
    is_multivalue: false,
    position: 10,
    default_text: '',
    default_boolean: '',
    min: '',
    max: '',
    min_length: '',
    max_length: '',
    pattern: '',
    allowed_values_text: '',
    min_date: '',
    max_date: '',
    min_items: '',
    max_items: '',
})

const dictionaryId = computed(() => String(route.params.dictionaryId ?? '').trim())
const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))

const sortedAttributes = computed(() => [...attributes.value].sort((a, b) => a.code.localeCompare(b.code, 'ru')))

const attributesById = computed(() => {
    const map = new Map<string, Attribute>()
    for (const attribute of attributes.value) {
        map.set(attribute.id, attribute)
    }
    return map
})

const schemaTableRows = computed(() =>
    [...schemaRows.value]
        .sort((a, b) => a.position - b.position)
        .map((row) => ({
            ...row,
            attribute: attributesById.value.get(row.attribute_id) ?? null,
        })),
)

const editorAttribute = computed(() => attributesById.value.get(editor.attribute_id) ?? null)
const editorType = computed<Attribute['data_type']>(() => editorAttribute.value?.data_type ?? 'string')
const editorReferenceDictionaryId = computed(() => editorAttribute.value?.ref_dictionary_id ?? '')

const referenceDefaultOptions = ref<ReferenceDefaultOption[]>([])
const referenceDefaultOptionCache = ref<Record<string, ReferenceDefaultOption>>({})
const referenceDefaultLoading = ref(false)
const referenceDefaultError = ref('')
const referenceDefaultSearch = ref('')
const referenceDefaultSearchableFieldsByDictionary = ref<Record<string, string[]>>({})
const referenceDefaultSearchSeq = ref(0)

watch(dictionaryId, (next, prev) => {
    if (!next || next === prev) {
        return
    }
    void loadWorkspace()
})

watch(
    [editorOpen, editorType, () => editor.is_multivalue, editorReferenceDictionaryId],
    async ([isOpen, type, , refDictionaryId]) => {
        if (!isOpen || type !== 'reference' || !refDictionaryId) {
            referenceDefaultOptions.value = []
            referenceDefaultOptionCache.value = {}
            referenceDefaultError.value = ''
            referenceDefaultLoading.value = false
            referenceDefaultSearch.value = ''
            return
        }
        await searchReferenceDefaultOptions(refDictionaryId, referenceDefaultSearch.value)
    },
)

watch(referenceDefaultSearch, (query) => {
    if (!editorOpen.value || editorType.value !== 'reference' || !editorReferenceDictionaryId.value) {
        return
    }
    void searchReferenceDefaultOptions(editorReferenceDictionaryId.value, query)
})

watch(
    () => editor.is_multivalue,
    (isMultivalue) => {
        if (isMultivalue) {
            editor.is_unique = false
        }
    },
)

function clearFeedback(): void {
    error.value = ''
    message.value = ''
}

function asRecord(value: unknown): Record<string, unknown> {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return {}
    }
    return value as Record<string, unknown>
}

function syncDetailsForm(): void {
    detailsForm.name = dictionary.value?.name ?? ''
    detailsForm.description = dictionary.value?.description ?? ''
}

function syncSchemaRows(): void {
    schemaRows.value = currentSchema.value.map((item) => ({
        row_id: crypto.randomUUID(),
        attribute_id: item.attribute_id,
        required: item.required,
        is_unique: item.is_unique,
        is_multivalue: item.is_multivalue,
        position: item.position,
        default_value: item.default_value,
        validators: item.validators ? { ...item.validators } : undefined,
    }))
}

function summarizeReferenceData(value: unknown): string {
    if (value === null || value === undefined) {
        return '—'
    }
    if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
        return String(value)
    }
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return String(value)
    }
    const data = value as Record<string, unknown>
    const preferredKeys = ['name', 'title', 'code', 'article']
    for (const key of preferredKeys) {
        const candidate = data[key]
        if (typeof candidate === 'string' && candidate.trim() !== '') {
            return candidate.trim()
        }
    }
    for (const candidate of Object.values(data)) {
        if (typeof candidate === 'string' && candidate.trim() !== '') {
            return candidate.trim()
        }
    }
    return '—'
}

async function ensureReferenceDefaultSearchableFields(referenceDictionaryId: string): Promise<string[]> {
    const cached = referenceDefaultSearchableFieldsByDictionary.value[referenceDictionaryId]
    if (cached) {
        return cached
    }

    const schemaResult = await mdmApi.getDictionarySchema(referenceDictionaryId)
    const searchable = schemaResult.attributes
        .map((row) => attributesById.value.get(row.attribute_id))
        .filter((attribute): attribute is Attribute => Boolean(attribute))
        .filter((attribute) => attribute.data_type === 'string' || attribute.data_type === 'enum')
        .map((attribute) => attribute.code)

    referenceDefaultSearchableFieldsByDictionary.value[referenceDictionaryId] = searchable
    return searchable
}

function pickReferenceDefaultSearchAttribute(searchableFields: string[]): string | null {
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

async function searchReferenceDefaultOptions(referenceDictionaryId: string, query: string): Promise<void> {
    const normalizedQuery = query.trim()
    const seq = referenceDefaultSearchSeq.value + 1
    referenceDefaultSearchSeq.value = seq
    referenceDefaultLoading.value = true
    referenceDefaultError.value = ''

    try {
        let items: Array<{ id: string; external_key?: string; data: Record<string, unknown> }> = []
        if (normalizedQuery === '') {
            const result = await mdmApi.listEntries(referenceDictionaryId, 30, 0)
            items = result.items
        } else {
            const searchableFields = await ensureReferenceDefaultSearchableFields(referenceDictionaryId)
            if (searchableFields.length === 0) {
                const result = await mdmApi.listEntries(referenceDictionaryId, 30, 0)
                const lowered = normalizedQuery.toLowerCase()
                items = result.items.filter((item) => {
                    const external = item.external_key?.toLowerCase() ?? ''
                    const summary = summarizeReferenceData(item.data).toLowerCase()
                    return item.id.toLowerCase().includes(lowered) || external.includes(lowered) || summary.includes(lowered)
                })
            } else {
                const searchAttribute = pickReferenceDefaultSearchAttribute(searchableFields)
                if (searchAttribute) {
                    const result = await mdmApi.searchEntries(referenceDictionaryId, {
                        filters: [{ attribute: searchAttribute, op: 'contains', value: normalizedQuery }],
                        page: { limit: 40, offset: 0 },
                    })
                    items = result.items
                }
            }
        }

        if (referenceDefaultSearchSeq.value !== seq) {
            return
        }
        referenceDefaultOptions.value = items.map((item) => {
            const externalKey = item.external_key?.trim()
            const summary = summarizeReferenceData(item.data)
            const prefix = externalKey && externalKey !== '' ? externalKey : item.id.slice(0, 8)
            return {
                id: item.id,
                label: summary === '—' ? `${prefix} (${item.id})` : `${prefix} (${item.id}) — ${summary}`,
            }
        })
        for (const option of referenceDefaultOptions.value) {
            referenceDefaultOptionCache.value[option.id] = option
        }
    } catch (err) {
        if (referenceDefaultSearchSeq.value !== seq) {
            return
        }
        referenceDefaultOptions.value = []
        referenceDefaultError.value = formatError(err)
    } finally {
        if (referenceDefaultSearchSeq.value === seq) {
            referenceDefaultLoading.value = false
        }
    }
}

function editorSelectedReferenceDefaultIds(): string[] {
    return splitItems(editor.default_text)
}

function setEditorSelectedReferenceDefaults(ids: string[]): void {
    const normalized = Array.from(new Set(ids.map((item) => item.trim()).filter((item) => item !== '')))
    editor.default_text = normalized.join('\n')
}

function toggleEditorReferenceDefault(optionId: string, checked: boolean): void {
    const current = editorSelectedReferenceDefaultIds()
    if (checked) {
        setEditorSelectedReferenceDefaults([...current, optionId])
        return
    }
    setEditorSelectedReferenceDefaults(current.filter((item) => item !== optionId))
}

function isEditorReferenceDefaultSelected(optionId: string): boolean {
    return editorSelectedReferenceDefaultIds().includes(optionId)
}

function filteredReferenceDefaultOptions(): ReferenceDefaultOption[] {
    const query = referenceDefaultSearch.value.trim().toLowerCase()
    if (query === '') {
        return referenceDefaultOptions.value
    }
    return referenceDefaultOptions.value.filter((option) => option.label.toLowerCase().includes(query))
}

function visibleReferenceDefaultOptions(): ReferenceDefaultOption[] {
    const current = filteredReferenceDefaultOptions()
    const selectedIds = editorSelectedReferenceDefaultIds()
    const selectedOptions = selectedIds.map((id) => {
        return current.find((option) => option.id === id) ?? referenceDefaultOptionCache.value[id] ?? { id, label: id }
    })

    const merged = [...selectedOptions, ...current]
    const deduplicated: ReferenceDefaultOption[] = []
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

function makeNextPosition(): number {
    if (schemaRows.value.length === 0) {
        return 10
    }
    const maxPosition = Math.max(...schemaRows.value.map((row) => row.position))
    return maxPosition + 10
}

function resetEditorFields(): void {
    editor.required = false
    editor.is_unique = false
    editor.is_multivalue = false
    editor.position = 10
    editor.default_text = ''
    editor.default_boolean = ''
    editor.min = ''
    editor.max = ''
    editor.min_length = ''
    editor.max_length = ''
    editor.pattern = ''
    editor.allowed_values_text = ''
    editor.min_date = ''
    editor.max_date = ''
    editor.min_items = ''
    editor.max_items = ''
    referenceDefaultSearch.value = ''
    referenceDefaultOptionCache.value = {}
}

function closeEditor(): void {
    editorOpen.value = false
    editorIssues.value = []
}

function openCreateEditor(): void {
    resetEditorFields()
    const used = new Set(schemaRows.value.map((row) => row.attribute_id))
    const firstAvailable = sortedAttributes.value.find((attribute) => !used.has(attribute.id))
    editor.mode = 'create'
    editor.row_id = ''
    editor.attribute_id = firstAvailable?.id ?? sortedAttributes.value[0]?.id ?? ''
    editor.position = makeNextPosition()
    editorOpen.value = true
}

function toNumberText(value: unknown): string {
    if (typeof value === 'number' && Number.isFinite(value)) {
        return String(value)
    }
    return ''
}

function toStringArrayText(value: unknown): string {
    if (!Array.isArray(value)) {
        return ''
    }
    return value
        .filter((item): item is string => typeof item === 'string')
        .map((item) => item.trim())
        .filter((item) => item.length > 0)
        .join('\n')
}

function openEditEditor(rowId: string): void {
    const row = schemaRows.value.find((item) => item.row_id === rowId)
    if (!row) {
        return
    }

    resetEditorFields()
    editor.mode = 'edit'
    editor.row_id = row.row_id
    editor.attribute_id = row.attribute_id
    editor.required = row.required
    editor.is_unique = row.is_unique
    editor.is_multivalue = row.is_multivalue
    editor.position = row.position

    const validators = asRecord(row.validators)
    editor.min = toNumberText(validators.min)
    editor.max = toNumberText(validators.max)
    editor.min_length = toNumberText(validators.min_length)
    editor.max_length = toNumberText(validators.max_length)
    editor.pattern = typeof validators.pattern === 'string' ? validators.pattern : ''
    editor.min_date = typeof validators.min_date === 'string' ? validators.min_date : ''
    editor.max_date = typeof validators.max_date === 'string' ? validators.max_date : ''
    editor.min_items = toNumberText(validators.min_items)
    editor.max_items = toNumberText(validators.max_items)

    const allowedValues = Array.isArray(validators.allowed_values)
        ? validators.allowed_values
        : Array.isArray(validators.enum)
            ? validators.enum
            : []
    editor.allowed_values_text = toStringArrayText(allowedValues)

    if (row.is_multivalue) {
        if (Array.isArray(row.default_value)) {
            editor.default_text = row.default_value.map((item) => String(item)).join('\n')
        }
    } else if (editorType.value === 'boolean') {
        editor.default_boolean = row.default_value === true ? 'true' : row.default_value === false ? 'false' : ''
    } else if (row.default_value !== undefined) {
        editor.default_text = String(row.default_value)
    }

    editorOpen.value = true
}

function splitItems(value: string): string[] {
    return value
        .split(/\n|,/)
        .map((item) => item.trim())
        .filter((item) => item.length > 0)
}

function parseIntegerField(fieldLabel: string, value: string, issues: string[]): number | undefined {
    const trimmed = value.trim()
    if (trimmed === '') {
        return undefined
    }
    const parsed = Number(trimmed)
    if (!Number.isInteger(parsed)) {
        issues.push(`Поле "${fieldLabel}" должно быть целым числом`)
        return undefined
    }
    return parsed
}

function parseNumberField(fieldLabel: string, value: string, issues: string[]): number | undefined {
    const trimmed = value.trim()
    if (trimmed === '') {
        return undefined
    }
    const parsed = Number(trimmed)
    if (!Number.isFinite(parsed)) {
        issues.push(`Поле "${fieldLabel}" должно быть числом`)
        return undefined
    }
    return parsed
}

function isValidDateYmd(value: string): boolean {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) {
        return false
    }
    return !Number.isNaN(Date.parse(`${value}T00:00:00Z`))
}

function parseSingleValue(raw: string, dataType: Attribute['data_type'], issues: string[]): unknown {
    const trimmed = raw.trim()
    if (trimmed === '') {
        throw new Error('Пустое значение')
    }

    switch (dataType) {
        case 'number': {
            const parsed = Number(trimmed)
            if (!Number.isFinite(parsed)) {
                throw new Error('Ожидалось число')
            }
            return parsed
        }
        case 'boolean': {
            if (trimmed === 'true') return true
            if (trimmed === 'false') return false
            throw new Error('Ожидалось true или false')
        }
        case 'date': {
            if (!isValidDateYmd(trimmed)) {
                throw new Error('Ожидалась дата в формате YYYY-MM-DD')
            }
            return trimmed
        }
        case 'reference': {
            if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(trimmed)) {
                throw new Error('Ожидался UUID ссылки')
            }
            return trimmed
        }
        case 'enum':
        case 'string':
            return trimmed
        default:
            issues.push('Неподдерживаемый тип данных')
            return trimmed
    }
}

function buildValidators(issues: string[]): Record<string, unknown> | undefined {
    const validators: Record<string, unknown> = {}

    if (editorType.value === 'string') {
        const minLength = parseIntegerField('Минимальная длина', editor.min_length, issues)
        const maxLength = parseIntegerField('Максимальная длина', editor.max_length, issues)
        if (minLength !== undefined) validators.min_length = minLength
        if (maxLength !== undefined) validators.max_length = maxLength
        if (editor.pattern.trim() !== '') validators.pattern = editor.pattern.trim()
    }

    if (editorType.value === 'number') {
        const min = parseNumberField('Минимум', editor.min, issues)
        const max = parseNumberField('Максимум', editor.max, issues)
        if (min !== undefined) validators.min = min
        if (max !== undefined) validators.max = max
    }

    if (editorType.value === 'date') {
        if (editor.min_date.trim() !== '') {
            if (!isValidDateYmd(editor.min_date.trim())) {
                issues.push('Поле "Дата с" должно быть в формате YYYY-MM-DD')
            } else {
                validators.min_date = editor.min_date.trim()
            }
        }
        if (editor.max_date.trim() !== '') {
            if (!isValidDateYmd(editor.max_date.trim())) {
                issues.push('Поле "Дата до" должно быть в формате YYYY-MM-DD')
            } else {
                validators.max_date = editor.max_date.trim()
            }
        }
    }

    if (editorType.value === 'enum') {
        const values = splitItems(editor.allowed_values_text)
        if (values.length > 0) {
            validators.allowed_values = values
        }
    }

    if (editor.is_multivalue) {
        const minItems = parseIntegerField('Минимум значений', editor.min_items, issues)
        const maxItems = parseIntegerField('Максимум значений', editor.max_items, issues)
        if (minItems !== undefined) validators.min_items = minItems
        if (maxItems !== undefined) validators.max_items = maxItems
    }

    if (Object.keys(validators).length === 0) {
        return undefined
    }
    return validators
}

function buildDefaultValue(issues: string[]): unknown | undefined {
    if (editor.is_multivalue) {
        const items = splitItems(editor.default_text)
        if (items.length === 0) {
            return undefined
        }
        const result: unknown[] = []
        for (const item of items) {
            try {
                result.push(parseSingleValue(item, editorType.value, issues))
            } catch (err) {
                issues.push(`Значение по умолчанию: ${formatError(err)}`)
            }
        }
        return result
    }

    if (editorType.value === 'boolean') {
        if (editor.default_boolean === '') {
            return undefined
        }
        return editor.default_boolean === 'true'
    }

    if (editor.default_text.trim() === '') {
        return undefined
    }

    try {
        return parseSingleValue(editor.default_text.trim(), editorType.value, issues)
    } catch (err) {
        issues.push(`Значение по умолчанию: ${formatError(err)}`)
        return undefined
    }
}

function applyEditor(): void {
    editorIssues.value = []
    const issues: string[] = []

    if (!editor.attribute_id) {
        issues.push('Выберите атрибут')
    }
    if (!Number.isInteger(editor.position) || editor.position < 0) {
        issues.push('Позиция должна быть целым числом >= 0')
    }

    const duplicate = schemaRows.value.some(
        (row) => row.attribute_id === editor.attribute_id && row.row_id !== editor.row_id,
    )
    if (duplicate) {
        issues.push('Этот атрибут уже добавлен в схему')
    }

    const validators = buildValidators(issues)
    const defaultValue = buildDefaultValue(issues)

    if (issues.length > 0) {
        editorIssues.value = issues
        return
    }

    const nextRow: SchemaDraftRow = {
        row_id: editor.mode === 'create' ? crypto.randomUUID() : editor.row_id,
        attribute_id: editor.attribute_id,
        required: editor.required,
        is_unique: editor.is_unique,
        is_multivalue: editor.is_multivalue,
        position: editor.position,
        default_value: defaultValue,
        validators,
    }

    if (editor.mode === 'create') {
        schemaRows.value.push(nextRow)
    } else {
        const index = schemaRows.value.findIndex((row) => row.row_id === editor.row_id)
        if (index >= 0) {
            schemaRows.value[index] = nextRow
        }
    }

    closeEditor()
    message.value = 'Изменения подготовлены. Нажмите "Сохранить схему".'
}

function removeSchemaRow(rowId: string): void {
    if (!canWrite.value || busy.value) {
        return
    }
    if (!window.confirm('Удалить атрибут из схемы?')) {
        return
    }
    schemaRows.value = schemaRows.value.filter((row) => row.row_id !== rowId)
    message.value = 'Изменения подготовлены. Нажмите "Сохранить схему".'
}

function validatorsSummary(row: SchemaDraftRow): string {
    const validators = asRecord(row.validators)
    const parts: string[] = []

    if (validators.min !== undefined) parts.push(`min=${validators.min}`)
    if (validators.max !== undefined) parts.push(`max=${validators.max}`)
    if (validators.min_length !== undefined) parts.push(`min_length=${validators.min_length}`)
    if (validators.max_length !== undefined) parts.push(`max_length=${validators.max_length}`)
    if (validators.pattern !== undefined) parts.push('pattern')
    if (validators.min_date !== undefined) parts.push(`min_date=${validators.min_date}`)
    if (validators.max_date !== undefined) parts.push(`max_date=${validators.max_date}`)
    if (validators.min_items !== undefined) parts.push(`min_items=${validators.min_items}`)
    if (validators.max_items !== undefined) parts.push(`max_items=${validators.max_items}`)
    if (Array.isArray(validators.allowed_values)) parts.push(`allowed_values=${validators.allowed_values.length}`)
    if (Array.isArray(validators.enum)) parts.push(`enum=${validators.enum.length}`)

    return parts.length > 0 ? parts.join(', ') : '—'
}

function defaultValuePreview(value: unknown): string {
    if (value === undefined) {
        return '—'
    }
    if (Array.isArray(value)) {
        return value.length === 0 ? '[]' : value.join(', ')
    }
    if (typeof value === 'boolean') {
        return value ? 'true' : 'false'
    }
    return String(value)
}

function buildSchemaPayload(): { attributes: SchemaAttribute[]; issues: string[] } {
    const issues: string[] = []
    const seen = new Set<string>()
    const payload: SchemaAttribute[] = []

    for (const row of schemaRows.value) {
        if (!row.attribute_id) {
            issues.push('Есть строка схемы без атрибута')
            continue
        }
        if (seen.has(row.attribute_id)) {
            issues.push('В схеме есть дублирующиеся атрибуты')
            continue
        }
        seen.add(row.attribute_id)

        if (!Number.isInteger(row.position) || row.position < 0) {
            issues.push('Позиция у каждого атрибута должна быть целым числом >= 0')
            continue
        }

        payload.push({
            attribute_id: row.attribute_id,
            required: row.required,
            is_unique: row.is_unique,
            is_multivalue: row.is_multivalue,
            position: row.position,
            default_value: row.default_value,
            validators: row.validators,
        })
    }

    payload.sort((a, b) => a.position - b.position)
    return { attributes: payload, issues }
}

async function loadWorkspace(): Promise<void> {
    if (!dictionaryId.value) {
        error.value = 'dictionary_id не задан'
        return
    }

    loading.value = true
    clearFeedback()

    try {
        const [dictionaryResult, attributesResult, schemaResult] = await Promise.all([
            mdmApi.getDictionary(dictionaryId.value),
            mdmApi.listAttributes(500, 0),
            mdmApi.getDictionarySchema(dictionaryId.value),
        ])

        dictionary.value = dictionaryResult
        attributes.value = attributesResult.items
        currentSchema.value = schemaResult.attributes

        syncDetailsForm()
        syncSchemaRows()
    } catch (err) {
        error.value = formatError(err)
    } finally {
        loading.value = false
    }
}

async function saveDictionaryDetails(): Promise<void> {
    if (!canWrite.value || !dictionary.value) {
        return
    }

    busy.value = true
    clearFeedback()
    try {
        dictionary.value = await mdmApi.updateDictionary(dictionary.value.id, {
            name: detailsForm.name.trim() || undefined,
            description: detailsForm.description.trim() || undefined,
        })
        syncDetailsForm()
        message.value = 'Карточка справочника обновлена'
    } catch (err) {
        error.value = formatError(err)
    } finally {
        busy.value = false
    }
}

async function saveSchema(): Promise<void> {
    if (!canWrite.value) {
        return
    }

    busy.value = true
    clearFeedback()
    try {
        const payload = buildSchemaPayload()
        if (payload.issues.length > 0) {
            error.value = payload.issues.join('\n')
            return
        }

        const result = await mdmApi.putDictionarySchema(dictionaryId.value, payload.attributes)
        currentSchema.value = result.attributes
        syncSchemaRows()
        message.value = 'Схема справочника сохранена'
    } catch (err) {
        error.value = formatError(err)
    } finally {
        busy.value = false
    }
}

onMounted(loadWorkspace)
</script>

<template>
    <section>
        <div class="section-head">
            <div>
                <p class="muted">
                    <RouterLink class="link" to="/dictionaries">← К списку справочников</RouterLink>
                </p>
                <h1>{{ dictionary?.name || 'Справочник' }}</h1>
                <p class="muted">Настройка карточки и схемы атрибутов без JSON-редактирования.</p>
            </div>
            <div class="inline-actions">
                <RouterLink v-if="dictionary" class="btn primary"
                    :to="{ path: '/objects', query: { dictionaryId: dictionary.id } }">
                    <Settings2 class="btn-icon" :size="16" aria-hidden="true" />
                    Перейти к объектам
                </RouterLink>
                <button class="btn" :disabled="loading || busy" @click="loadWorkspace">
                    <RefreshCw class="btn-icon" :size="16" aria-hidden="true" />
                    Обновить
                </button>
            </div>
        </div>

        <p v-if="message" class="alert success">{{ message }}</p>
        <p v-if="error" class="alert error">{{ error }}</p>

        <article v-if="dictionary" class="card" :class="{ 'is-disabled': !canWrite }">
            <div class="card-title-line">
                <h3>Карточка справочника</h3>
                <span class="pill">schema_version={{ dictionary.schema_version }}</span>
            </div>

            <div class="detail-grid">
                <div class="detail-item">
                    <p class="muted">ID</p>
                    <p><code>{{ dictionary.id }}</code></p>
                </div>
                <div class="detail-item">
                    <p class="muted">Code</p>
                    <p><code>{{ dictionary.code }}</code></p>
                </div>
            </div>

            <form class="form-grid" @submit.prevent="saveDictionaryDetails">
                <label>
                    Name
                    <input v-model="detailsForm.name" :disabled="!canWrite || busy" />
                </label>
                <label>
                    Description
                    <input v-model="detailsForm.description" :disabled="!canWrite || busy" />
                </label>
                <div class="form-actions">
                    <button class="btn primary" :disabled="!canWrite || busy">
                        <Save class="btn-icon" :size="16" aria-hidden="true" />
                        Сохранить карточку
                    </button>
                </div>
            </form>

            <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
        </article>

        <article class="card" :class="{ 'is-disabled': !canWrite }">
            <div class="card-title-line">
                <h3>Атрибуты справочника (схема)</h3>
                <div class="inline-actions">
                    <button class="btn" :disabled="!canWrite || busy" @click="openCreateEditor">
                        <Plus class="btn-icon" :size="16" aria-hidden="true" />
                        Добавить атрибут
                    </button>
                    <button class="btn primary" :disabled="!canWrite || busy" @click="saveSchema">
                        <Save class="btn-icon" :size="16" aria-hidden="true" />
                        Сохранить схему
                    </button>
                </div>
            </div>


            <div class="table-wrap">
                <table class="table">
                    <thead>
                        <tr>
                            <th>Code</th>
                            <th>Name</th>
                            <th>Type</th>
                            <th>Required</th>
                            <th>Unique</th>
                            <th>Multivalue</th>
                            <th>Position</th>
                            <th>Default</th>
                            <th>Правила</th>
                            <th class="actions">Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="schemaTableRows.length === 0">
                            <td colspan="10" class="muted">Схема пустая. Добавьте первый атрибут.</td>
                        </tr>
                        <tr v-for="row in schemaTableRows" :key="row.row_id">
                            <td><code>{{ row.attribute?.code ?? row.attribute_id }}</code></td>
                            <td>{{ row.attribute?.name ?? 'Неизвестный атрибут' }}</td>
                            <td><span class="pill">{{ row.attribute?.data_type ?? 'n/a' }}</span></td>
                            <td>{{ row.required ? 'Да' : 'Нет' }}</td>
                            <td>{{ row.is_unique ? 'Да' : 'Нет' }}</td>
                            <td>{{ row.is_multivalue ? 'Да' : 'Нет' }}</td>
                            <td>{{ row.position }}</td>
                            <td>{{ defaultValuePreview(row.default_value) }}</td>
                            <td>{{ validatorsSummary(row) }}</td>
                            <td class="actions-row">
                                <button class="btn btn-icon-only" title="Изменить атрибут схемы"
                                    :disabled="!canWrite || busy" @click="openEditEditor(row.row_id)">
                                    <Pencil :size="16" aria-hidden="true" />
                                    <span class="sr-only">Изменить атрибут схемы</span>
                                </button>
                                <button class="btn danger btn-icon-only" title="Удалить атрибут схемы"
                                    :disabled="!canWrite || busy" @click="removeSchemaRow(row.row_id)">
                                    <Trash2 :size="16" aria-hidden="true" />
                                    <span class="sr-only">Удалить атрибут схемы</span>
                                </button>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>
            <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
        </article>

        <div v-if="editorOpen" class="modal-backdrop">
            <article class="modal-card">
                <div class="card-title-line">
                    <h3>{{ editor.mode === 'create' ? 'Добавить атрибут в схему' : 'Редактирование атрибута схемы' }}
                    </h3>
                    <button class="btn btn-icon-only" type="button" title="Закрыть" @click="closeEditor">
                        <X :size="16" aria-hidden="true" />
                        <span class="sr-only">Закрыть</span>
                    </button>
                </div>

                <ul v-if="editorIssues.length > 0" class="issue-list">
                    <li v-for="issue in editorIssues" :key="issue">{{ issue }}</li>
                </ul>

                <form class="form-grid" @submit.prevent="applyEditor">
                    <label class="full">
                        Атрибут
                        <select v-model="editor.attribute_id">
                            <option value="">Выберите атрибут</option>
                            <option v-for="attribute in sortedAttributes" :key="attribute.id" :value="attribute.id">
                                {{ attribute.code }} ({{ attribute.name }})
                            </option>
                        </select>
                    </label>

                    <label>
                        Позиция в форме
                        <input v-model.number="editor.position" type="number" min="0" />
                    </label>

                    <div class="full check-stack">
                        <label class="check">
                            <input v-model="editor.required" type="checkbox" />
                            Обязательное поле
                        </label>

                        <label class="check">
                            <input v-model="editor.is_unique" type="checkbox" :disabled="editor.is_multivalue" />
                            Уникальное значение
                        </label>
                        <p v-if="editor.is_multivalue" class="validator-hints">
                            Для множественного значения уникальность применяется по каждому элементу массива.
                            Флаг отключен, чтобы избежать конфликтной семантики.
                        </p>

                        <label class="check">
                            <input v-model="editor.is_multivalue" type="checkbox" />
                            Можно несколько значений
                        </label>
                    </div>

                    <label v-if="editorType === 'boolean' && !editor.is_multivalue" class="full">
                        Значение по умолчанию
                        <select v-model="editor.default_boolean">
                            <option value="">Не задано</option>
                            <option value="true">true</option>
                            <option value="false">false</option>
                        </select>
                    </label>
                    <label v-else-if="editorType === 'reference' && !editor.is_multivalue" class="full">
                        Значение по умолчанию
                        <input v-model="referenceDefaultSearch" placeholder="Поиск значения справочника" :disabled="referenceDefaultLoading" />
                        <select v-model="editor.default_text" :disabled="referenceDefaultLoading">
                            <option value="">Не задано</option>
                            <option v-for="option in visibleReferenceDefaultOptions()" :key="option.id" :value="option.id">
                                {{ option.label }}
                            </option>
                        </select>
                        <span v-if="!referenceDefaultLoading && visibleReferenceDefaultOptions().length === 0" class="muted">
                            Нет значений по фильтру
                        </span>
                        <span v-if="referenceDefaultLoading" class="muted">Загрузка значений связанного справочника…</span>
                        <span v-else-if="referenceDefaultError" class="muted">{{ referenceDefaultError }}</span>
                    </label>
                    <label v-else-if="editorType === 'reference' && editor.is_multivalue" class="full">
                        Значение по умолчанию
                        <input v-model="referenceDefaultSearch" placeholder="Поиск значения справочника" :disabled="referenceDefaultLoading" />
                        <div class="reference-options-list">
                            <label v-for="option in visibleReferenceDefaultOptions()" :key="option.id" class="check reference-option">
                                <input
                                    type="checkbox"
                                    :checked="isEditorReferenceDefaultSelected(option.id)"
                                    :disabled="referenceDefaultLoading"
                                    @change="toggleEditorReferenceDefault(option.id, ($event.target as HTMLInputElement).checked)"
                                />
                                {{ option.label }}
                            </label>
                            <span v-if="visibleReferenceDefaultOptions().length === 0" class="muted">
                                Нет значений по фильтру
                            </span>
                        </div>
                        <span class="muted">Выбрано: {{ editorSelectedReferenceDefaultIds().length }}</span>
                        <span v-if="referenceDefaultLoading" class="muted">Загрузка значений связанного справочника…</span>
                        <span v-else-if="referenceDefaultError" class="muted">{{ referenceDefaultError }}</span>
                    </label>

                    <label v-else class="full">
                        Значение по умолчанию {{ editor.is_multivalue ? '(каждое с новой строки)' : '' }}
                        <textarea v-if="editor.is_multivalue" v-model="editor.default_text"
                            class="code-area code-area-compact" placeholder="значение 1&#10;значение 2"></textarea>
                        <input v-else v-model="editor.default_text" :placeholder="editorType" />
                    </label>

                    <label v-if="editorType === 'string'">
                        Минимальная длина
                        <input v-model="editor.min_length" placeholder="например: 2" />
                    </label>
                    <label v-if="editorType === 'string'">
                        Максимальная длина
                        <input v-model="editor.max_length" placeholder="например: 120" />
                    </label>
                    <label v-if="editorType === 'string'" class="full">
                        RegExp pattern
                        <input v-model="editor.pattern" placeholder="например: ^[A-Z0-9_-]+$" />
                    </label>

                    <label v-if="editorType === 'number'">
                        Минимум
                        <input v-model="editor.min" placeholder="например: 0" />
                    </label>
                    <label v-if="editorType === 'number'">
                        Максимум
                        <input v-model="editor.max" placeholder="например: 100000" />
                    </label>

                    <label v-if="editorType === 'date'">
                        Дата с
                        <input v-model="editor.min_date" type="date" />
                    </label>
                    <label v-if="editorType === 'date'">
                        Дата до
                        <input v-model="editor.max_date" type="date" />
                    </label>

                    <label v-if="editorType === 'enum'" class="full">
                        Допустимые значения enum (каждое с новой строки)
                        <textarea v-model="editor.allowed_values_text" class="code-area code-area-compact"
                            placeholder="new&#10;used&#10;refurbished"></textarea>
                    </label>

                    <label v-if="editor.is_multivalue">
                        Минимум значений
                        <input v-model="editor.min_items" placeholder="например: 1" />
                    </label>
                    <label v-if="editor.is_multivalue">
                        Максимум значений
                        <input v-model="editor.max_items" placeholder="например: 10" />
                    </label>

                    <div class="form-actions">
                        <button class="btn primary" type="submit">
                            <Save class="btn-icon" :size="16" aria-hidden="true" />
                            Применить
                        </button>
                        <button class="btn" type="button" @click="closeEditor">
                            <X class="btn-icon" :size="16" aria-hidden="true" />
                            Отмена
                        </button>
                    </div>
                </form>
            </article>
        </div>
    </section>
</template>
