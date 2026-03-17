// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, createSegment, deleteSegment } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Segments page', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/segments')
    await page.waitForFunction(() => !document.querySelector('tbody .animate-pulse'), { timeout: 15000 })
  })

  test('shows segments list or empty state', async ({ page }) => {
    await expect(page.locator('tbody tr').first()).toBeVisible()
  })

  test('content persists after loading', async ({ page }) => {
    await expect(page.getByRole('button', { name: '+ New Segment' })).toBeVisible()
    await page.waitForTimeout(1000)
    await expect(page.getByRole('button', { name: '+ New Segment' })).toBeVisible()
    await expect(page.locator('tbody tr').first()).toBeVisible()
  })

  test('can create segment via modal', async ({ page }) => {
    const name = `Segment ${Date.now()}`
    const description = 'Created by E2E test'

    await page.getByRole('button', { name: '+ New Segment' }).click()
    await expect(page.getByRole('heading', { name: 'New Segment' })).toBeVisible()

    await page.getByPlaceholder('Newsletter subscribers').fill(name)
    await page.getByPlaceholder('Optional description').fill(description)
    await page.getByRole('button', { name: 'Create Segment' }).click()

    await expect(page.getByText('Segment created')).toBeVisible()
    await expect(page.getByText(name)).toBeVisible()

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    const res = await fetch(`${BASE_URL}/auth/admin/segments`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const segments = await res.json()
    const created = segments.find((s) => s.name === name)
    if (created) await deleteSegment(BASE_URL, token, created.id)
  })

  test('created segment appears in list', async ({ page }) => {
    const name = `Listed Seg ${Date.now()}`
    const token = await getAdminToken(BASE_URL)
    const segment = await createSegment(BASE_URL, token, name, 'E2E test segment')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    await expect(page.getByText(name)).toBeVisible()

    // Cleanup
    await deleteSegment(BASE_URL, token, segment.id)
  })

  test('can delete segment with confirmation', async ({ page }) => {
    const name = `Del Seg ${Date.now()}`
    const token = await getAdminToken(BASE_URL)
    await createSegment(BASE_URL, token, name, 'To be deleted')

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: name })
    await row.getByRole('button', { name: 'Delete' }).click()

    await expect(page.getByRole('heading', { name: new RegExp(name) })).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()

    await expect(page.locator('tbody').getByText(name)).not.toBeVisible()
    await expect(page.getByText('Segment deleted')).toBeVisible()
  })

  test('duplicate name shows error', async ({ page }) => {
    const name = `Dup Seg ${Date.now()}`
    const token = await getAdminToken(BASE_URL)
    const segment = await createSegment(BASE_URL, token, name, 'Original')

    await page.getByRole('button', { name: '+ New Segment' }).click()
    await page.getByPlaceholder('Newsletter subscribers').fill(name)
    await page.getByRole('button', { name: 'Create Segment' }).click()

    // Toast shows an error (duplicate rejected by API)
    await expect(page.locator('.bg-red-50, [class*="error"]').or(page.getByText(/already|duplicate|exist/i))).toBeVisible()

    await page.getByRole('button', { name: 'Cancel' }).click()

    // Cleanup
    await deleteSegment(BASE_URL, token, segment.id)
  })
})
