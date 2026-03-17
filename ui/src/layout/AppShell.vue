<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import type { DevProfileKey } from '../lib/devIdentity'
import { useDevIdentityStore } from '../stores/devIdentity'

const route = useRoute()
const devIdentity = useDevIdentityStore()

const navItems = [
  { to: '/dictionaries', label: 'Справочники', icon: 'dictionaries' },
  { to: '/attributes', label: 'Атрибуты', icon: 'attributes' },
  { to: '/objects', label: 'Объекты', icon: 'objects' },
  { to: '/audit', label: 'Аудит', icon: 'audit' },
  { to: '/health', label: 'Health', icon: 'health' },
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

const apiBaseUrl = computed(() => (import.meta.env.VITE_API_BASE_URL || '/api/v1').trim() || '/api/v1')
const environmentLabel = import.meta.env.DEV ? 'development' : 'production'
const userAvatarText = computed(() => (devIdentity.currentUserId || 'U').slice(0, 1).toUpperCase())
</script>

<template>
  <div class="app-shell">
    <aside class="app-sidebar">
      <div class="brand">
        <div class="brand-mark">MDM</div>
        <div>
          <p class="brand-title">MDM Console</p>
          <p class="brand-subtitle">Data Management UI</p>
        </div>
      </div>

      <p class="sidebar-caption">Разделы</p>
      <nav class="nav-list">
        <RouterLink
          v-for="item in navItems"
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
        <p class="sidebar-caption">Текущий контекст</p>
        <p class="sidebar-user">user={{ devIdentity.currentUserId }}</p>
        <p class="sidebar-roles">{{ devIdentity.rolesText }}</p>
      </div>
    </aside>

    <div class="app-content">
      <header class="topbar">
        <div class="topbar-main">
          <div class="topbar-left">
            <button type="button" class="icon-btn" aria-label="Menu">☰</button>
            <label class="topbar-search">
              <input type="text" placeholder="Search..." disabled />
            </label>
          </div>

          <div class="topbar-right">
            <button type="button" class="icon-btn" aria-label="Notifications">●</button>
            <span class="topbar-avatar">{{ userAvatarText }}</span>
          </div>
        </div>

        <div class="topbar-meta">
          <span class="badge">API {{ apiBaseUrl }}</span>
          <span class="badge">{{ environmentLabel }}</span>
          <span class="badge" v-if="devIdentity.activeIdentity">
            roles={{ devIdentity.rolesText }}
          </span>
          <span class="badge" v-if="devIdentity.activeIdentity">
            user={{ devIdentity.currentUserId }}
          </span>
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
