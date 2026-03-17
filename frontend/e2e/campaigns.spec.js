// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, createSegment, deleteSegment, createCampaign, deleteCampaign } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Campaigns page', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/campaigns')
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })
  })

  test('shows campaigns list or empty state', async ({ page }) => {
    await expect(page.locator('tbody tr').first()).toBeVisible()
  })

  test('content persists after loading', async ({ page }) => {
    await expect(page.getByRole('button', { name: '+ New Campaign' })).toBeVisible()
    await page.waitForTimeout(1000)
    await expect(page.getByRole('button', { name: '+ New Campaign' })).toBeVisible()
    await expect(page.locator('tbody tr').first()).toBeVisible()
  })

  test('can create campaign via modal', async ({ page }) => {
    const ts = Date.now()
    const subject = `Test Campaign ${ts}`
    const token = await getAdminToken(BASE_URL)

    // Need at least one segment for the form's segment selection
    const segment = await createSegment(BASE_URL, token, `Cam Seg ${ts}`, '')

    // Reload so the newly created segment appears in the modal's segment list
    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    await page.getByRole('button', { name: '+ New Campaign' }).click()
    await expect(page.getByRole('heading', { name: 'New Campaign' })).toBeVisible()

    await page.getByPlaceholder('Welcome to our newsletter').fill(subject)
    await page.getByPlaceholder('Acme Corp').fill('E2E Sender')
    await page.getByPlaceholder('hello@acme.com').fill('e2e@example.com')

    // Select the segment checkbox (label wraps checkbox + span, so locate via the label text)
    await page.locator('label', { hasText: segment.name }).locator('input[type="checkbox"]').check()

    await page.getByPlaceholder('<h1>Hello!</h1><p>Your email content here...</p>').fill('<p>E2E test body</p>')

    await page.getByRole('button', { name: 'Create Campaign' }).click()

    await expect(page.getByText('Campaign created')).toBeVisible()
    await expect(page.getByText(subject)).toBeVisible()

    // Cleanup
    const res = await fetch(`${BASE_URL}/auth/admin/campaigns`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const campaigns = await res.json()
    const created = campaigns.find((c) => c.subject === subject)
    if (created) await deleteCampaign(BASE_URL, token, created.id)
    await deleteSegment(BASE_URL, token, segment.id)
  })

  test('created campaign appears in list with draft status', async ({ page }) => {
    const ts = Date.now()
    const subject = `Draft Campaign ${ts}`
    const token = await getAdminToken(BASE_URL)
    const segment = await createSegment(BASE_URL, token, `Draft Seg ${ts}`, '')
    const campaign = await createCampaign(BASE_URL, token, subject, '<p>Hello</p>', [segment.id])

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: subject })
    await expect(row).toBeVisible()
    await expect(row.locator('span', { hasText: 'Draft' }).first()).toBeVisible()

    // Cleanup
    await deleteCampaign(BASE_URL, token, campaign.id)
    await deleteSegment(BASE_URL, token, segment.id)
  })

  test('can delete draft campaign', async ({ page }) => {
    const ts = Date.now()
    const subject = `Del Campaign ${ts}`
    const token = await getAdminToken(BASE_URL)
    const segment = await createSegment(BASE_URL, token, `Del Cam Seg ${ts}`, '')
    await createCampaign(BASE_URL, token, subject, '<p>To delete</p>', [segment.id])

    await page.reload()
    await expect(page.locator('tbody tr td .animate-pulse').first()).toHaveCount(0, { timeout: 10000 })

    const row = page.locator('tr', { hasText: subject })
    await row.getByRole('button', { name: 'Delete' }).click()

    await expect(page.getByRole('heading', { name: new RegExp(subject) })).toBeVisible()
    await page.getByRole('button', { name: 'Delete' }).last().click()

    await expect(page.locator('tbody').getByText(subject)).not.toBeVisible()
    await expect(page.getByText('Campaign deleted')).toBeVisible()

    // Cleanup segment (campaign already deleted)
    await deleteSegment(BASE_URL, token, segment.id)
  })
})
