import { devIdentityToHeaders, resolveDevIdentity } from './devIdentity'

const API_BASE = normalizeBase(import.meta.env.VITE_API_BASE_URL ?? '/api/v1')
const ROOT_BASE = normalizeBase(import.meta.env.VITE_ROOT_API_BASE_URL ?? '')

function normalizeBase(value: string): string {
  const trimmed = value.trim()
  if (trimmed === '' || trimmed === '/') {
    return ''
  }
  return trimmed.endsWith('/') ? trimmed.slice(0, -1) : trimmed
}

function baseForRootPath(): string {
  return ROOT_BASE
}

export interface ApiEnvelope<T> {
  request_id: string
  data: T
}

interface ApiErrorBody {
  request_id?: string
  error?: {
    code?: string
    message?: string
    details?: unknown
  }
}

export class ApiError extends Error {
  status: number
  code: string
  details: unknown
  requestId: string | null

  constructor(status: number, code: string, message: string, details: unknown, requestId: string | null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
    this.requestId = requestId
  }
}

function makeUrl(path: string, options?: { root?: boolean }): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  if (options?.root) {
    return `${baseForRootPath()}${normalizedPath}`
  }
  return `${API_BASE}${normalizedPath}`
}

function requestHeaders(body?: BodyInit | null): Headers {
  const headers = new Headers()
  headers.set('Accept', 'application/json')
  headers.set('X-Request-Id', crypto.randomUUID())

  for (const [key, value] of Object.entries(devIdentityToHeaders(resolveDevIdentity()))) {
    headers.set(key, value)
  }

  if (body && !(body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }

  return headers
}

async function decodeJson(response: Response): Promise<unknown> {
  const contentType = response.headers.get('content-type') ?? ''
  if (!contentType.toLowerCase().includes('application/json')) {
    return null
  }

  try {
    return await response.json()
  } catch {
    return null
  }
}

async function request<T>(path: string, init: RequestInit = {}, options?: { root?: boolean }): Promise<T> {
  const url = makeUrl(path, options)
  const headers = requestHeaders(init.body ?? null)

  if (init.headers) {
    const incoming = new Headers(init.headers)
    incoming.forEach((value, key) => {
      headers.set(key, value)
    })
  }

  const response = await fetch(url, {
    ...init,
    headers,
  })

  if (response.status === 204) {
    return undefined as T
  }

  const decoded = await decodeJson(response)

  if (!response.ok) {
    const payload = decoded as ApiErrorBody | null
    const code = payload?.error?.code ?? 'http_error'
    const message = payload?.error?.message ?? `HTTP ${response.status}`
    const requestId = payload?.request_id ?? null
    throw new ApiError(response.status, code, message, payload?.error?.details, requestId)
  }

  const envelope = decoded as ApiEnvelope<T> | null
  if (!envelope || typeof envelope !== 'object' || !('data' in envelope)) {
    throw new ApiError(response.status, 'invalid_response', 'Response envelope is invalid', decoded, null)
  }

  return envelope.data
}

function queryString(query: Record<string, string | number | undefined | null>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || `${value}`.trim() === '') {
      continue
    }
    params.set(key, String(value))
  }
  const serialized = params.toString()
  return serialized ? `?${serialized}` : ''
}

export interface PageResult<T> {
  items: T[]
  total: number
  limit: number
  offset: number
}

export interface Dictionary {
  id: string
  code: string
  name: string
  description?: string
  schema_version: number
}

export interface Attribute {
  id: string
  code: string
  name: string
  description?: string
  data_type: 'string' | 'number' | 'date' | 'boolean' | 'enum' | 'reference'
  ref_dictionary_id?: string
}

export interface SchemaAttribute {
  attribute_id: string
  required: boolean
  default_value?: unknown
  validators?: Record<string, unknown>
  is_unique: boolean
  is_multivalue: boolean
  position: number
}

export interface Entry {
  id: string
  dictionary_id: string
  external_key?: string
  data: Record<string, unknown>
  version: number
}

export interface AuditEvent {
  event_id: string
  request_id?: string
  actor_external_id?: string
  actor_type: 'user' | 'service'
  action: string
  entity_type: string
  entity_id?: string
  dictionary_id?: string
  occurred_at: string
  before_state?: unknown
  after_state?: unknown
  metadata?: unknown
}

export interface SearchFilter {
  attribute: string
  op: string
  value?: unknown
  values?: unknown[]
  from?: unknown
  to?: unknown
}

export interface SearchSort {
  attribute: string
  direction?: 'asc' | 'desc'
}

export interface SearchRequest {
  filters?: SearchFilter[]
  sort?: SearchSort[]
  page?: {
    limit?: number
    offset?: number
  }
}

export interface Readiness {
  status: string
  dependencies: Record<string, string>
}

export const mdmApi = {
  health(): Promise<{ status: string }> {
    return request<{ status: string }>('/healthz', {}, { root: true })
  },

  ready(): Promise<Readiness> {
    return request<Readiness>('/readyz', {}, { root: true })
  },

  listDictionaries(limit = 50, offset = 0): Promise<PageResult<Dictionary>> {
    return request<PageResult<Dictionary>>(`/dictionaries${queryString({ limit, offset })}`)
  },

  createDictionary(input: { code: string; name: string; description?: string }): Promise<Dictionary> {
    return request<Dictionary>('/dictionaries', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  getDictionary(dictionaryId: string): Promise<Dictionary> {
    return request<Dictionary>(`/dictionaries/${dictionaryId}`)
  },

  updateDictionary(dictionaryId: string, input: { name?: string; description?: string }): Promise<Dictionary> {
    return request<Dictionary>(`/dictionaries/${dictionaryId}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  },

  deleteDictionary(dictionaryId: string): Promise<void> {
    return request<void>(`/dictionaries/${dictionaryId}`, {
      method: 'DELETE',
    })
  },

  listAttributes(limit = 50, offset = 0): Promise<PageResult<Attribute>> {
    return request<PageResult<Attribute>>(`/attributes${queryString({ limit, offset })}`)
  },

  createAttribute(input: {
    code: string
    name: string
    description?: string
    data_type: Attribute['data_type']
    ref_dictionary_id?: string
  }): Promise<Attribute> {
    return request<Attribute>('/attributes', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  getAttribute(attributeId: string): Promise<Attribute> {
    return request<Attribute>(`/attributes/${attributeId}`)
  },

  updateAttribute(attributeId: string, input: { name?: string; description?: string }): Promise<Attribute> {
    return request<Attribute>(`/attributes/${attributeId}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  },

  deleteAttribute(attributeId: string): Promise<void> {
    return request<void>(`/attributes/${attributeId}`, {
      method: 'DELETE',
    })
  },

  getDictionarySchema(dictionaryId: string): Promise<{ attributes: SchemaAttribute[] }> {
    return request<{ attributes: SchemaAttribute[] }>(`/dictionaries/${dictionaryId}/schema`)
  },

  putDictionarySchema(dictionaryId: string, attributes: SchemaAttribute[]): Promise<{ attributes: SchemaAttribute[] }> {
    return request<{ attributes: SchemaAttribute[] }>(`/dictionaries/${dictionaryId}/schema`, {
      method: 'PUT',
      body: JSON.stringify({ attributes }),
    })
  },

  listEntries(dictionaryId: string, limit = 50, offset = 0): Promise<PageResult<Entry>> {
    return request<PageResult<Entry>>(`/dictionaries/${dictionaryId}/entries${queryString({ limit, offset })}`)
  },

  createEntry(dictionaryId: string, input: { external_key?: string; data: Record<string, unknown> }): Promise<Entry> {
    return request<Entry>(`/dictionaries/${dictionaryId}/entries`, {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  getEntry(dictionaryId: string, entryId: string): Promise<Entry> {
    return request<Entry>(`/dictionaries/${dictionaryId}/entries/${entryId}`)
  },

  updateEntry(dictionaryId: string, entryId: string, data: Record<string, unknown>): Promise<Entry> {
    return request<Entry>(`/dictionaries/${dictionaryId}/entries/${entryId}`, {
      method: 'PATCH',
      body: JSON.stringify({ data }),
    })
  },

  deleteEntry(dictionaryId: string, entryId: string): Promise<void> {
    return request<void>(`/dictionaries/${dictionaryId}/entries/${entryId}`, {
      method: 'DELETE',
    })
  },

  searchEntries(dictionaryId: string, requestBody: SearchRequest): Promise<PageResult<Entry>> {
    return request<PageResult<Entry>>(`/dictionaries/${dictionaryId}/entries/search`, {
      method: 'POST',
      body: JSON.stringify(requestBody),
    })
  },

  listAuditEvents(query: {
    limit?: number
    offset?: number
    entity_type?: string
    entity_id?: string
    actor_external_id?: string
    occurred_from?: string
    occurred_to?: string
  }): Promise<PageResult<AuditEvent>> {
    return request<PageResult<AuditEvent>>(`/audit/events${queryString(query)}`)
  },
}
