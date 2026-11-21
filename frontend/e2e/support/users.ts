import { randomBytes } from 'node:crypto'
import type { Api } from '../api/client'

export interface NewUser {
  fullName: string
  email: string
  password: string
}

/** A password that satisfies the server's rules: upper, lower, digit, 8+ characters. */
export function strongPassword(): string {
  return `${randomBytes(8).toString('hex')}Aa1`
}

export function newUser(tag: string): NewUser {
  return {
    fullName: `E2E ${tag}`,
    email: `${tag}-${randomBytes(3).toString('hex')}@e2e.local`.toLowerCase(),
    password: strongPassword(),
  }
}

/**
 * Creates an account over the API. Tests use throw-away users for anything
 * that can lock an account (repeated wrong passwords), never the admin.
 */
export async function createUser(api: Api, user: NewUser): Promise<void> {
  const cookies = (await api.http.storageState()).cookies
  const csrf = cookies.find((c) => c.name === 'csrf_token')?.value
  const response = await api.http.post('/api/v1/auth/sign-up', {
    data: user,
    headers: csrf ? { 'X-CSRF-Token': csrf } : {},
  })
  if (!response.ok()) {
    throw new Error(`could not create ${user.email}: ${response.status()} ${await response.text()}`)
  }
}
