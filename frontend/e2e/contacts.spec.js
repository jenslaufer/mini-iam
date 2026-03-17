// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, createContact, deleteContact } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Contacts page', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/contacts')
    // Wait for data to fully load (skeleton gone AND content stable)
    await page.waitForFunction(() => !document.querySelector('tbody .animate-pulse'), { timeout: 15000 })
  })

  test('shows contacts list or empty state', async ({ page }) => {
    const emptyState = page.getByRole('cell', { name: 'No contacts found' })
    const firstRow = page.locator('tbody tr').first()
    await expect(emptyState.or(firstRow)).toBeVisible()
  })

  test('content persists after loading', async ({ page }) => {
    await expect(page.getByRole('button', { name: '+ Add Contact' })).toBeVisible()
    await page.waitForTimeout(1000)
    await expect(page.getByRole('button', { name: '+ Add Contact' })).toBeVisible()
    const emptyState = page.getByRole('cell', { name: 'No contacts found' })
    const firstRow = page.locator('tbody tr').first()
    await expect(emptyState.or(firstRow)).toBeVisible()
  })

  test('can create contact via modal', async ({ page }) => {
    const email = `contact-${Date.now()}@example.com`
    const name = `Test Contact ${Date.now()}`

    await page.getByRole('button', { name: '+ Add Contact' }).click()
    await expect(page.getByRole('heading', { name: 'Add Contact' })).toBeVisible()

    await page.getByPlaceholder('user@example.com').fill(email)
    await page.getByPlaceholder('Jane Smith').fill(name)
    await page.getByRole('button', { name: 'Add Contact', exact: true }).click()

    await expect(page.getByText('Contact added')).toBeVisible()
    await expect(page.locator('tbody').getByText(email)).toBeVisible()

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    const res = await fetch(`${BASE_URL}/auth/admin/contacts`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const contacts = await res.json()
    const created = contacts.find((c) => c.email === email)
    if (created) await deleteContact(BASE_URL, token, created.id)
  })

  test('created contact appears in list', async ({ page }) => {
    const email = `listed-${Date.now()}@example.com`
    const token = await getAdminToken(BASE_URL)
    const contact = await createContact(BASE_URL, token, email, 'Listed Contact')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    await expect(page.getByText(email)).toBeVisible()

    // Cleanup
    await deleteContact(BASE_URL, token, contact.id)
  })

  test('can delete contact with confirmation', async ({ page }) => {
    const email = `del-contact-${Date.now()}@example.com`
    const token = await getAdminToken(BASE_URL)
    await createContact(BASE_URL, token, email, 'Delete Me')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: email })
    await row.getByRole('button', { name: 'Delete' }).click()

    await expect(page.getByRole('heading', { name: new RegExp(email) })).toBeVisible()
    await expect(page.getByText('This action cannot be undone.')).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()

    await expect(page.locator('tbody').getByText(email)).not.toBeVisible()
    await expect(page.getByText('Contact deleted')).toBeVisible()
  })

  test('cancel delete keeps contact', async ({ page }) => {
    const email = `keep-contact-${Date.now()}@example.com`
    const token = await getAdminToken(BASE_URL)
    const contact = await createContact(BASE_URL, token, email, 'Keep Me')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: email })
    await row.getByRole('button', { name: 'Delete' }).click()

    await page.getByRole('button', { name: 'Cancel' }).click()

    await expect(page.locator('tbody').getByText(email)).toBeVisible()

    // Cleanup
    await deleteContact(BASE_URL, token, contact.id)
  })

  test('duplicate email shows error', async ({ page }) => {
    const email = `dup-${Date.now()}@example.com`
    const token = await getAdminToken(BASE_URL)
    const contact = await createContact(BASE_URL, token, email, 'Original')

    await page.getByRole('button', { name: '+ Add Contact' }).click()
    await page.getByPlaceholder('user@example.com').fill(email)
    await page.getByRole('button', { name: 'Add Contact', exact: true }).click()

    // Toast shows an error (duplicate rejected by API)
    await expect(page.locator('.bg-red-50, [class*="error"]').or(page.getByText(/already|duplicate|exist/i))).toBeVisible()

    await page.getByRole('button', { name: 'Cancel' }).click()

    // Cleanup
    await deleteContact(BASE_URL, token, contact.id)
  })
})
