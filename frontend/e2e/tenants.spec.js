// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, importTenant, exportTenant, deleteTenant } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Tenants page', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/tenants')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })
  })

  test('shows tenants list with at least the default tenant', async ({ page }) => {
    await expect(page.locator('tbody').getByText('default').first()).toBeVisible()
  })

  test('content persists after loading', async ({ page }) => {
    await expect(page.locator('tbody').getByText('default').first()).toBeVisible()
    await page.waitForTimeout(1000)
    await expect(page.locator('tbody').getByText('default').first()).toBeVisible()
    await expect(page.getByRole('button', { name: 'Import Tenant' })).toBeVisible()
  })

  test('imported tenant appears in list', async ({ page }) => {
    const slug = `e2e-tenant-${Date.now()}`
    const token = await getAdminToken(BASE_URL)
    const config = { slug, name: `E2E Tenant ${Date.now()}`, smtp: {}, clients: [] }
    const result = await importTenant(BASE_URL, token, config)

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    await expect(page.getByText(slug)).toBeVisible()

    // Cleanup
    await deleteTenant(BASE_URL, token, result.id)
  })

  test('can export tenant and response has required fields', async ({ page }) => {
    const token = await getAdminToken(BASE_URL)

    // Find the default tenant id
    const res = await fetch(`${BASE_URL}/auth/admin/tenants`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const tenants = await res.json()
    const defaultTenant = tenants.find((t) => t.slug === 'default')
    expect(defaultTenant).toBeDefined()

    const exported = await exportTenant(BASE_URL, token, defaultTenant.id)

    expect(exported).toHaveProperty('slug')
    expect(exported).toHaveProperty('name')
    expect(exported.smtp_password).toBeUndefined()
  })

  test('can delete tenant with confirmation', async ({ page }) => {
    const ts = Date.now()
    const slug = `del-tenant-${ts}`
    const name = `Del Tenant ${ts}`
    const token = await getAdminToken(BASE_URL)
    const config = { slug, name, smtp: {}, clients: [] }
    await importTenant(BASE_URL, token, config)

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: slug })
    await row.getByRole('button', { name: 'Delete' }).click()

    await expect(page.getByRole('heading', { name: new RegExp(name) })).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()

    await expect(page.locator('tbody').getByText(slug)).not.toBeVisible()
    await expect(page.getByText('Tenant deleted')).toBeVisible()
  })
})
