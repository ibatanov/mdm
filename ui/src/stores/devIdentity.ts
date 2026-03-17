import { defineStore } from 'pinia'

import {
  DEV_PROFILE_PRESETS,
  type DevIdentitySettings,
  type DevProfileKey,
  loadDevIdentitySettings,
  parseRoles,
  presetByKey,
  resolveDevIdentity,
  saveDevIdentitySettings,
} from '../lib/devIdentity'

interface DevIdentityState {
  selectedProfile: DevProfileKey
  customUserId: string
  customRoles: string
}

function toState(settings: DevIdentitySettings): DevIdentityState {
  return {
    selectedProfile: settings.selectedProfile,
    customUserId: settings.customUserId,
    customRoles: settings.customRoles,
  }
}

export const useDevIdentityStore = defineStore('dev-identity', {
  state: (): DevIdentityState => toState(loadDevIdentitySettings()),

  getters: {
    isDev(): boolean {
      return import.meta.env.DEV
    },

    profileOptions() {
      return [
        ...DEV_PROFILE_PRESETS.map((preset) => ({ key: preset.key, label: preset.label })),
        { key: 'custom' as const, label: 'custom' },
      ]
    },

    activeIdentity(state) {
      return resolveDevIdentity({
        selectedProfile: state.selectedProfile,
        customUserId: state.customUserId,
        customRoles: state.customRoles,
      })
    },

    currentRoles(): string[] {
      return this.activeIdentity?.roles ?? []
    },

    rolesText(): string {
      return this.currentRoles.join(', ')
    },

    currentUserId(): string {
      return this.activeIdentity?.userId ?? 'n/a'
    },
  },

  actions: {
    persist(): void {
      saveDevIdentitySettings({
        selectedProfile: this.selectedProfile,
        customUserId: this.customUserId,
        customRoles: this.customRoles,
      })
    },

    setProfile(profile: DevProfileKey): void {
      this.selectedProfile = profile
      if (profile !== 'custom') {
        const preset = presetByKey(profile)
        this.customUserId = preset.identity.userId
        this.customRoles = preset.identity.roles.join(',')
      }
      this.persist()
    },

    setCustomUserId(value: string): void {
      this.customUserId = value.trim()
      this.persist()
    },

    setCustomRoles(value: string): void {
      this.customRoles = parseRoles(value).join(',')
      this.persist()
    },
  },
})
