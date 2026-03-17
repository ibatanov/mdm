export type DevProfileKey = 'admin' | 'editor' | 'viewer' | 'auditor' | 'custom'

export interface DevIdentity {
  userId: string
  roles: string[]
}

export interface DevProfilePreset {
  key: Exclude<DevProfileKey, 'custom'>
  label: string
  identity: DevIdentity
}

export interface DevIdentitySettings {
  selectedProfile: DevProfileKey
  customUserId: string
  customRoles: string
}

const STORAGE_KEY = 'mdm.ui.dev.identity.v1'

export const DEV_PROFILE_PRESETS: DevProfilePreset[] = [
  {
    key: 'admin',
    label: 'admin',
    identity: { userId: '100', roles: ['mdm_admin'] },
  },
  {
    key: 'editor',
    label: 'editor',
    identity: { userId: '101', roles: ['mdm_editor'] },
  },
  {
    key: 'viewer',
    label: 'viewer',
    identity: { userId: '102', roles: ['mdm_viewer'] },
  },
  {
    key: 'auditor',
    label: 'auditor',
    identity: { userId: '103', roles: ['mdm_auditor'] },
  },
]

function safeWindow(): Window | null {
  if (typeof window === 'undefined') {
    return null
  }
  return window
}

function clean(value: string | null | undefined): string {
  return (value ?? '').trim()
}

export function parseRoles(value: string | string[] | null | undefined): string[] {
  if (Array.isArray(value)) {
    return value
      .map((entry) => clean(entry))
      .filter((entry) => entry.length > 0)
  }

  return clean(value)
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
}

export function defaultSettings(): DevIdentitySettings {
  return {
    selectedProfile: 'admin',
    customUserId: clean(import.meta.env.VITE_DEV_USER_ID),
    customRoles: clean(import.meta.env.VITE_DEV_USER_ROLES),
  }
}

export function loadDevIdentitySettings(): DevIdentitySettings {
  const defaults = defaultSettings()
  const win = safeWindow()
  if (!win) {
    return defaults
  }

  try {
    const raw = win.localStorage.getItem(STORAGE_KEY)
    if (!raw) {
      return defaults
    }

    const parsed = JSON.parse(raw) as Partial<DevIdentitySettings>
    const selectedProfile = isDevProfileKey(parsed.selectedProfile) ? parsed.selectedProfile : defaults.selectedProfile

    return {
      selectedProfile,
      customUserId: clean(parsed.customUserId) || defaults.customUserId,
      customRoles: clean(parsed.customRoles) || defaults.customRoles,
    }
  } catch {
    return defaults
  }
}

export function saveDevIdentitySettings(settings: DevIdentitySettings): void {
  const win = safeWindow()
  if (!win) {
    return
  }
  win.localStorage.setItem(STORAGE_KEY, JSON.stringify(settings))
}

export function isDevProfileKey(value: unknown): value is DevProfileKey {
  return value === 'admin' || value === 'editor' || value === 'viewer' || value === 'auditor' || value === 'custom'
}

export function presetByKey(key: Exclude<DevProfileKey, 'custom'>): DevProfilePreset {
  const preset = DEV_PROFILE_PRESETS.find((entry) => entry.key === key)
  if (!preset) {
    return DEV_PROFILE_PRESETS[0]
  }
  return preset
}

function customIdentity(settings: DevIdentitySettings): DevIdentity | null {
  const userId = clean(settings.customUserId)
  const roles = parseRoles(settings.customRoles)
  if (!userId || roles.length === 0) {
    return null
  }
  return { userId, roles }
}

export function resolveDevIdentity(settingsArg?: DevIdentitySettings): DevIdentity | null {
  if (!import.meta.env.DEV) {
    return null
  }

  const settings = settingsArg ?? loadDevIdentitySettings()
  if (settings.selectedProfile === 'custom') {
    return customIdentity(settings)
  }

  return presetByKey(settings.selectedProfile).identity
}

export function devIdentityToHeaders(identity: DevIdentity | null): Record<string, string> {
  if (!identity) {
    return {}
  }

  return {
    'X-User-Id': identity.userId,
    'X-User-Role': identity.roles.join(','),
  }
}
