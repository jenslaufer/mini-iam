// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken } from './helpers.js'

const BASE_URL = 'http://localhost:3000/auth'

test.describe('Tenant settings', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
  })

  test('tenant list shows clickable tenant names', async ({ page }) => {
    await page.goto('/tenants')
    await page.waitForFunction(
      () => !document.querySelector('tbody .animate-pulse'),
      { timeout: 15000 },
    )

    // Tenant name should be a link
    const firstLink = page.locator('tbody tr:first-child a')
    await expect(firstLink).toBeVisible()
    const href = await firstLink.getAttribute('href')
    expect(href).toMatch(/\/tenants\//)

    // Click it
    await firstLink.click()
    await expect(page).toHaveURL(/\/tenants\/[a-z0-9-]+/)
  })

  test('tenant detail page shows all fields', async ({ page }) => {
    // Navigate to tenant list, click first tenant
    await page.goto('/tenants')
    await page.waitForFunction(
      () => !document.querySelector('tbody .animate-pulse'),
      { timeout: 15000 },
    )
    await page.locator('tbody tr:first-child a').click()
    await expect(page).toHaveURL(/\/tenants\//)

    // General fields
    await expect(page.getByLabel('Name')).toBeVisible()
    await expect(page.getByText('Slug')).toBeVisible()
    await expect(page.getByText('Registration')).toBeVisible()
    await expect(page.getByText('Created')).toBeVisible()

    // SMTP fields
    await expect(page.getByLabel('Host')).toBeVisible()
    await expect(page.getByLabel('Port')).toBeVisible()
    await expect(page.getByLabel('User')).toBeVisible()
    await expect(page.getByLabel('Password')).toBeVisible()
    await expect(page.getByLabel('From')).toBeVisible()
    await expect(page.getByLabel('From Name')).toBeVisible()
    await expect(page.getByLabel('Rate (ms)')).toBeVisible()

    // Save button and back link
    await expect(page.getByRole('button', { name: 'Save' })).toBeVisible()
    await expect(page.getByRole('link', { name: /back/i })).toBeVisible()
  })

  test('edit tenant name', async ({ page }) => {
    await page.goto('/tenants')
    await page.waitForFunction(
      () => !document.querySelector('tbody .animate-pulse'),
      { timeout: 15000 },
    )
    await page.locator('tbody tr:first-child a').click()
    await expect(page).toHaveURL(/\/tenants\//)

    const nameInput = page.getByLabel('Name')
    const originalName = await nameInput.inputValue()

    // Change name
    await nameInput.fill('Updated Name E2E')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('saved')).toBeVisible({ timeout: 5000 })

    // Reload and verify
    await page.reload()
    await expect(nameInput).toHaveValue('Updated Name E2E')

    // Restore original name
    await nameInput.fill(originalName)
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('saved')).toBeVisible({ timeout: 5000 })
  })

  test('toggle registration enabled', async ({ page }) => {
    await page.goto('/tenants')
    await page.waitForFunction(
      () => !document.querySelector('tbody .animate-pulse'),
      { timeout: 15000 },
    )
    await page.locator('tbody tr:first-child a').click()
    await expect(page).toHaveURL(/\/tenants\//)

    const toggle = page.getByRole('checkbox', { name: /registration/i })
    const wasBefore = await toggle.isChecked()

    // Toggle
    await toggle.click()
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('saved')).toBeVisible({ timeout: 5000 })

    // Reload and verify
    await page.reload()
    if (wasBefore) {
      await expect(toggle).not.toBeChecked()
    } else {
      await expect(toggle).toBeChecked()
    }

    // Toggle back
    await toggle.click()
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('saved')).toBeVisible({ timeout: 5000 })
  })

  test('edit SMTP configuration', async ({ page }) => {
    await page.goto('/tenants')
    await page.waitForFunction(
      () => !document.querySelector('tbody .animate-pulse'),
      { timeout: 15000 },
    )
    await page.locator('tbody tr:first-child a').click()
    await expect(page).toHaveURL(/\/tenants\//)

    // Fill SMTP fields
    await page.getByLabel('Host').fill('smtp.test.com')
    await page.getByLabel('Port').fill('465')
    await page.getByLabel('From', { exact: true }).fill('test@example.com')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('saved')).toBeVisible({ timeout: 5000 })

    // Reload and verify
    await page.reload()
    await expect(page.getByLabel('Host')).toHaveValue('smtp.test.com')
    await expect(page.getByLabel('Port')).toHaveValue('465')
    await expect(page.getByLabel('From', { exact: true })).toHaveValue('test@example.com')
  })

  test('back link returns to tenant list', async ({ page }) => {
    await page.goto('/tenants')
    await page.waitForFunction(
      () => !document.querySelector('tbody .animate-pulse'),
      { timeout: 15000 },
    )
    await page.locator('tbody tr:first-child a').click()
    await expect(page).toHaveURL(/\/tenants\//)

    await page.getByRole('link', { name: /back/i }).click()
    await expect(page).toHaveURL('/tenants')
  })
})
