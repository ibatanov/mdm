<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import type { DevProfileKey } from '../lib/devIdentity'
import { hasAnyRole } from '../lib/rbac'
import { useDevIdentityStore } from '../stores/devIdentity'

const route = useRoute()
const devIdentity = useDevIdentityStore()
const sidebarOpen = ref(false)

const navItems = [
  {
    to: '/dashboard',
    label: 'Панель',
    icon: 'dashboard',
    allowedRoles: ['mdm_admin', 'mdm_editor', 'mdm_viewer', 'mdm_auditor'],
  },
  {
    to: '/dictionaries',
    label: 'Справочники',
    icon: 'dictionaries',
    allowedRoles: ['mdm_admin', 'mdm_editor', 'mdm_viewer'],
  },
  {
    to: '/attributes',
    label: 'Атрибуты',
    icon: 'attributes',
    allowedRoles: ['mdm_admin', 'mdm_editor', 'mdm_viewer'],
  },
  {
    to: '/objects',
    label: 'Объекты',
    icon: 'objects',
    allowedRoles: ['mdm_admin', 'mdm_editor', 'mdm_viewer'],
  },
  {
    to: '/audit',
    label: 'Аудит',
    icon: 'audit',
    allowedRoles: ['mdm_admin', 'mdm_auditor'],
  },
  {
    to: '/health',
    label: 'Сервис',
    icon: 'health',
    allowedRoles: ['mdm_admin', 'mdm_editor', 'mdm_viewer', 'mdm_auditor'],
  },
]

const selectedProfile = computed({
  get: () => devIdentity.selectedProfile,
  set: (value: string) => devIdentity.setProfile(value as DevProfileKey),
})

const customUserId = computed({
  get: () => devIdentity.customUserId,
  set: (value: string) => devIdentity.setCustomUserId(value),
})

const customRoles = computed({
  get: () => devIdentity.customRoles,
  set: (value: string) => devIdentity.setCustomRoles(value),
})

function isCurrentPath(path: string): boolean {
  return route.path === path || route.path.startsWith(`${path}/`)
}

const availableNavItems = computed(() => {
  if (!devIdentity.isDev) {
    return navItems
  }
  return navItems.filter((item) => hasAnyRole(devIdentity.currentRoles, item.allowedRoles))
})

const currentPageTitle = computed(() => {
  const current = navItems.find((item) => isCurrentPath(item.to))
  return current?.label ?? 'MDM Console'
})

const currentPageDescription = computed(() => {
  if (isCurrentPath('/dictionaries')) return 'Создание и настройка справочников'
  if (isCurrentPath('/attributes')) return 'Каталог атрибутов и их типы'
  if (isCurrentPath('/objects')) return 'Работа с объектами по схеме'
  if (isCurrentPath('/audit')) return 'Контроль изменений и истории'
  if (isCurrentPath('/health')) return 'Состояние API и зависимостей'
  return 'Управление мастер-данными'
})

const activeRoleLabel = computed(() => {
  if (!devIdentity.activeIdentity) {
    return 'gateway mode'
  }
  return devIdentity.rolesText || 'роль не выбрана'
})

const apiBaseUrl = computed(() => (import.meta.env.VITE_API_BASE_URL || '/api/v1').trim() || '/api/v1')
const environmentLabel = import.meta.env.DEV ? 'development' : 'production'
const userAvatarText = computed(() => (devIdentity.currentUserId || 'U').slice(0, 1).toUpperCase())

function closeSidebar(): void {
  sidebarOpen.value = false
}

watch(
  () => route.fullPath,
  () => {
    closeSidebar()
  },
)
</script>

<template>
  <div class="app-shell" :class="{ 'sidebar-open': sidebarOpen }">
    <button class="mobile-overlay" type="button" @click="closeSidebar" aria-label="Закрыть меню"></button>

    <aside class="app-sidebar">
      <div class="brand">
        <div class="brand-mark">MDM</div>
        <div>
          <p class="brand-title">MDM Console</p>
          <p class="brand-subtitle">Легкое управление справочниками</p>
        </div>
      </div>

      <p class="sidebar-caption">Разделы</p>
      <nav class="nav-list">
        <RouterLink
          v-for="item in availableNavItems"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          :class="{ 'is-active': isCurrentPath(item.to) }"
        >
          <span class="nav-icon" :class="`is-${item.icon}`"></span>
          <span class="nav-label">{{ item.label }}</span>
        </RouterLink>
      </nav>

      <div v-if="devIdentity.activeIdentity" class="sidebar-profile">
        <p class="sidebar-caption">Контекст доступа</p>
        <p class="sidebar-user">user={{ devIdentity.currentUserId }}</p>
        <p class="sidebar-roles">{{ activeRoleLabel }}</p>
      </div>

      <div class="sidebar-note">
        <p class="sidebar-caption">Подсказка</p>
        <p class="muted">Начните со справочника, затем задайте атрибуты, схему и только после этого добавляйте объекты.</p>
      </div>
    </aside>

    <div class="app-content">
      <header class="topbar">
        <div class="topbar-main">
          <div class="topbar-left">
            <button type="button" class="icon-btn menu-btn" aria-label="Открыть меню" @click="sidebarOpen = !sidebarOpen">
              ☰
            </button>
            <div>
              <p class="topbar-title">{{ currentPageTitle }}</p>
              <p class="topbar-subtitle muted">{{ currentPageDescription }}</p>
            </div>
          </div>

          <div class="topbar-right">
            <span class="badge">{{ environmentLabel }}</span>
            <span class="badge">role={{ activeRoleLabel }}</span>
            <span class="topbar-avatar">{{ userAvatarText }}</span>
          </div>
        </div>

        <div class="topbar-meta">
          <span class="badge">API {{ apiBaseUrl }}</span>
          <span class="badge" v-if="devIdentity.activeIdentity">user={{ devIdentity.currentUserId }}</span>
        </div>

        <div v-if="devIdentity.isDev" class="dev-inline">
          <label class="dev-inline-field">
            Dev profile
            <select v-model="selectedProfile">
              <option v-for="item in devIdentity.profileOptions" :key="item.key" :value="item.key">
                {{ item.label }}
              </option>
            </select>
          </label>

          <label v-if="selectedProfile === 'custom'" class="dev-inline-field">
            X-User-Id
            <input v-model="customUserId" placeholder="100" />
          </label>

          <label v-if="selectedProfile === 'custom'" class="dev-inline-field">
            X-User-Role
            <input v-model="customRoles" placeholder="mdm_admin,mdm_editor" />
          </label>
        </div>
      </header>

      <main class="page slide-up">
        <slot />
      </main>
    </div>
  </div>
</template>
