// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, deleteClientApi } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Clients page', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/clients')
    // Wait for skeleton loaders to finish
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })
  })

  test('shows empty client list or lists existing clients', async ({ page }) => {
    // Either the empty state message or at least one row must be present
    const emptyState = page.getByText('No clients registered yet')
    const firstRow = page.locator('tbody tr').first()
    await expect(emptyState.or(firstRow)).toBeVisible()
  })

  test('can create new client via modal', async ({ page }) => {
    const clientName = `Test Client ${Date.now()}`
    let createdClientId

    await page.getByRole('button', { name: '+ New Client' }).click()

    // Modal is visible
    await expect(page.getByRole('heading', { name: 'New OAuth2 Client' })).toBeVisible()

    await page.getByPlaceholder('My App').fill(clientName)
    await page.getByPlaceholder(/https:\/\/app\.example\.com\/callback/).fill('http://localhost:9999/callback')

    await page.getByRole('button', { name: 'Create Client' }).click()

    // Secret banner appears
    const banner = page.locator('.bg-amber-50')
    await expect(banner).toBeVisible()
    await expect(banner).toContainText('Client secret — copy now, never shown again')

    // New client row appears in the table
    await expect(page.getByText(clientName)).toBeVisible()

    // Capture client_id for cleanup (first code element in the new row)
    const row = page.locator('tr', { hasText: clientName })
    const clientIdText = await row.locator('code').innerText()
    // clientIdText may be truncated — we'll delete via admin API using a search
    // Fetch full list to find the id
    const token = await getAdminToken(BASE_URL)
    const res = await fetch(`${BASE_URL}/auth/admin/clients`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const clients = await res.json()
    const created = clients.find((c) => c.name === clientName)
    if (created) {
      await deleteClientApi(BASE_URL, token, created.client_id)
    }
  })

  test('shows client secret after creation (only once)', async ({ page }) => {
    const clientName = `Secret Test ${Date.now()}`

    await page.getByRole('button', { name: '+ New Client' }).click()
    await page.getByPlaceholder('My App').fill(clientName)
    await page.getByPlaceholder(/https:\/\/app\.example\.com\/callback/).fill('http://localhost:9999/callback')
    await page.getByRole('button', { name: 'Create Client' }).click()

    // Banner with secret is shown
    const banner = page.locator('.bg-amber-50')
    await expect(banner).toBeVisible()
    await expect(banner.locator('code')).toBeVisible()

    // Dismiss the banner
    await banner.getByTitle('Dismiss').click()
    await expect(banner).not.toBeVisible()

    // Reload — secret is gone (not stored server-side)
    await page.reload()
    await expect(page.locator('.bg-amber-50')).not.toBeVisible()

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    const res = await fetch(`${BASE_URL}/auth/admin/clients`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const clients = await res.json()
    const created = clients.find((c) => c.name === clientName)
    if (created) {
      await deleteClientApi(BASE_URL, token, created.client_id)
    }
  })

  test('can copy client secret', async ({ page, context }) => {
    // Grant clipboard permissions
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])

    const clientName = `Copy Secret ${Date.now()}`

    await page.getByRole('button', { name: '+ New Client' }).click()
    await page.getByPlaceholder('My App').fill(clientName)
    await page.getByPlaceholder(/https:\/\/app\.example\.com\/callback/).fill('http://localhost:9999/callback')
    await page.getByRole('button', { name: 'Create Client' }).click()

    const banner = page.locator('.bg-amber-50')
    await expect(banner).toBeVisible()

    // Read the displayed secret text before copying
    const displayedSecret = await banner.locator('code').innerText()

    // Click the copy button
    await banner.getByTitle('Copy to clipboard').click()
    await expect(page.getByText('Secret copied to clipboard')).toBeVisible()

    // Verify clipboard content matches
    const clipboardText = await page.evaluate(() => navigator.clipboard.readText())
    expect(clipboardText).toBe(displayedSecret)

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    const res = await fetch(`${BASE_URL}/auth/admin/clients`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const clients = await res.json()
    const created = clients.find((c) => c.name === clientName)
    if (created) {
      await deleteClientApi(BASE_URL, token, created.client_id)
    }
  })

  test('can delete client with confirmation', async ({ page }) => {
    // Create a client first via the UI so we can delete it
    const clientName = `Del Client ${Date.now()}`

    await page.getByRole('button', { name: '+ New Client' }).click()
    await page.getByPlaceholder('My App').fill(clientName)
    await page.getByPlaceholder(/https:\/\/app\.example\.com\/callback/).fill('http://localhost:9999/callback')
    await page.getByRole('button', { name: 'Create Client' }).click()

    // Dismiss the secret banner so it doesn't interfere
    const banner = page.locator('.bg-amber-50')
    await expect(banner).toBeVisible()
    await banner.getByTitle('Dismiss').click()

    // Delete the client
    const row = page.locator('tr', { hasText: clientName })
    await row.getByRole('button', { name: 'Delete' }).click()

    // Confirm dialog
    await expect(page.getByRole('heading', { name: new RegExp(clientName) })).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()

    // Client is removed from the table
    await expect(page.locator('tbody').getByText(clientName)).not.toBeVisible()
    await expect(page.getByText('Client deleted')).toBeVisible()
  })

  test('cancel on modal closes without creating', async ({ page }) => {
    const clientName = `Never Created ${Date.now()}`

    await page.getByRole('button', { name: '+ New Client' }).click()
    await expect(page.getByRole('heading', { name: 'New OAuth2 Client' })).toBeVisible()

    await page.getByPlaceholder('My App').fill(clientName)

    await page.getByRole('button', { name: 'Cancel' }).click()

    // Modal is closed
    await expect(page.getByRole('heading', { name: 'New OAuth2 Client' })).not.toBeVisible()

    // Client was not created
    await expect(page.getByText(clientName)).not.toBeVisible()
  })
})
