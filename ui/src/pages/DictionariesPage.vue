<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'

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

const createForm = reactive({
    code: '',
    name: '',
    description: '',
})

const editForm = reactive({
    id: '',
    name: '',
    description: '',
})

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))

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

function beginEdit(item: Dictionary): void {
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
    error.value = ''
    message.value = ''

    try {
        await mdmApi.updateDictionary(editForm.id, {
            name: editForm.name || undefined,
            description: editForm.description || undefined,
        })

        message.value = 'Справочник обновлен'
        cancelEdit()
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

onMounted(load)
</script>

<template>
    <section>
        <div class="section-head">
            <div>
                <h1>Справочники</h1>
                <p class="muted">Создание справочников и переход к настройке их атрибутов.</p>
            </div>
            <button class="btn" :disabled="loading" @click="load">Обновить</button>
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
                    <input v-model="createForm.description" placeholder="Описание"
                        :disabled="!canWrite || submitting" />
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
                            <th>Description</th>
                            <th>Schema ver.</th>
                            <th>Атрибутов в схеме</th>
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
                            <td>{{ item.description || '—' }}</td>
                            <td>{{ item.schema_version }}</td>
                            <td>{{ schemaSizeByDictionary[item.id] ?? '—' }}</td>
                            <td class="actions-row">
                                <RouterLink class="btn" :to="`/dictionaries/${item.id}`">Open</RouterLink>
                                <button class="btn" :disabled="!canWrite" @click="beginEdit(item)">Edit</button>
                                <button class="btn danger" :disabled="!canWrite"
                                    @click="removeDictionary(item.id)">Delete</button>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </article>

        <article v-if="editForm.id" class="card">
            <h3>Редактирование</h3>
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
