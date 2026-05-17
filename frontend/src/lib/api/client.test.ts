import { describe, it, expect, beforeEach } from 'vitest'
import { encodeQueryString, buildApiPath, getSession, saveSession, clearSession } from './client'
import type { AuthResponse } from './types'

const fakeSession: AuthResponse = {
  token: 'tok123',
  user: {
    userId: 'u1',
    workspaceId: 'w1',
    username: 'alice',
    email: 'alice@example.com',
    fullName: 'Alice',
    isAdmin: false,
    createdAt: '2024-01-01T00:00:00Z',
  },
  workspace: {
    workspaceId: 'w1',
    slug: 'alice',
    name: "Alice's workspace",
    createdAt: '2024-01-01T00:00:00Z',
  },
  expiresAt: '2024-01-04T00:00:00Z',
}

describe('encodeQueryString', () => {
  it('returns empty string for empty params', () => {
    expect(encodeQueryString({})).toBe('')
  })

  it('encodes a single param', () => {
    expect(encodeQueryString({ foo: 'bar' })).toBe('foo=bar')
  })

  it('sorts params alphabetically', () => {
    expect(encodeQueryString({ z: '1', a: '2' })).toBe('a=2&z=1')
  })

  it('skips null, undefined, and empty-string values', () => {
    expect(encodeQueryString({ a: null, b: undefined, c: '', d: 'keep' })).toBe('d=keep')
  })

  it('encodes array values as repeated keys', () => {
    const result = encodeQueryString({ tags: ['x', 'y'] })
    expect(result).toBe('tags=x&tags=y')
  })

  it('URL-encodes special characters', () => {
    expect(encodeQueryString({ q: 'hello world' })).toBe('q=hello%20world')
  })
})

describe('buildApiPath', () => {
  it('returns path unchanged when no params given', () => {
    expect(buildApiPath('/api/v1/resource')).toBe('/api/v1/resource')
  })

  it('appends a query string when params are provided', () => {
    expect(buildApiPath('/api/v1/resource', { id: '42' })).toBe('/api/v1/resource?id=42')
  })

  it('uses & when path already contains ?', () => {
    expect(buildApiPath('/api/v1/resource?existing=1', { next: '2' })).toBe('/api/v1/resource?existing=1&next=2')
  })
})

describe('session management', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('getSession returns null when nothing is stored', () => {
    expect(getSession()).toBeNull()
  })

  it('saveSession persists and getSession retrieves the session', () => {
    saveSession(fakeSession)
    const retrieved = getSession()
    expect(retrieved).not.toBeNull()
    expect(retrieved?.token).toBe('tok123')
    expect(retrieved?.user.email).toBe('alice@example.com')
  })

  it('clearSession removes the stored session', () => {
    saveSession(fakeSession)
    clearSession()
    expect(getSession()).toBeNull()
  })
})
