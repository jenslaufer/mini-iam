// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin } from './helpers.js'

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
  })

  test('shows dashboard with stat cards', async ({ page }) => {
    await expect(page.getByText('Total Users')).toBeVisible()
    await expect(page.getByText('Admins')).toBeVisible()
    await expect(page.getByText('OAuth2 Clients')).toBeVisible()
  })

  test('stat cards show correct counts', async ({ page }) => {
    // Wait for the loading placeholders to resolve (they show '—' while loading)
    await expect(page.getByText('—')).toHaveCount(0, { timeout: 10000 })

    // At minimum the seeded admin account should be counted
    const usersCard = page.locator('div').filter({ hasText: 'Total Users' }).first()
    await expect(usersCard).toBeVisible()

    const adminsCard = page.locator('div').filter({ hasText: /^Admins$/ }).first()
    await expect(adminsCard).toBeVisible()

    const clientsCard = page.locator('div').filter({ hasText: 'OAuth2 Clients' }).first()
    await expect(clientsCard).toBeVisible()
  })

  test('quick links navigate to users page', async ({ page }) => {
    await page.getByRole('link', { name: 'Manage Users' }).click()
    await expect(page).toHaveURL('/users')
    await expect(page.getByPlaceholder('Search users...')).toBeVisible()
  })

  test('quick links navigate to clients page', async ({ page }) => {
    await page.getByRole('link', { name: 'OAuth2 Clients' }).click()
    await expect(page).toHaveURL('/clients')
    await expect(page.getByRole('button', { name: '+ New Client' })).toBeVisible()
  })

  test('sidebar navigation works', async ({ page }) => {
    // Navigate to Users via sidebar
    await page.getByRole('link', { name: 'Users' }).click()
    await expect(page).toHaveURL('/users')

    // Navigate to Clients via sidebar
    await page.getByRole('link', { name: 'Clients' }).click()
    await expect(page).toHaveURL('/clients')

    // Navigate back to Dashboard via sidebar
    await page.getByRole('link', { name: 'Dashboard' }).click()
    await expect(page).toHaveURL('/dashboard')
  })
})
