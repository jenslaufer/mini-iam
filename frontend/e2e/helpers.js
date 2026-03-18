// @ts-check
import { expect } from '@playwright/test'

const ADMIN_EMAIL = 'admin@launch-kit.local'
const ADMIN_PASSWORD = 'changeme'

/**
 * Log in as the seeded admin user and wait for the dashboard.
 * @param {import('@playwright/test').Page} page
 */
export async function loginAsAdmin(page) {
  await page.goto('/login')
  // Leave Tenant field empty — platform admin login
  await page.getByPlaceholder('Leave empty for platform admin').fill('')
  await page.getByPlaceholder('admin@example.com').fill(ADMIN_EMAIL)
  await page.getByPlaceholder('••••••••').fill(ADMIN_PASSWORD)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL('/dashboard', { timeout: 15000 })
  // Wait for tenant store to initialize (platform admin loads tenant list)
  await page.waitForFunction(
    () => {
      const raw = localStorage.getItem('selected_tenant')
      return raw && raw.length > 0
    },
    { timeout: 10000 },
  )
}

/**
 * Log in as a tenant-scoped admin and wait for the dashboard.
 * @param {import('@playwright/test').Page} page
 * @param {string} tenantSlug
 * @param {string} email
 * @param {string} password
 */
export async function loginAsTenantAdmin(page, tenantSlug, email, password) {
  await page.goto('/login')
  await page.getByPlaceholder('Leave empty for platform admin').fill(tenantSlug)
  await page.getByPlaceholder('admin@example.com').fill(email)
  await page.getByPlaceholder('••••••••').fill(password)
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
 * Obtain an access token for an arbitrary user via the API.
 * @param {string} baseURL
 * @param {string} email
 * @param {string} password
 * @param {string} [tenant]
 * @returns {Promise<string>}
 */
export async function getUserToken(baseURL, email, password, tenant = '') {
  const res = await fetch(`${baseURL}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, tenant }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`getUserToken failed (${res.status}): ${body}`)
  }
  const data = await res.json()
  return data.access_token
}

/**
 * Change a user's password via the authenticated API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} currentPassword
 * @param {string} newPassword
 * @param {string} confirmPassword
 * @returns {Promise<Response>}
 */
export async function changePasswordApi(baseURL, token, currentPassword, newPassword, confirmPassword = newPassword) {
  return fetch(`${baseURL}/auth/password`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({
      current_password: currentPassword,
      new_password: newPassword,
      confirm_password: confirmPassword,
    }),
  })
}

/**
 * Promote a user to admin via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} userId
 * @returns {Promise<object>}
 */
export async function promoteToAdmin(baseURL, token, userId) {
  const res = await fetch(`${baseURL}/auth/admin/users/${userId}`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ role: 'admin' }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`promoteToAdmin failed (${res.status}): ${body}`)
  }
  return res.json()
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

/**
 * Create a contact via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} email
 * @param {string} name
 * @returns {Promise<object>}
 */
export async function createContact(baseURL, token, email, name) {
  const res = await fetch(`${baseURL}/auth/admin/contacts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ email, name }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`createContact failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Delete a contact via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} id
 */
export async function deleteContact(baseURL, token, id) {
  const res = await fetch(`${baseURL}/auth/admin/contacts/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteContact failed (${res.status}): ${body}`)
  }
}

/**
 * Create a segment via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} name
 * @param {string} description
 * @returns {Promise<object>}
 */
export async function createSegment(baseURL, token, name, description) {
  const res = await fetch(`${baseURL}/auth/admin/segments`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name, description }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`createSegment failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Delete a segment via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} id
 */
export async function deleteSegment(baseURL, token, id) {
  const res = await fetch(`${baseURL}/auth/admin/segments/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteSegment failed (${res.status}): ${body}`)
  }
}

/**
 * Create a campaign via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} subject
 * @param {string} htmlBody
 * @param {string[]} segmentIds
 * @returns {Promise<object>}
 */
export async function createCampaign(baseURL, token, subject, htmlBody, segmentIds = []) {
  const res = await fetch(`${baseURL}/auth/admin/campaigns`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({
      subject,
      html_body: htmlBody,
      from_name: 'Test Sender',
      from_email: 'test@example.com',
      segment_ids: segmentIds,
    }),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`createCampaign failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Delete a campaign via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} id
 */
export async function deleteCampaign(baseURL, token, id) {
  const res = await fetch(`${baseURL}/auth/admin/campaigns/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteCampaign failed (${res.status}): ${body}`)
  }
}

/**
 * Create a user via the contact invite flow.
 * Suitable for tenants where registration is disabled.
 * Returns the created user object.
 * @param {string} baseURL
 * @param {string} token  Admin token for the target tenant
 * @param {string} email
 * @param {string} password
 * @param {string} name
 * @param {string} [tenantSlug]  Pass to target a specific tenant via path prefix
 * @returns {Promise<object>}
 */
export async function createUserViaInvite(baseURL, token, email, password, name, tenantSlug = '') {
  const prefix = tenantSlug ? `/t/${tenantSlug}` : ''

  // 1. Create contact to get an invite_token
  const contactRes = await fetch(`${baseURL}/auth${prefix}/admin/contacts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ email, name }),
  })
  if (!contactRes.ok) {
    const body = await contactRes.text()
    throw new Error(`createUserViaInvite: contact creation failed (${contactRes.status}): ${body}`)
  }
  const contact = await contactRes.json()
  const inviteToken = contact.invite_token
  if (!inviteToken) throw new Error('createUserViaInvite: no invite_token in response')

  // 2. Activate the invite to create the user account
  const activateRes = await fetch(`${baseURL}/auth${prefix}/activate/${inviteToken}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  if (!activateRes.ok) {
    const body = await activateRes.text()
    throw new Error(`createUserViaInvite: activation failed (${activateRes.status}): ${body}`)
  }
  const result = await activateRes.json()

  // 3. Return user-like object so callers can use .id
  return { id: result.user_id, email, name }
}

/**
 * Import a tenant via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {object} config
 * @returns {Promise<object>}
 */
export async function importTenant(baseURL, token, config) {
  const res = await fetch(`${baseURL}/auth/admin/tenants/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(config),
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`importTenant failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Export a tenant via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} id
 * @returns {Promise<object>}
 */
export async function exportTenant(baseURL, token, id) {
  const res = await fetch(`${baseURL}/auth/admin/tenants/${id}/export`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`exportTenant failed (${res.status}): ${body}`)
  }
  return res.json()
}

/**
 * Delete a tenant via the admin API.
 * @param {string} baseURL
 * @param {string} token
 * @param {string} id
 */
export async function deleteTenant(baseURL, token, id) {
  const res = await fetch(`${baseURL}/auth/admin/tenants/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 404) {
    const body = await res.text()
    throw new Error(`deleteTenant failed (${res.status}): ${body}`)
  }
}
