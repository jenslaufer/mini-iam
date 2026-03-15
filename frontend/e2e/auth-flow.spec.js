// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, registerUser, createClient, getAdminToken, deleteUserApi, deleteClientApi } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Full auth integration flow', () => {
  test('register user and client, verify in UI, change role, then delete both', async ({ page }) => {
    const timestamp = Date.now()
    const userEmail = `flow-user-${timestamp}@example.com`
    const clientName = `Flow Client ${timestamp}`

    let userId
    let clientId
    let adminToken

    // --- Setup via API ---
    adminToken = await getAdminToken(BASE_URL)

    // Register a regular user
    const user = await registerUser(BASE_URL, userEmail, 'testpass123', 'Flow User')
    userId = user.id

    // Create an OAuth2 client
    const client = await createClient(BASE_URL, adminToken, clientName, [
      'http://localhost:9999/callback',
    ])
    clientId = client.client_id

    // --- Login as admin in the UI ---
    await loginAsAdmin(page)

    // --- Verify new user appears in users list ---
    await page.goto('/users')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })
    await expect(page.getByText(userEmail)).toBeVisible()

    // --- Verify new client appears in clients list ---
    await page.goto('/clients')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })
    await expect(page.getByText(clientName)).toBeVisible()

    // --- Change user role to admin via the UI ---
    await page.goto('/users')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const userRow = page.locator('tr', { hasText: userEmail })
    await userRow.getByRole('button', { name: /Role/i }).click()
    await page.getByRole('button', { name: 'admin' }).click()
    await expect(page.getByText('Role updated to admin')).toBeVisible()

    // --- Delete the user via the UI ---
    await userRow.getByRole('button', { name: 'Delete' }).click()
    await expect(page.getByRole('heading', { name: new RegExp(userEmail) })).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()
    await expect(page.getByText(userEmail)).not.toBeVisible()
    await expect(page.getByText('User deleted')).toBeVisible()
    userId = null // already deleted

    // --- Delete the client via the UI ---
    await page.goto('/clients')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const clientRow = page.locator('tr', { hasText: clientName })
    await clientRow.getByRole('button', { name: 'Delete' }).click()
    await expect(page.getByRole('heading', { name: new RegExp(clientName) })).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()
    await expect(page.getByText(clientName)).not.toBeVisible()
    await expect(page.getByText('Client deleted')).toBeVisible()
    clientId = null // already deleted

    // Teardown — clean up anything that wasn't deleted by the test
    test.info().annotations.push({ type: 'cleanup', description: 'Resources deleted during test' })
    if (userId || clientId) {
      adminToken = await getAdminToken(BASE_URL)
      if (userId) await deleteUserApi(BASE_URL, adminToken, userId)
      if (clientId) await deleteClientApi(BASE_URL, adminToken, clientId)
    }
  })
})
