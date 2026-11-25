import type {Page} from '@playwright/test';

// Configuration prerequisites remain visible; scroll the section into view.
export async function openTaskSettings(page:Page) {
 await page.getByRole('heading',{name:'2. Workflow & repository'}).scrollIntoViewIfNeeded();
}

// Task tracking uses explicit sections instead of one long disclosure list.
export async function openTaskSection(page:Page, name:'Overview'|'Environment & logs'|'Changes & checks'|'Activity'|'Request'|'Settings & access') {
 await page.getByRole('navigation',{name:'Task sections'}).getByRole('link',{name,exact:true}).click();
}

export async function openTaskStopControls(page:Page) {
 await page.locator('summary').filter({hasText:/^Stop task$/}).click();
}
