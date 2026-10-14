// Typed API client.
//
// Two rules this module exists to enforce:
//   1. Every request is same-origin and sends cookies (`credentials: 'include'`).
//   2. Every mutation carries the CSRF header, read from the readable CSRF cookie
//      the server sets alongside the session. A write without it is rejected 403.
//
// Response shapes mirror openapi/openapi.yaml; `pnpm openapi-types` regenerates
// src/api/schema.ts from that file when the contract changes.

export type ApiErrorBody = {
  error: {
    code: string
    message: string
    details?: { field: string; message: string }[]
    request_id?: string
  }
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: { field: string; message: string }[]
  readonly requestId?: string

  constructor(status: number, body: Partial<ApiErrorBody> | undefined, fallback: string) {
    const err = body?.error
    super(err?.message ?? fallback)
    this.name = 'ApiError'
    this.status = status
    this.code = err?.code ?? 'unknown'
    this.details = err?.details ?? []
    this.requestId = err?.request_id
  }

  /** A per-field message for embedding next to an input. */
  fieldError(field: string): string | undefined {
    return this.details.find((d) => d.field === field)?.message
  }
}

const CSRF_COOKIE = 'hanpath_csrf'
const CSRF_HEADER = 'X-CSRF-Token'

export function readCookie(name: string): string | undefined {
  const match = document.cookie.split('; ').find((c) => c.startsWith(`${name}=`))
  return match ? decodeURIComponent(match.slice(name.length + 1)) : undefined
}

type RequestOptions = {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'

  if (method !== 'GET') {
    const csrf = readCookie(CSRF_COOKIE)
    if (csrf) headers[CSRF_HEADER] = csrf
  }

  const response = await fetch(path, {
    method,
    headers,
    credentials: 'include',
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  })

  if (response.status === 204) return undefined as T

  const text = await response.text()
  const payload = text ? safeParse(text) : undefined

  if (!response.ok) {
    throw new ApiError(response.status, payload as ApiErrorBody | undefined, response.statusText)
  }
  return payload as T
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}

/** True when the error means "not signed in", which the app routes on. */
export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}

/**
 * Upload a multipart body (a learner's recording, today) with the same rules as
 * `api`: same-origin, cookies included, CSRF header on the mutation, and the same
 * `ApiError` on failure so callers can branch on the server's error code.
 *
 * The body is deliberately not passed through `JSON.stringify`, which is the only
 * reason this is a second function rather than an option on the first.
 */
export async function apiUpload<T>(path: string, form: FormData, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const csrf = readCookie(CSRF_COOKIE)
  if (csrf) headers[CSRF_HEADER] = csrf

  const response = await fetch(path, {
    method: 'POST',
    headers,
    credentials: 'include',
    body: form,
    signal,
  })

  const text = await response.text()
  const payload = text ? safeParse(text) : undefined
  if (!response.ok) {
    throw new ApiError(response.status, payload as ApiErrorBody | undefined, response.statusText)
  }
  return payload as T
}
