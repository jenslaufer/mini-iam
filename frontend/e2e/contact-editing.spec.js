// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, createContact, deleteContact } from './helpers.js'

test.describe('Contact editing', () => {
  let adminToken
  let testContact

  test.beforeEach(async ({ page, baseURL }) => {
    adminToken = await getAdminToken(baseURL)
    const email = `edit-contact-${Date.now()}@example.com`
    testContact = await createContact(baseURL, adminToken, email, 'Test Contact')
    await loginAsAdmin(page)
    await page.goto('/contacts')
    await page.waitForSelector('table tbody tr')
  })

  test.afterEach(async ({ baseURL }) => {
    if (testContact) {
      await deleteContact(baseURL, adminToken, testContact.id).catch(() => {})
    }
  })

  test('contact has edit button', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testContact.email}`) })
    await expect(row.getByRole('button', { name: 'Edit' })).toBeVisible()
    await expect(row.getByRole('button', { name: 'Delete' })).toBeVisible()
  })

  test('edit contact opens pre-filled modal', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testContact.email}`) })
    await row.getByRole('button', { name: 'Edit' }).click()

    const modal = page.locator('[role="dialog"]')
    await expect(modal).toBeVisible()

    // Check pre-filled values
    const nameInput = modal.locator('input').nth(1) // second input (after email)
    const emailInput = modal.locator('input[type="email"]')
    await expect(emailInput).toHaveValue(testContact.email)
    await expect(nameInput).toHaveValue('Test Contact')
  })

  test('save contact edit updates list', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testContact.email}`) })
    await row.getByRole('button', { name: 'Edit' }).click()

    const modal = page.locator('[role="dialog"]')
    // Find the name input and update it
    const inputs = modal.locator('input')
    // Name input - find by label
    const nameInput = modal.locator('input').filter({ has: page.locator('..') }).nth(1)
    await nameInput.fill('Updated Contact')

    await modal.getByRole('button', { name: 'Save' }).click()

    // Modal closes
    await expect(modal).not.toBeVisible({ timeout: 5000 })

    // Toast shows success
    await expect(page.locator('text=Contact updated')).toBeVisible({ timeout: 5000 })

    // Contact name in list updated
    await expect(page.locator('td', { hasText: 'Updated Contact' })).toBeVisible()

    // Restore original name
    const updatedRow = page.locator('tr', { has: page.locator(`text=${testContact.email}`) })
    await updatedRow.getByRole('button', { name: 'Edit' }).click()
    const restoreInput = page.locator('[role="dialog"] input').nth(1)
    await restoreInput.fill('Test Contact')
    await page.locator('[role="dialog"]').getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('[role="dialog"]')).not.toBeVisible({ timeout: 5000 })
  })
})
