<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { type Attribute, type Dictionary, type SchemaAttribute, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

interface SchemaRowDraft {
    row_id: string
    attribute_id: string
    required: boolean
    is_unique: boolean
    is_multivalue: boolean
    position: number
    default_value_text: string
    validators_text: string
}

interface SchemaViewRow {
    attribute_id: string
    code: string
    name: string
    data_type: Attribute['data_type']
    required: boolean
    is_unique: boolean
    is_multivalue: boolean
    position: number
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
const schemaRows = ref<SchemaRowDraft[]>([])

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

const schemaView = computed<SchemaViewRow[]>(() => {
    const result: SchemaViewRow[] = []
    const sortedSchema = [...currentSchema.value].sort((a, b) => a.position - b.position)

    for (const item of sortedSchema) {
        const attr = attributesById.value.get(item.attribute_id)
        if (!attr) {
            continue
        }
        result.push({
            attribute_id: item.attribute_id,
            code: attr.code,
            name: attr.name,
            data_type: attr.data_type,
            required: item.required,
            is_unique: item.is_unique,
            is_multivalue: item.is_multivalue,
            position: item.position,
        })
    }

    return result
})

watch(dictionaryId, (next, prev) => {
    if (!next || next === prev) {
        return
    }
    void loadWorkspace()
})

function clearFeedback(): void {
    error.value = ''
    message.value = ''
}

function syncDetailsForm(): void {
    detailsForm.name = dictionary.value?.name ?? ''
    detailsForm.description = dictionary.value?.description ?? ''
}

function makeRow(partial?: Partial<SchemaRowDraft>): SchemaRowDraft {
    return {
        row_id: crypto.randomUUID(),
        attribute_id: '',
        required: false,
        is_unique: false,
        is_multivalue: false,
        position: 10,
        default_value_text: '',
        validators_text: '',
        ...partial,
    }
}

function syncSchemaRows(): void {
    schemaRows.value = currentSchema.value.map((item) =>
        makeRow({
            attribute_id: item.attribute_id,
            required: item.required,
            is_unique: item.is_unique,
            is_multivalue: item.is_multivalue,
            position: item.position,
            default_value_text: item.default_value === undefined ? '' : JSON.stringify(item.default_value, null, 2),
            validators_text:
                item.validators === undefined || item.validators === null ? '' : JSON.stringify(item.validators, null, 2),
        }),
    )
}

function parseJSONField(value: string, rowIndex: number, fieldLabel: string): unknown | undefined {
    const trimmed = value.trim()
    if (trimmed === '') {
        return undefined
    }

    try {
        return JSON.parse(trimmed) as unknown
    } catch {
        throw new Error(`Строка ${rowIndex}: ${fieldLabel} содержит невалидный JSON`)
    }
}

function parseValidators(value: string, rowIndex: number): Record<string, unknown> | undefined {
    const parsed = parseJSONField(value, rowIndex, 'validators')
    if (parsed === undefined) {
        return undefined
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(`Строка ${rowIndex}: validators должен быть JSON-объектом`)
    }
    return parsed as Record<string, unknown>
}

function buildSchemaPayload(): { attributes: SchemaAttribute[]; issues: string[] } {
    const issues: string[] = []
    const uniqueAttributeIDs = new Set<string>()
    const payload: SchemaAttribute[] = []

    for (let index = 0; index < schemaRows.value.length; index += 1) {
        const row = schemaRows.value[index]
        const rowNumber = index + 1

        if (!row.attribute_id) {
            issues.push(`Строка ${rowNumber}: выберите атрибут`)
            continue
        }
        if (uniqueAttributeIDs.has(row.attribute_id)) {
            issues.push(`Строка ${rowNumber}: атрибут не должен повторяться`)
            continue
        }
        uniqueAttributeIDs.add(row.attribute_id)

        if (!Number.isInteger(row.position) || row.position < 0) {
            issues.push(`Строка ${rowNumber}: position должен быть целым числом >= 0`)
            continue
        }

        try {
            const defaultValue = parseJSONField(row.default_value_text, rowNumber, 'default_value')
            const validators = parseValidators(row.validators_text, rowNumber)

            payload.push({
                attribute_id: row.attribute_id,
                required: row.required,
                is_unique: row.is_unique,
                is_multivalue: row.is_multivalue,
                position: row.position,
                default_value: defaultValue,
                validators,
            })
        } catch (err) {
            issues.push(formatError(err))
        }
    }

    payload.sort((a, b) => a.position - b.position)
    return { attributes: payload, issues }
}

function addSchemaRow(): void {
    schemaRows.value.push(
        makeRow({
            attribute_id: sortedAttributes.value[0]?.id ?? '',
            position: schemaRows.value.length === 0 ? 10 : schemaRows.value.length * 10,
        }),
    )
}

function removeSchemaRow(rowId: string): void {
    schemaRows.value = schemaRows.value.filter((row) => row.row_id !== rowId)
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
                <p class="muted">Настройка карточки справочника и его атрибутов.</p>
            </div>
            <div class="inline-actions">
                <RouterLink v-if="dictionary" class="btn"
                    :to="{ path: '/objects', query: { dictionaryId: dictionary.id } }">
                    Перейти к объектам
                </RouterLink>
                <button class="btn" :disabled="loading || busy" @click="loadWorkspace">Обновить</button>
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
                    <button class="btn primary" :disabled="!canWrite || busy">Сохранить карточку</button>
                </div>
            </form>

            <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
        </article>

        <article class="card" :class="{ 'is-disabled': !canWrite }">
            <div class="card-title-line">
                <h3>Атрибуты справочника (схема)</h3>
                <div class="inline-actions">
                    <button class="btn" :disabled="!canWrite || busy" @click="addSchemaRow">Добавить атрибут</button>
                    <button class="btn primary" :disabled="!canWrite || busy" @click="saveSchema">Сохранить
                        схему</button>
                </div>
            </div>
            <p class="muted">
                Здесь вы связываете справочник с атрибутами и задаете правила полей (`required`, `unique`,
                `multivalue`).
            </p>

            <div class="schema-rows">
                <div v-if="schemaRows.length === 0" class="schema-row muted">Схема пока пустая</div>

                <div v-for="(row, index) in schemaRows" :key="row.row_id" class="schema-row">
                    <p class="row-title">Правило {{ index + 1 }}</p>
                    <div class="schema-grid">
                        <label>
                            Attribute
                            <select v-model="row.attribute_id" :disabled="!canWrite || busy">
                                <option value="">Выберите атрибут</option>
                                <option v-for="attribute in sortedAttributes" :key="attribute.id" :value="attribute.id">
                                    {{ attribute.code }} ({{ attribute.name }})
                                </option>
                            </select>
                        </label>

                        <label>
                            Position
                            <input v-model.number="row.position" type="number" min="0" :disabled="!canWrite || busy" />
                        </label>

                        <label class="check">
                            <input v-model="row.required" type="checkbox" :disabled="!canWrite || busy" />
                            Required
                        </label>

                        <label class="check">
                            <input v-model="row.is_unique" type="checkbox" :disabled="!canWrite || busy" />
                            Unique
                        </label>

                        <label class="check">
                            <input v-model="row.is_multivalue" type="checkbox" :disabled="!canWrite || busy" />
                            Multivalue
                        </label>

                        <label class="full">
                            default_value (JSON)
                            <textarea v-model="row.default_value_text" class="code-area code-area-compact"
                                placeholder='например: "Неизвестно" или 0' :disabled="!canWrite || busy"></textarea>
                        </label>

                        <label class="full">
                            validators (JSON object)
                            <textarea v-model="row.validators_text" class="code-area code-area-compact"
                                placeholder='например: {"min": 0, "max": 1000}'
                                :disabled="!canWrite || busy"></textarea>
                        </label>
                    </div>

                    <div class="inline-actions">
                        <button class="btn danger" :disabled="!canWrite || busy"
                            @click="removeSchemaRow(row.row_id)">Удалить</button>
                    </div>
                </div>
            </div>
            <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
        </article>

        <article class="card">
            <h3>Текущие атрибуты в схеме ({{ schemaView.length }})</h3>
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
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="schemaView.length === 0">
                            <td colspan="7" class="muted">Схема пустая</td>
                        </tr>
                        <tr v-for="item in schemaView" :key="item.attribute_id">
                            <td><code>{{ item.code }}</code></td>
                            <td>{{ item.name }}</td>
                            <td><span class="pill">{{ item.data_type }}</span></td>
                            <td>{{ item.required }}</td>
                            <td>{{ item.is_unique }}</td>
                            <td>{{ item.is_multivalue }}</td>
                            <td>{{ item.position }}</td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </article>
    </section>
</template>
