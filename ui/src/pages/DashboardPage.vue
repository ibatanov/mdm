<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { RefreshCw } from 'lucide-vue-next'

import { mdmApi } from '../lib/api'
import { formatError } from '../lib/errors'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

const loading = ref(false)
const error = ref('')
const identity = useDevIdentityStore()

const readyStatus = ref('unknown')
const postgresStatus = ref('unknown')
const kafkaStatus = ref('unknown')

const dictionariesTotal = ref(0)
const attributesTotal = ref(0)

const canManageData = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor']))
const canReadData = computed(
  () => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_editor', 'mdm_viewer']),
)
const canReadAudit = computed(() => !identity.isDev || hasAnyRole(identity.currentRoles, ['mdm_admin', 'mdm_auditor']))

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
        <h1>Панель управления</h1>
        <p class="muted">Сводка по системе и быстрый переход к основным шагам настройки MDM.</p>
      </div>
      <button class="btn" :disabled="loading" @click="loadDashboard">
        <RefreshCw class="btn-icon" :size="16" aria-hidden="true" />
        Обновить
      </button>
    </div>

    <p v-if="error" class="alert error">{{ error }}</p>

    <div class="cards grid-3">
      <article class="card">
        <h3>Готовность API</h3>
        <p><span :class="readyClass">{{ readyStatus }}</span></p>
        <p class="muted">Postgres: {{ postgresStatus }}</p>
        <p class="muted">Kafka: {{ kafkaStatus }}</p>
      </article>

      <article class="card">
        <h3>Справочники</h3>
        <p class="kpi">{{ dictionariesTotal }}</p>
        <RouterLink class="link" to="/dictionaries">Открыть раздел</RouterLink>
      </article>

      <article class="card">
        <h3>Атрибуты</h3>
        <p class="kpi">{{ attributesTotal }}</p>
        <RouterLink class="link" to="/attributes">Открыть раздел</RouterLink>
      </article>
    </div>

    <article class="card">
      <div class="card-title-line">
        <h3>Быстрый сценарий запуска</h3>
      </div>
      <div class="onboarding-grid">
        <RouterLink class="quick-step" to="/dictionaries" :class="{ 'is-disabled': !canReadData }">
          <span class="step-index">1</span>
          <div>
            <p class="step-title">Создайте справочник</p>
            <p class="muted">Например: товары, бренды, категории</p>
          </div>
        </RouterLink>
        <RouterLink class="quick-step" to="/attributes" :class="{ 'is-disabled': !canReadData }">
          <span class="step-index">2</span>
          <div>
            <p class="step-title">Добавьте атрибуты</p>
            <p class="muted">Типы полей: строка, число, дата, enum, reference</p>
          </div>
        </RouterLink>
        <RouterLink class="quick-step" to="/objects" :class="{ 'is-disabled': !canReadData }">
          <span class="step-index">3</span>
          <div>
            <p class="step-title">Наполните объектами</p>
            <p class="muted">Создание, поиск, редактирование и обновление</p>
          </div>
        </RouterLink>
        <RouterLink class="quick-step" to="/audit" :class="{ 'is-disabled': !canReadAudit }">
          <span class="step-index">4</span>
          <div>
            <p class="step-title">Проверьте аудит</p>
            <p class="muted">Кто и когда менял данные</p>
          </div>
        </RouterLink>
      </div>
    </article>

    <article class="card">
      <h3>Права в текущем контексте</h3>
      <p class="muted">Изменение данных: <strong>{{ canManageData ? 'доступно' : 'нет доступа' }}</strong></p>
      <p class="muted">Чтение справочников/объектов: <strong>{{ canReadData ? 'доступно' : 'нет доступа' }}</strong></p>
      <p class="muted">Просмотр аудита: <strong>{{ canReadAudit ? 'доступно' : 'нет доступа' }}</strong></p>
    </article>
  </section>
</template>
