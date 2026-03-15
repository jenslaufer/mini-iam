// @ts-check
import { expect } from '@playwright/test'

const ADMIN_EMAIL = 'admin@mini-iam.local'
const ADMIN_PASSWORD = 'changeme'

/**
 * Log in as the seeded admin user and wait for the dashboard.
 * @param {import('@playwright/test').Page} page
 */
export async function loginAsAdmin(page) {
  await page.goto('/login')
  await page.getByPlaceholder('admin@example.com').fill(ADMIN_EMAIL)
  await page.getByPlaceholder('••••••••').fill(ADMIN_PASSWORD)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL('/dashboard')
}

/**
 * Register a regular user via the public API.
 * @param {string} baseURL
 * @param {string} email
 * @param {string} password
 * @param {string} name
 * @returns {Promise<object>} Created user object
 */
export async function registerUser(baseURL, email, password, name) {
  const res = await fetch(`${baseURL}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, name }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`registerUser failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Obtain an admin access token via the API.
 * @param {string} baseURL
 * @returns {Promise<string>} access_token
 */
export async function getAdminToken(baseURL) {
  const res = await fetch(`${baseURL}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email: ADMIN_EMAIL, password: ADMIN_PASSWORD }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`getAdminToken failed (${res.status}): ${body}`)
  }
  const data = await res.json()
  return data.access_token
}

/**
 * Create an OAuth2 client via the API using an admin token.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} name
 * @param {string[]} redirectUris
 * @returns {Promise<object>} Created client object (includes client_secret)
 */
export async function createClient(baseURL, token, name, redirectUris) {
  const res = await fetch(`${baseURL}/auth/clients`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ name, redirect_uris: redirectUris }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`createClient failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Delete a user via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} userId
 */
export async function deleteUserApi(baseURL, token, userId) {
  const res = await fetch(`${baseURL}/auth/admin/users/${userId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteUser failed (${res.status}): ${body}`)
  }
}

/**
 * Delete a client via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} clientId
 */
export async function deleteClientApi(baseURL, token, clientId) {
  const res = await fetch(`${baseURL}/auth/admin/clients/${clientId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteClient failed (${res.status}): ${body}`)
  }
}
