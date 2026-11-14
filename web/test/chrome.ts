// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Helpers for the Etesian header the suites drive like a person: the classic bar's apps menu, a page's actions (in the header, or in
// its ⋮ when they do not fit), and the title's menu of views. The same in a desktop window.
import type { Page } from 'playwright'

/** opens a section (or "Desktop") from the classic bar's apps menu */
export async function openApp(page: Page, title: string) {
  await page.click('.x-cl__apps')
  await page.locator('.x-cl__grid .x-cl__icon', { hasText: title }).last().click()
}

/** presses a page action by its name: in the header when it is shown there, otherwise from the ⋮ menu */
export async function tool(page: Page, label: string) {
  const b = page.locator(`[role=toolbar] [aria-label="${label}"]:visible, .x-cl__bottom [aria-label="${label}"]:visible, .x-win__bottom [aria-label="${label}"]:visible`).first()
  if (await b.count()) return b.click()
  await page.locator('[aria-label=More]:visible').last().click()
  await page.getByRole('menuitem', { name: label }).click()
}

/** picks one of the title's views (a folder in Mail, a mode of Android) */
export async function view(page: Page, name: string) {
  await page.locator('.x-cl__title.is-menu:visible, .x-win__title--menu:visible').first().click()
  await page.getByRole('option', { name }).click()
}

/** the title the header shows (the current view, when the title is a menu) */
export const shownTitle = (page: Page) => page.locator('.x-cl__title:visible, .x-win__title:visible').first().innerText()
