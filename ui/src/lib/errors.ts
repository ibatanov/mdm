import { ApiError } from './api'

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

export function formatError(error: unknown): string {
  if (isApiError(error)) {
    const suffix = error.requestId ? ` (request_id: ${error.requestId})` : ''
    return `${error.code}: ${error.message}${suffix}`
  }

  if (error instanceof Error) {
    return error.message
  }

  return 'Unknown error'
}

export function prettyJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}
