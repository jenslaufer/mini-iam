// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, createClient, deleteClientApi } from './helpers.js'

test.describe('Client editing', () => {
  let adminToken
  let testClient

  test.beforeEach(async ({ page, baseURL }) => {
    adminToken = await getAdminToken(baseURL)
    testClient = await createClient(baseURL, adminToken, 'Test Client', ['http://localhost/cb'])
    await loginAsAdmin(page)
    await page.goto('/clients')
    await page.waitForSelector('table tbody tr')
  })

  test.afterEach(async ({ baseURL }) => {
    if (testClient) {
      await deleteClientApi(baseURL, adminToken, testClient.client_id).catch(() => {})
    }
  })

  test('client has edit button', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testClient.name}`) })
    await expect(row.getByRole('button', { name: 'Edit' })).toBeVisible()
    await expect(row.getByRole('button', { name: 'Delete' })).toBeVisible()
  })

  test('edit client opens pre-filled modal', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testClient.name}`) })
    await row.getByRole('button', { name: 'Edit' }).click()

    // Modal should be visible with pre-filled values
    const modal = page.locator('[role="dialog"]')
    await expect(modal).toBeVisible()

    const nameInput = modal.locator('input[type="text"]').first()
    await expect(nameInput).toHaveValue('Test Client')

    const urisTextarea = modal.locator('textarea')
    await expect(urisTextarea).toContainText('http://localhost/cb')
  })

  test('save client edit updates list', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testClient.name}`) })
    await row.getByRole('button', { name: 'Edit' }).click()

    const modal = page.locator('[role="dialog"]')
    const nameInput = modal.locator('input[type="text"]').first()
    await nameInput.fill('Updated Client')

    await modal.getByRole('button', { name: 'Save' }).click()

    // Modal closes
    await expect(modal).not.toBeVisible({ timeout: 5000 })

    // Toast shows success
    await expect(page.locator('text=Client updated')).toBeVisible({ timeout: 5000 })

    // Client name in list updated
    await expect(page.locator('td', { hasText: 'Updated Client' })).toBeVisible()

    // Restore original name
    const updatedRow = page.locator('tr', { has: page.locator('text=Updated Client') })
    await updatedRow.getByRole('button', { name: 'Edit' }).click()
    const restoreInput = page.locator('[role="dialog"] input[type="text"]').first()
    await restoreInput.fill('Test Client')
    await page.locator('[role="dialog"]').getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('[role="dialog"]')).not.toBeVisible({ timeout: 5000 })
  })
})
