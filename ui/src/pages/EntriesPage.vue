<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import JsonBox from '../components/JsonBox.vue'
import { type Dictionary, type Entry, type SearchRequest, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

const identity = useDevIdentityStore()

const dictionaries = ref<Dictionary[]>([])
const selectedDictionaryId = ref('')

const entries = ref<Entry[]>([])
const total = ref(0)
const limit = ref(20)
const offset = ref(0)

const loading = ref(false)
const submitting = ref(false)
const message = ref('')
const error = ref('')

const createForm = reactive({
    externalKey: '',
    dataJson: '{\n  \n}',
})

const editForm = reactive({
    entryId: '',
    dataJson: '{\n  \n}',
})

const searchJson = ref('{\n  "filters": [],\n  "sort": [],\n  "page": { "limit": 20, "offset": 0 }\n}')
const searchResult = ref<Entry[] | null>(null)
const searchTotal = ref(0)

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))

async function loadDictionaries(): Promise<void> {
    const result = await mdmApi.listDictionaries(500, 0)
    dictionaries.value = result.items

    if (!selectedDictionaryId.value && dictionaries.value.length > 0) {
        selectedDictionaryId.value = dictionaries.value[0].id
    }
}

async function loadEntries(): Promise<void> {
    if (!selectedDictionaryId.value) {
        return
    }

    loading.value = true
    error.value = ''

    try {
        const result = await mdmApi.listEntries(selectedDictionaryId.value, limit.value, offset.value)
        entries.value = result.items
        total.value = result.total
        searchResult.value = null
        searchTotal.value = 0
    } catch (err) {
        error.value = formatError(err)
    } finally {
        loading.value = false
    }
}

function parseDataJson(value: string): Record<string, unknown> {
    const parsed = JSON.parse(value) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error('data must be JSON object')
    }
    return parsed as Record<string, unknown>
}

async function createEntry(): Promise<void> {
    if (!selectedDictionaryId.value || !canWrite.value) {
        return
    }

    submitting.value = true
    error.value = ''
    message.value = ''

    try {
        const data = parseDataJson(createForm.dataJson)
        await mdmApi.createEntry(selectedDictionaryId.value, {
            external_key: createForm.externalKey.trim() || undefined,
            data,
        })
        message.value = 'Объект создан'
        createForm.externalKey = ''
        createForm.dataJson = '{\n  \n}'
        await loadEntries()
    } catch (err) {
        error.value = formatError(err)
    } finally {
        submitting.value = false
    }
}

function beginEdit(item: Entry): void {
    editForm.entryId = item.id
    editForm.dataJson = JSON.stringify(item.data, null, 2)
}

function cancelEdit(): void {
    editForm.entryId = ''
    editForm.dataJson = '{\n  \n}'
}

async function refreshEntry(entryId: string): Promise<void> {
    if (!selectedDictionaryId.value) {
        return
    }

    try {
        const fresh = await mdmApi.getEntry(selectedDictionaryId.value, entryId)
        const index = entries.value.findIndex((item) => item.id === entryId)
        if (index >= 0) {
            entries.value[index] = fresh
        }
        if (editForm.entryId === entryId) {
            editForm.dataJson = JSON.stringify(fresh.data, null, 2)
        }
    } catch (err) {
        error.value = formatError(err)
    }
}

async function saveEdit(): Promise<void> {
    if (!selectedDictionaryId.value || !editForm.entryId || !canWrite.value) {
        return
    }

    submitting.value = true
    error.value = ''
    message.value = ''

    try {
        const data = parseDataJson(editForm.dataJson)
        await mdmApi.updateEntry(selectedDictionaryId.value, editForm.entryId, data)
        message.value = 'Объект обновлен'
        cancelEdit()
        await loadEntries()
    } catch (err) {
        error.value = formatError(err)
    } finally {
        submitting.value = false
    }
}

async function removeEntry(entryId: string): Promise<void> {
    if (!selectedDictionaryId.value || !canWrite.value) {
        return
    }
    if (!window.confirm('Удалить объект?')) {
        return
    }

    error.value = ''
    message.value = ''

    try {
        await mdmApi.deleteEntry(selectedDictionaryId.value, entryId)
        message.value = 'Объект удален'
        await loadEntries()
    } catch (err) {
        error.value = formatError(err)
    }
}

async function runSearch(): Promise<void> {
    if (!selectedDictionaryId.value) {
        return
    }

    loading.value = true
    error.value = ''

    try {
        const body = JSON.parse(searchJson.value) as SearchRequest
        const result = await mdmApi.searchEntries(selectedDictionaryId.value, body)
        searchResult.value = result.items
        searchTotal.value = result.total
    } catch (err) {
        error.value = formatError(err)
    } finally {
        loading.value = false
    }
}

function nextPage(): void {
    if (offset.value + limit.value >= total.value) {
        return
    }
    offset.value += limit.value
    void loadEntries()
}

function prevPage(): void {
    if (offset.value === 0) {
        return
    }
    offset.value = Math.max(0, offset.value - limit.value)
    void loadEntries()
}

onMounted(async () => {
    loading.value = true
    error.value = ''
    try {
        await loadDictionaries()
        await loadEntries()
    } catch (err) {
        error.value = formatError(err)
    } finally {
        loading.value = false
    }
})
</script>

<template>
    <section>
        <div class="section-head">
            <div>
                <h1>Объекты справочника</h1>
                <p class="muted">CRUD и поиск по entries, включая динамические фильтры.</p>
            </div>
            <button class="btn" :disabled="loading || !selectedDictionaryId" @click="loadEntries">Обновить</button>
        </div>

        <p v-if="message" class="alert success">{{ message }}</p>
        <p v-if="error" class="alert error">{{ error }}</p>

        <article class="card">
            <div class="form-inline">
                <label>
                    Справочник
                    <select v-model="selectedDictionaryId" @change="loadEntries">
                        <option value="">Выберите справочник</option>
                        <option v-for="dictionary in dictionaries" :key="dictionary.id" :value="dictionary.id">
                            {{ dictionary.code }}
                        </option>
                    </select>
                </label>
            </div>
        </article>

        <article class="card" :class="{ 'is-disabled': !canWrite }">
            <h3>Создать объект</h3>
            <form class="form-grid" @submit.prevent="createEntry">
                <label>
                    external_key
                    <input v-model="createForm.externalKey" placeholder="SKU-001" :disabled="!canWrite || submitting" />
                </label>
                <label class="full">
                    data (JSON object)
                    <textarea v-model="createForm.dataJson" class="code-area"
                        :disabled="!canWrite || submitting"></textarea>
                </label>
                <div class="form-actions">
                    <button class="btn primary"
                        :disabled="!canWrite || submitting || !selectedDictionaryId">Создать</button>
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
                        <tr v-for="item in entries" :key="item.id">
                            <td><code>{{ item.id }}</code></td>
                            <td>{{ item.external_key || '—' }}</td>
                            <td>{{ item.version }}</td>
                            <td class="actions-row">
                                <button class="btn" @click="refreshEntry(item.id)">Get</button>
                                <button class="btn" :disabled="!canWrite" @click="beginEdit(item)">Patch</button>
                                <button class="btn danger" :disabled="!canWrite"
                                    @click="removeEntry(item.id)">Delete</button>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </article>

        <article v-if="editForm.entryId" class="card">
            <h3>Редактирование объекта</h3>
            <form class="form-grid" @submit.prevent="saveEdit">
                <label class="full">
                    data (PATCH JSON object)
                    <textarea v-model="editForm.dataJson" class="code-area" :disabled="submitting"></textarea>
                </label>
                <div class="form-actions">
                    <button class="btn primary" :disabled="submitting">Сохранить</button>
                    <button type="button" class="btn" :disabled="submitting" @click="cancelEdit">Отмена</button>
                </div>
            </form>
        </article>

        <article class="card">
            <h3>Поиск (`POST /entries/search`)</h3>
            <textarea v-model="searchJson" class="code-area"></textarea>
            <div class="form-actions">
                <button class="btn primary" :disabled="!selectedDictionaryId" @click="runSearch">Выполнить
                    поиск</button>
            </div>

            <div v-if="searchResult" class="search-result">
                <p class="muted">Найдено: {{ searchTotal }}</p>
                <div v-for="item in searchResult" :key="item.id" class="result-item">
                    <p><strong>{{ item.external_key || item.id }}</strong> <span class="muted">(v{{ item.version
                            }})</span></p>
                    <JsonBox :value="item.data" label="data" />
                </div>
            </div>
        </article>
    </section>
</template>
