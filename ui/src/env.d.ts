/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string
  readonly VITE_ROOT_API_BASE_URL?: string
  readonly VITE_DEV_USER_ID?: string
  readonly VITE_DEV_USER_ROLES?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
