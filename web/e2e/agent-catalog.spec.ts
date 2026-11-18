import { expect, test } from '@playwright/test';
import type { AgentSummary } from '../src/api/agents';
import type { GraphSummary } from '../src/api/types';

const headers = {Authorization:'Bearer e2e-admin-token'};

test('creates, reuses and configures an agent through workflow creation and the catalog', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/graphs');
  await page.getByRole('button', {name:'New graph', exact:true}).click();
  const dialog = page.getByRole('dialog', {name:'New graph'});
  await dialog.getByRole('textbox', {name:'Workflow document'}).fill(JSON.stringify({
    metadata:{id:'catalog-review-source',name:'Catalog review source'},
    spec:{nodes:[{id:'reviewer',name:'Catalog reviewer',description:'Reviews incoming changes.',
      runtime:{kind:'native'},model:{provider:'e2e'},prompt:'Review @repository.',loop:{mode:'event'}}],
      parameters:[{name:'repository',value:'woliveiras/weft'}],channels:[{id:'requests',to:['reviewer']}]},
  }));
  await dialog.getByRole('button', {name:'Create', exact:true}).click();
  await expect(dialog).not.toBeVisible();

  const agents = page.getByRole('navigation', {name:'Main navigation'}).getByRole('button', {name:'Agents', exact:true});
  await agents.focus();
  await page.keyboard.press('Enter');
  await expect(agents).toHaveAttribute('aria-current', 'page');
  await page.getByRole('searchbox', {name:'Search agents'}).fill('Reviews incoming');
  const card = page.getByRole('link', {name:'Catalog reviewer Reviews incoming changes. Version 1', exact:true});
  await expect(card).toBeVisible();
  const catalog = await (await page.request.get('/api/v1/agents', {headers})).json() as AgentSummary[];
  const agent = catalog.find(item => item.origin?.workflow === 'catalog-review-source');
  expect(agent?.latest).toBe(1);
  await card.click();
  const agentId = page.getByRole('textbox', {name:'Agent ID', exact:true});
  const description = page.getByRole('textbox', {name:'Description', exact:true});
  const model = page.getByRole('combobox', {name:'Model', exact:true});
  await expect(description).toHaveAttribute('rows', '4');
  await expect(model).toHaveAttribute('placeholder', 'fixture');
  const width = await agentId.evaluate(input => input.getBoundingClientRect().width);
  for (const field of [description, model]) {
    expect(Math.abs(await field.evaluate(input => input.getBoundingClientRect().width) - width)).toBeLessThanOrEqual(1);
  }
  expect(await model.evaluate(input => getComputedStyle(input, '::placeholder').color))
    .toBe(await model.evaluate(input => getComputedStyle(input).color));
  await model.fill('custom-model');
  await expect(model).toHaveValue('custom-model');
  await model.clear();
  const sections = page.getByRole('navigation', {name:'Agent definition sections'});
  await sections.getByRole('button', {name:'Tools & permissions', exact:true}).click();
  const checkbox = page.getByRole('checkbox', {name:'Require a read-only workspace', exact:true});
  const checkboxLabel = page.getByText('Require a read-only workspace', {exact:true});
  const geometry = await checkbox.evaluate(input => {
    const checkbox = input.getBoundingClientRect();
    const text = input.nextElementSibling?.getBoundingClientRect();
    return text && {gap:text.left - checkbox.right, offset:Math.abs(text.top + text.height / 2 - checkbox.top - checkbox.height / 2)};
  });
  await expect(checkboxLabel).toBeVisible();
  expect(geometry?.gap).toBeGreaterThanOrEqual(0);
  expect(geometry?.gap).toBeLessThan(16);
  expect(geometry?.offset).toBeLessThan(2);
  const sandboxTitle = page.getByText('Command sandbox', {exact:true});
  const isolation = page.getByRole('combobox', {name:'Isolation', exact:true});
  const titleEdge = await sandboxTitle.evaluate(title => title.getBoundingClientRect().left);
  expect(Math.abs(titleEdge - await isolation.evaluate(input => input.getBoundingClientRect().left))).toBeLessThanOrEqual(1);
  const reloadMcp = page.getByRole('button', {name:'Reload MCP tools', exact:true});
  const mcpLayout = () => reloadMcp.evaluate(button => {
    const action = button.getBoundingClientRect();
    const text = button.previousElementSibling?.getBoundingClientRect();
    const row = button.parentElement?.getBoundingClientRect();
    return text && row && {
      gap: action.left - text.right,
      rowEnd: row.right - action.right,
      wraps: action.top >= text.bottom,
    };
  });
  const wide = await mcpLayout();
  expect(wide?.gap).toBeGreaterThan(0);
  expect(wide?.rowEnd).toBeCloseTo(0);
  expect(wide?.wraps).toBe(false);
  const desktop = page.viewportSize();
  if (desktop) {
    await page.setViewportSize({width:540, height:desktop.height});
    expect((await mcpLayout())?.wraps).toBe(true);
    await page.setViewportSize(desktop);
  }
  await sections.getByRole('button', {name:'Workflows', exact:true}).click();
  await expect(page.getByRole('link', {name:'Catalog review source / Catalog reviewer'})).toBeVisible();

  await page.goto('/#/graphs');
  await page.getByRole('button', {name:'New graph', exact:true}).click();
  const document = dialog.getByRole('textbox', {name:'Workflow document'});
  await document.fill(`# Target workflow
metadata:
  id: catalog-review-target
  name: Catalog review target
spec:
  nodes: []
`);
  await dialog.getByRole('button', {name:'Add existing agent'}).click();
  const picker = page.getByRole('dialog', {name:'Select a reusable agent'});
  await picker.getByRole('searchbox', {name:'Search agents'}).fill('Reviews incoming');
  await picker.getByRole('combobox', {name:'Reusable agent'}).selectOption(agent!.id);
  await expect(picker.getByRole('button', {name:'Add to workflow'})).toBeDisabled();
  await picker.getByRole('combobox', {name:'Agent version'}).selectOption('1');
  await picker.getByRole('textbox', {name:/repository/}).fill('other/project');
  await picker.getByRole('button', {name:'Add to workflow'}).click();
  await expect(document).toHaveValue(/# Target workflow/);
  await dialog.getByRole('button', {name:'Create', exact:true}).click();
  await expect(dialog).not.toBeVisible();

  await page.goto(`/#/agents/${agent!.id}`);
  await sections.getByRole('button', {name:'Workflows', exact:true}).click();
  await page.getByRole('link', {name:'Catalog review target / Catalog reviewer'}).click();
  await page.getByRole('button', {name:'Instructions', exact:true}).click();
  await page.getByRole('textbox', {name:'Workflow instructions'}).fill('Inspect only API changes.');
  await page.getByRole('button', {name:'Review & save'}).click();
  const submitted = page.waitForResponse(response => response.url().endsWith('/api/v1/graphs/catalog-review-target')
    && response.request().method() === 'PUT');
  await page.getByRole('button', {name:'Save graph draft'}).click();
  const saved = await submitted;
  expect(saved.status()).toBe(200);
  const target = await saved.json() as GraphSummary;
  expect(target.graph.spec.nodes[0]).toMatchObject({
    prompt:'Review @repository.', localInstructions:'Inspect only API changes.',
    agent:{id:agent!.id,version:1,parameters:{repository:'other/project'}},
  });

  const source = await (await page.request.get('/api/v1/graphs/catalog-review-source', {headers})).json() as GraphSummary;
  expect(source.graph.spec.nodes[0].localInstructions).toBeUndefined();
  expect(source.graph.spec.channels?.[0].to).toEqual(['reviewer']);

  await page.goto(`/#/agents/${agent!.id}`);
  const publish = page.getByRole('button', {name:'Publish', exact:true});
  const pending = page.getByText('Unpublished changes', {exact:true});
  await expect(publish).toBeDisabled();
  const agentName = page.getByRole('textbox', {name:'Name', exact:true});
  await agentName.fill('Release reviewer');
  await expect(pending).toBeVisible();
  await agentName.fill('Catalog reviewer');
  await expect(pending).toHaveCount(0);
  await expect(publish).toBeDisabled();
  await agentName.fill('Release reviewer');
  await sections.getByRole('button', {name:'Tools & permissions', exact:true}).click();
  await expect(pending).toBeVisible();
  await checkbox.check();
  await sections.getByRole('button', {name:'Instructions', exact:true}).click();
  await page.getByRole('textbox', {name:'Agent instructions'}).fill('Review @repository with the revised checklist.');
  await sections.getByRole('button', {name:'Workflows', exact:true}).click();
  await expect(pending).toBeVisible();
  const publication = page.waitForResponse(response => response.url().endsWith(`/api/v1/agents/${agent!.id}`)
    && response.request().method() === 'PUT');
  await publish.click();
  const published = await publication;
  expect(published.ok()).toBe(true);
  const { versions } = await published.json();
  expect(versions).toHaveLength(2);
  expect(versions[1].definition).toMatchObject({
    name:'Release reviewer', readOnly:true, prompt:'Review @repository with the revised checklist.',
  });
  await expect(page.getByRole('combobox', {name:'Version', exact:true})).toHaveValue('2');
  await expect(pending).toHaveCount(0);
  await expect(publish).toBeDisabled();
  await sections.getByRole('button', {name:'Workflows', exact:true}).click();
  await page.getByRole('link', {name:'Catalog review target / Catalog reviewer'}).click();
  const binding = page.getByRole('region', {name:'Agent definition', exact:true});
  await expect(binding.getByText('Version 1')).toBeVisible();
  expect(await page.getByRole('link', {name:'Save as reusable'}).count()).toBe(0);
});
