import type { Locator, Page } from '@playwright/test'

/**
 * The form control that follows a text label. The settings forms put a plain
 * label (or heading) next to the control without linking them, so a role or
 * label query cannot find them; the document order does.
 *
 * A label can also appear as a table heading ("Kind", "Key"). The form is
 * rendered after the table, so the last label with that text is the form's.
 */
export function field(page: Page, label: string): Locator {
  const text = label.replace(/'/g, "\\'")
  return page.locator(
    `xpath=((//*[self::label or self::div or self::span or self::p][normalize-space(text())='${text}'])[last()]` +
      `/following::*[self::input or self::select or self::textarea][1])`,
  )
}
