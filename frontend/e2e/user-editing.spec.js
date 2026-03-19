// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, registerUser } from './helpers.js'

test.describe('User name editing', () => {
  let adminToken
  let testUser

  test.beforeEach(async ({ page, baseURL }) => {
    adminToken = await getAdminToken(baseURL)
    // Create a test user to edit
    const email = `edit-user-${Date.now()}@example.com`
    testUser = await registerUser(baseURL, email, 'testpass123', 'Original Name')
    await loginAsAdmin(page)
    await page.goto('/users')
    await page.waitForSelector('table tbody tr')
  })

  test('user name is editable inline', async ({ page, baseURL }) => {
    // Find the row with the test user and click the name
    const row = page.locator('tr', { has: page.locator(`text=${testUser.email}`) })
    const nameCell = row.locator('td:nth-child(2)')
    await nameCell.click()

    // Input field should appear with current name
    const input = nameCell.locator('input')
    await expect(input).toBeVisible()
    await expect(input).toHaveValue('Original Name')

    // Clear and type new name
    await input.fill('Edited Name')
    await input.press('Enter')

    // Toast shows success
    await expect(page.locator('text=Name updated')).toBeVisible({ timeout: 5000 })

    // Name in the list updates
    await expect(nameCell).toContainText('Edited Name')

    // Restore original name
    await nameCell.click()
    const restoreInput = nameCell.locator('input')
    await restoreInput.fill('Original Name')
    await restoreInput.press('Enter')
    await expect(page.locator('text=Name updated')).toBeVisible({ timeout: 5000 })
  })

  test('inline edit cancel with Escape', async ({ page }) => {
    const row = page.locator('tr', { has: page.locator(`text=${testUser.email}`) })
    const nameCell = row.locator('td:nth-child(2)')
    await nameCell.click()

    const input = nameCell.locator('input')
    await expect(input).toBeVisible()
    await input.fill('Should Not Save')
    await input.press('Escape')

    // Name reverts to original value
    await expect(nameCell).toContainText('Original Name')
    // No input visible
    await expect(input).not.toBeVisible()
  })
})
