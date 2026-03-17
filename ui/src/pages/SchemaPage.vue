<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { type Dictionary, type SchemaAttribute, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

const identity = useDevIdentityStore()

const dictionaries = ref<Dictionary[]>([])
const selectedDictionaryId = ref('')

const currentSchema = ref<SchemaAttribute[]>([])
const schemaJson = ref('[]')

const loading = ref(false)
const submitting = ref(false)
const message = ref('')
const error = ref('')

const canWrite = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))

async function loadDictionaries(): Promise<void> {
    const result = await mdmApi.listDictionaries(500, 0)
    dictionaries.value = result.items

    if (!selectedDictionaryId.value && dictionaries.value.length > 0) {
        selectedDictionaryId.value = dictionaries.value[0].id
    }
}

async function loadSchema(): Promise<void> {
    if (!selectedDictionaryId.value) {
        return
    }

    loading.value = true
    error.value = ''
    message.value = ''

    try {
        const result = await mdmApi.getDictionarySchema(selectedDictionaryId.value)
        currentSchema.value = result.attributes
        schemaJson.value = JSON.stringify(result.attributes, null, 2)
    } catch (err) {
        error.value = formatError(err)
    } finally {
        loading.value = false
    }
}

async function applySchema(): Promise<void> {
    if (!selectedDictionaryId.value || !canWrite.value) {
        return
    }

    submitting.value = true
    error.value = ''
    message.value = ''

    try {
        const parsed = JSON.parse(schemaJson.value) as SchemaAttribute[]
        const result = await mdmApi.putDictionarySchema(selectedDictionaryId.value, parsed)
        currentSchema.value = result.attributes
        schemaJson.value = JSON.stringify(result.attributes, null, 2)
        message.value = 'Схема обновлена'
    } catch (err) {
        error.value = formatError(err)
    } finally {
        submitting.value = false
    }
}

onMounted(async () => {
    loading.value = true
    error.value = ''
    try {
        await loadDictionaries()
        await loadSchema()
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
                <h1>Схема справочника</h1>
                <p class="muted">Просмотр и обновление `dictionary_attributes` через JSON-редактор.</p>
            </div>
            <button class="btn" :disabled="loading || !selectedDictionaryId" @click="loadSchema">Обновить</button>
        </div>

        <p v-if="message" class="alert success">{{ message }}</p>
        <p v-if="error" class="alert error">{{ error }}</p>

        <article class="card">
            <div class="form-inline">
                <label>
                    Справочник
                    <select v-model="selectedDictionaryId" @change="loadSchema">
                        <option value="">Выберите справочник</option>
                        <option v-for="dictionary in dictionaries" :key="dictionary.id" :value="dictionary.id">
                            {{ dictionary.code }}
                        </option>
                    </select>
                </label>
            </div>
        </article>

        <article class="card" :class="{ 'is-disabled': !canWrite }">
            <h3>PUT /dictionaries/{dictionary_id}/schema</h3>
            <p class="muted">Формат: массив объектов с полями `attribute_id`, `required`, `validators`, `is_unique`,
                `is_multivalue`, `position`.</p>
            <textarea v-model="schemaJson" class="code-area" :disabled="submitting || loading"></textarea>
            <div class="form-actions">
                <button class="btn primary" :disabled="!canWrite || submitting || !selectedDictionaryId"
                    @click="applySchema">
                    Применить схему
                </button>
            </div>
            <p v-if="!canWrite" class="muted">Нет прав на изменение (`mdm_editor` или `mdm_admin`).</p>
        </article>

        <article class="card">
            <h3>Текущая схема ({{ currentSchema.length }})</h3>
            <div class="table-wrap">
                <table class="table">
                    <thead>
                        <tr>
                            <th>attribute_id</th>
                            <th>required</th>
                            <th>unique</th>
                            <th>multivalue</th>
                            <th>position</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="currentSchema.length === 0">
                            <td colspan="5" class="muted">Схема пустая</td>
                        </tr>
                        <tr v-for="item in currentSchema" :key="item.attribute_id">
                            <td><code>{{ item.attribute_id }}</code></td>
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
