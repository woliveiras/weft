import type {Page} from '@playwright/test';

// Ordinary prerequisites remain visible; optional fields live in one disclosure.
export async function openTaskSettings(page:Page) {
 const advanced=page.locator('details').filter({has:page.locator('summary').filter({hasText:/^Advanced settings/})}).first();
 await advanced.waitFor({state:'visible'});
 if(!await advanced.evaluate(element=>(element as HTMLDetailsElement).open))await advanced.locator('summary').first().click();
 await advanced.scrollIntoViewIfNeeded();
}

// Task tracking uses explicit sections instead of one long disclosure list.
export async function openTaskSection(page:Page, name:'Overview'|'Environment & logs'|'Changes & checks'|'Activity'|'Request'|'Settings & access') {
 await page.getByRole('navigation',{name:'Task sections'}).getByRole('link',{name,exact:true}).click();
}

export async function openTaskStopControls(page:Page) {
 await page.locator('summary').filter({hasText:/^Stop task$/}).click();
}
