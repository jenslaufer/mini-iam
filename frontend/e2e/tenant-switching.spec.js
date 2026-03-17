// @ts-check
import { test, expect } from '@playwright/test'
import {
  loginAsAdmin,
  getAdminToken,
  importTenant,
  deleteTenant,
  createContact,
  deleteContact,
} from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Tenant selector', () => {
  test('platform admin sees tenant selector in sidebar', async ({ page }) => {
    await loginAsAdmin(page)
    const sidebar = page.locator('aside')
    await expect(sidebar.locator('select')).toBeVisible()
  })

  test('tenant selector lists available tenants', async ({ page }) => {
    const ts = Date.now()
    const slug = `switch-tenant-${ts}`
    const name = `Switch Tenant ${ts}`
    const token = await getAdminToken(BASE_URL)
    const result = await importTenant(BASE_URL, token, { slug, name, smtp: {}, clients: [] })

    await loginAsAdmin(page)
    const sidebar = page.locator('aside')
    const select = sidebar.locator('select')
    await expect(select).toBeVisible()
    await expect(select.locator(`option[value="${slug}"]`)).toHaveCount(1)

    // Cleanup
    await deleteTenant(BASE_URL, token, result.tenant_id)
  })

  test('switching tenant updates contacts view to show that tenant data', async ({ page }) => {
    const ts = Date.now()
    const slug = `ctx-tenant-${ts}`
    const name = `Ctx Tenant ${ts}`
    const token = await getAdminToken(BASE_URL)

    // Create a second tenant
    const result = await importTenant(BASE_URL, token, {
      slug,
      name,
      smtp: {},
      clients: [],
      registration_enabled: false,
    })

    // Create contacts in both tenants via their scoped API URLs
    const defaultEmail = `default-contact-${ts}@example.com`
    const tenantEmail = `tenant-contact-${ts}@example.com`

    const defaultContact = await createContact(BASE_URL, token, defaultEmail, 'Default Contact')

    // Get a token for the new tenant by importing an admin user
    const tenantToken = await getAdminToken(BASE_URL) // platform admin can access any tenant
    const tenantContact = await fetch(`${BASE_URL}/auth/t/${slug}/admin/contacts`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${tenantToken}` },
      body: JSON.stringify({ email: tenantEmail, name: 'Tenant Contact' }),
    }).then((r) => r.json())

    await loginAsAdmin(page)

    // Explicitly select the default tenant so the test is deterministic
    const sidebar = page.locator('aside')
    await sidebar.locator('select').selectOption('default')

    await page.goto('/contacts')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    // Default tenant selected — default contact should be visible
    await expect(page.locator('tbody').getByText(defaultEmail)).toBeVisible()
    await expect(page.locator('tbody').getByText(tenantEmail)).not.toBeVisible()

    // Switch to the new tenant
    await sidebar.locator('select').selectOption(slug)

    await page.goto('/contacts')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    // Now only the new tenant's contact should be visible
    await expect(page.locator('tbody').getByText(tenantEmail)).toBeVisible()
    await expect(page.locator('tbody').getByText(defaultEmail)).not.toBeVisible()

    // Cleanup
    await deleteContact(BASE_URL, token, defaultContact.id)
    await fetch(`${BASE_URL}/auth/t/${slug}/admin/contacts/${tenantContact.id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${tenantToken}` },
    })
    await deleteTenant(BASE_URL, token, result.tenant_id)
  })

  test('platform section visible for platform admin', async ({ page }) => {
    await loginAsAdmin(page)
    const sidebar = page.locator('aside')
    await expect(sidebar.getByText('Platform')).toBeVisible()
    await expect(sidebar.getByRole('link', { name: 'Tenants' })).toBeVisible()
  })

  test('tenant selector persists selection across navigation', async ({ page }) => {
    const ts = Date.now()
    const slug = `persist-tenant-${ts}`
    const name = `Persist Tenant ${ts}`
    const token = await getAdminToken(BASE_URL)
    const result = await importTenant(BASE_URL, token, { slug, name, smtp: {}, clients: [] })

    await loginAsAdmin(page)
    const sidebar = page.locator('aside')
    await sidebar.locator('select').selectOption(slug)

    // Navigate away and back
    await page.goto('/users')
    await page.goto('/contacts')

    // Selection should persist
    await expect(sidebar.locator('select')).toHaveValue(slug)

    // Cleanup
    await deleteTenant(BASE_URL, token, result.tenant_id)
  })
})
