import { ApiError } from './api'

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

interface ValidationIssuePayload {
  field?: unknown
  code?: unknown
  message?: unknown
}

function stringifyIssue(issue: ValidationIssuePayload): string {
  const field = typeof issue.field === 'string' ? issue.field.trim() : ''
  const message = typeof issue.message === 'string' ? issue.message.trim() : ''
  const code = typeof issue.code === 'string' ? issue.code.trim() : ''

  if (field && message) {
    return `${field}: ${message}`
  }
  if (message) {
    return message
  }
  if (field && code) {
    return `${field}: ${code}`
  }
  if (field) {
    return field
  }
  if (code) {
    return code
  }
  return ''
}

export function extractValidationIssues(error: unknown): string[] {
  if (!isApiError(error)) {
    return []
  }
  if (error.code !== 'validation_failed') {
    return []
  }

  const details = error.details
  if (!details || typeof details !== 'object' || Array.isArray(details)) {
    return []
  }

  const rawIssues = (details as Record<string, unknown>).issues
  if (!Array.isArray(rawIssues)) {
    return []
  }

  return rawIssues
    .map((raw): string => {
      if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
        return ''
      }
      return stringifyIssue(raw as ValidationIssuePayload)
    })
    .filter((line) => line !== '')
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
