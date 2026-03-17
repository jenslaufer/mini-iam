// @ts-check
import { test, expect } from '@playwright/test'
import { registerUser } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Login page', () => {
  test('shows login page at /login', async ({ page }) => {
    await page.goto('/login')
    await expect(page).toHaveURL('/login')
    await expect(page.getByRole('heading', { name: 'launch-kit' })).toBeVisible()
    await expect(page.getByPlaceholder('admin@example.com')).toBeVisible()
    await expect(page.getByPlaceholder('••••••••')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
  })

  test('redirects to /login when not authenticated', async ({ page }) => {
    // Clear any stored auth state
    await page.goto('/login')
    await page.evaluate(() => localStorage.clear())

    await page.goto('/dashboard')
    await expect(page).toHaveURL('/login')

    await page.goto('/users')
    await expect(page).toHaveURL('/login')

    await page.goto('/clients')
    await expect(page).toHaveURL('/login')
  })

  test('shows error on wrong credentials', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill('admin@launch-kit.local')
    await page.getByPlaceholder('••••••••').fill('wrongpassword')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.locator('.bg-red-50')).toBeVisible()
    await expect(page).toHaveURL('/login')
  })

  test('shows error when non-admin tries to login', async ({ page }) => {
    const email = `user-${Date.now()}@example.com`
    await registerUser(BASE_URL, email, 'testpass123', 'Regular User')

    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill(email)
    await page.getByPlaceholder('••••••••').fill('testpass123')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await expect(page.locator('.bg-red-50')).toBeVisible()
    await expect(page.locator('.bg-red-50')).toContainText('admin role required')
    await expect(page).toHaveURL('/login')
  })

  test('successful admin login redirects to dashboard', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill('admin@launch-kit.local')
    await page.getByPlaceholder('••••••••').fill('changeme')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/dashboard')
    await expect(page.getByText('Total Users')).toBeVisible()
  })

  test('logout returns to login page', async ({ page }) => {
    // Log in first
    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill('admin@launch-kit.local')
    await page.getByPlaceholder('••••••••').fill('changeme')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/dashboard', { timeout: 10000 })

    // Log out via sidebar button
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL('/login')

    // Confirm the session is gone: navigating to a protected page redirects back
    await page.goto('/dashboard')
    await expect(page).toHaveURL('/login')
  })
})
