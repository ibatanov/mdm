<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'

import { mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'

const loading = ref(false)
const error = ref('')

const readyStatus = ref('unknown')
const postgresStatus = ref('unknown')
const kafkaStatus = ref('unknown')

const dictionariesTotal = ref(0)
const attributesTotal = ref(0)

async function loadDashboard(): Promise<void> {
  loading.value = true
  error.value = ''

  try {
    const [ready, dictionaries, attributes] = await Promise.all([
      mdmApi.ready(),
      mdmApi.listDictionaries(1, 0),
      mdmApi.listAttributes(1, 0),
    ])

    readyStatus.value = ready.status
    postgresStatus.value = ready.dependencies.postgres ?? 'unknown'
    kafkaStatus.value = ready.dependencies.kafka ?? 'unknown'

    dictionariesTotal.value = dictionaries.total
    attributesTotal.value = attributes.total
  } catch (err) {
    error.value = formatError(err)
  } finally {
    loading.value = false
  }
}

const readyClass = computed(() => (readyStatus.value === 'ok' ? 'pill success' : 'pill warning'))

onMounted(loadDashboard)
</script>

<template>
  <section>
    <div class="section-head">
      <div>
        <h1>Dashboard</h1>
        <p class="muted">Обзор состояния MDM и быстрые переходы по разделам.</p>
      </div>
      <button class="btn" :disabled="loading" @click="loadDashboard">Обновить</button>
    </div>

    <p v-if="error" class="alert error">{{ error }}</p>

    <div class="cards grid-3">
      <article class="card">
        <h3>Ready</h3>
        <p><span :class="readyClass">{{ readyStatus }}</span></p>
        <p class="muted">Postgres: {{ postgresStatus }}</p>
        <p class="muted">Kafka: {{ kafkaStatus }}</p>
      </article>

      <article class="card">
        <h3>Справочники</h3>
        <p class="kpi">{{ dictionariesTotal }}</p>
        <RouterLink class="link" to="/dictionaries">Перейти к списку</RouterLink>
      </article>

      <article class="card">
        <h3>Атрибуты</h3>
        <p class="kpi">{{ attributesTotal }}</p>
        <RouterLink class="link" to="/attributes">Перейти к списку</RouterLink>
      </article>
    </div>
  </section>
</template>
