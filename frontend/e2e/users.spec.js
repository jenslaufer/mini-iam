// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, registerUser, getAdminToken, deleteUserApi } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Users page', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/users')
    // Wait for skeleton loaders to finish
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })
  })

  test('lists users including admin', async ({ page }) => {
    await expect(page.locator('tbody').getByText('admin@launch-kit.local')).toBeVisible()
  })

  test('content persists after loading', async ({ page }) => {
    await expect(page.locator('tbody').getByText('admin@launch-kit.local')).toBeVisible()
    await page.waitForTimeout(1000)
    await expect(page.locator('tbody').getByText('admin@launch-kit.local')).toBeVisible()
    await expect(page.getByPlaceholder('Search users...')).toBeVisible()
  })

  test('search filters users', async ({ page }) => {
    // Register a distinct user for this test
    const email = `searchable-${Date.now()}@example.com`
    const { id } = await registerUser(BASE_URL, email, 'testpass123', 'Searchable User')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    // Search matches the new user
    await page.getByPlaceholder('Search users...').fill('searchable')
    await expect(page.locator('tbody').getByText(email)).toBeVisible()
    await expect(page.locator('tbody').getByText('admin@launch-kit.local')).not.toBeVisible()

    // Clear search shows all users again
    await page.getByPlaceholder('Search users...').fill('')
    await expect(page.locator('tbody').getByText('admin@launch-kit.local')).toBeVisible()

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    await deleteUserApi(BASE_URL, token, id)
  })

  test('can change user role via dropdown', async ({ page }) => {
    const email = `role-test-${Date.now()}@example.com`
    const { id } = await registerUser(BASE_URL, email, 'testpass123', 'Role Test User')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    // Find the row for our test user and open the Role dropdown
    const row = page.locator('tr', { hasText: email })
    await row.getByRole('button', { name: /Role/i }).click()

    // Click 'admin' in the dropdown
    await page.getByRole('button', { name: 'admin' }).click()

    // Toast confirms the change
    await expect(page.getByText('Role updated to admin')).toBeVisible()

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    await deleteUserApi(BASE_URL, token, id)
  })

  test('can delete user with confirmation', async ({ page }) => {
    const email = `delete-me-${Date.now()}@example.com`
    await registerUser(BASE_URL, email, 'testpass123', 'Delete Me')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    // Click Delete on the specific user row
    const row = page.locator('tr', { hasText: email })
    await row.getByRole('button', { name: 'Delete' }).click()

    // Confirm dialog appears
    await expect(page.getByRole('heading', { name: new RegExp(email) })).toBeVisible()
    await expect(page.getByText('This action cannot be undone.')).toBeVisible()

    // Confirm deletion
    await page.getByRole('button', { name: 'Delete' }).last().click()

    // User is gone from the table
    await expect(page.locator('tbody').getByText(email)).toBeHidden({ timeout: 10000 })
    await expect(page.getByText('User deleted')).toBeVisible()
  })

  test('cancel delete dialog does not delete', async ({ page }) => {
    const email = `keep-me-${Date.now()}@example.com`
    const { id } = await registerUser(BASE_URL, email, 'testpass123', 'Keep Me')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: email })
    await row.getByRole('button', { name: 'Delete' }).click()

    // Cancel the dialog
    await page.getByRole('button', { name: 'Cancel' }).click()

    // User is still in the table
    await expect(page.locator('tbody').getByText(email)).toBeVisible()

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    await deleteUserApi(BASE_URL, token, id)
  })

  test('shows empty state when search has no results', async ({ page }) => {
    await page.getByPlaceholder('Search users...').fill('zzz-no-match-zzz')
    await expect(page.getByText('No users found')).toBeVisible()
  })
})
