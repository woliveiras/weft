import type {Page} from '@playwright/test';

// Exercise the real disclosure before interacting with advanced draft fields.
export async function openTaskSettings(page:Page) {
 const summary=page.getByText('Review settings',{exact:true});
 if(await summary.locator('..').getAttribute('open')===null)await summary.click();
}
