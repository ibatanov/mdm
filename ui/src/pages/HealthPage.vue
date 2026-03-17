<script setup lang="ts">
import { onMounted, ref } from 'vue'

import JsonBox from '../components/JsonBox.vue'
import { type Readiness, mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'

const loading = ref(false)
const error = ref('')
const healthStatus = ref('unknown')
const readiness = ref<Readiness | null>(null)

async function load(): Promise<void> {
    loading.value = true
    error.value = ''

    try {
        const [health, ready] = await Promise.all([mdmApi.health(), mdmApi.ready()])
        healthStatus.value = health.status
        readiness.value = ready
    } catch (err) {
        error.value = formatError(err)
    } finally {
        loading.value = false
    }
}

onMounted(load)
</script>

<template>
    <section>
        <div class="section-head">
            <div>
                <h1>Health</h1>
                <p class="muted">Проверка доступности сервиса и зависимостей.</p>
            </div>
            <button class="btn" :disabled="loading" @click="load">Обновить</button>
        </div>

        <p v-if="error" class="alert error">{{ error }}</p>

        <div class="cards grid-3">
            <article class="card">
                <h3>/healthz</h3>
                <p>
                    <span class="pill" :class="healthStatus === 'ok' ? 'success' : 'warning'">
                        {{ healthStatus }}
                    </span>
                </p>
            </article>

            <article class="card">
                <h3>/readyz</h3>
                <p>
                    <span class="pill" :class="readiness?.status === 'ok' ? 'success' : 'warning'">
                        {{ readiness?.status ?? 'unknown' }}
                    </span>
                </p>
            </article>

            <article class="card">
                <h3>Зависимости</h3>
                <p class="muted">postgres: {{ readiness?.dependencies.postgres ?? 'unknown' }}</p>
                <p class="muted">kafka: {{ readiness?.dependencies.kafka ?? 'unknown' }}</p>
            </article>
        </div>

        <article class="card">
            <h3>Raw /readyz</h3>
            <JsonBox :value="readiness" label="readiness" :open="true" />
        </article>
    </section>
</template>
