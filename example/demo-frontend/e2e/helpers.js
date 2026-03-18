// @ts-check
import { expect } from '@playwright/test'

// IAM endpoint — direct access to the OIDC provider (tenant: demo).
// The demo-frontend nginx does NOT proxy /auth; the Vue app calls IAM directly.
const IAM_BASE = 'http://localhost:8080/t/demo'

// Demo backend API — proxied through demo-frontend nginx at /api.
const API_BASE = 'http://localhost:4000/api'

// Seeded demo account — from tenant-demo.json (member role).
export const DEMO_EMAIL = 'alice@demo.app'
export const DEMO_PASSWORD = 'alice1234'

// Admin credentials for the demo tenant — from tenant-demo.json.
export const ADMIN_EMAIL = 'admin@demo.app'
export const ADMIN_PASSWORD = 'admin1234'

// ─── UI helpers ──────────────────────────────────────────────────────────────

/**
 * Log in via the UI and wait for /dashboard.
 * @param {import('@playwright/test').Page} page
 * @param {string} email
 * @param {string} password
 */
export async function login(page, email, password) {
  await page.goto('/login')
  await page.getByPlaceholder(/email/i).fill(email)
  await page.getByPlaceholder(/password/i).fill(password)
  await page.getByRole('button', { name: /sign in|log in|login/i }).click()
  await expect(page).toHaveURL('/dashboard', { timeout: 15000 })
}

/**
 * Log in as the seeded demo user.
 * @param {import('@playwright/test').Page} page
 */
export async function loginAsDemo(page) {
  await login(page, DEMO_EMAIL, DEMO_PASSWORD)
}

// ─── API helpers ──────────────────────────────────────────────────────────────

/**
 * Submit the early-access signup form via the demo-backend API.
 * Returns { status, email, invite_token }.
 * @param {string} email
 * @param {string} name
 * @returns {Promise<object>}
 */
export async function signupContact(email, name) {
  const res = await fetch(`${API_BASE}/subscribe`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, name }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`signupContact failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Activate an account via the demo-backend API (set password for invite token).
 * @param {string} token
 * @param {string} password
 * @returns {Promise<object>}
 */
export async function activateAccount(token, password) {
  const res = await fetch(`${API_BASE}/activate/${token}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`activateAccount failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Obtain an access token for a user via IAM login.
 * @param {string} email
 * @param {string} password
 * @returns {Promise<string>} access_token
 */
export async function getToken(email, password) {
  const res = await fetch(`${IAM_BASE}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`getToken failed (${res.status}): ${body}`)
  }
  const data = await res.json()
  return data.access_token
}

/**
 * Get an admin access token for cleanup operations.
 * @returns {Promise<string>} access_token
 */
export async function getAdminToken() {
  return getToken(ADMIN_EMAIL, ADMIN_PASSWORD)
}

/**
 * Delete a user via the IAM admin API.
 * @param {string} adminToken
 * @param {string} userId
 */
export async function deleteUserApi(adminToken, userId) {
  const res = await fetch(`${IAM_BASE}/admin/users/${userId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${adminToken}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteUserApi failed (${res.status}): ${body}`)
  }
}

/**
 * Delete a contact via the IAM admin API.
 * @param {string} adminToken
 * @param {string} contactId
 */
export async function deleteContactApi(adminToken, contactId) {
  const res = await fetch(`${IAM_BASE}/admin/contacts/${contactId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${adminToken}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteContactApi failed (${res.status}): ${body}`)
  }
}

/**
 * Find and delete a user by email via admin API. Best-effort.
 * @param {string} adminToken
 * @param {string} email
 */
export async function findAndDeleteUser(adminToken, email) {
  const res = await fetch(`${IAM_BASE}/admin/users`, {
    headers: { Authorization: `Bearer ${adminToken}` },
  })
  if (!res.ok) return
  const users = await res.json()
  const match = (Array.isArray(users) ? users : users.items ?? users.data ?? [])
    .find((u) => u.email === email)
  if (match) await deleteUserApi(adminToken, match.id)
}

/**
 * Find and delete a contact by email via admin API. Best-effort.
 * @param {string} adminToken
 * @param {string} email
 */
export async function findAndDeleteContact(adminToken, email) {
  const res = await fetch(`${IAM_BASE}/admin/contacts`, {
    headers: { Authorization: `Bearer ${adminToken}` },
  })
  if (!res.ok) return
  const contacts = await res.json()
  const match = (Array.isArray(contacts) ? contacts : contacts.items ?? contacts.data ?? [])
    .find((c) => c.email === email)
  if (match) await deleteContactApi(adminToken, match.id)
}

/**
 * Return a unique email address for use in a single test run.
 * @param {string} prefix
 * @returns {string}
 */
export function uniqueEmail(prefix = 'test') {
  return `${prefix}-${Date.now()}@example.com`
}
