import { expect, request, test, type Page } from '@playwright/test';

async function login(page: Page, name: string, responsible = false) {
  await page.goto('/');
  if (responsible) await page.getByRole('button', { name: 'Ответственный', exact: true }).click();
  await page.getByRole('button', { name: new RegExp(name) }).click();
  await expect(page.getByRole('heading', { name: 'AI Buddy', exact: true })).toBeVisible();
}
async function section(page: Page, mobile: boolean, name: string) {
  if (mobile) await page.getByRole('button', { name: 'Рабочие разделы' }).click();
  await page.getByRole('button', { name, exact: true }).last().click();
  return page.getByRole('dialog');
}

test('five interns reach one mentor without sharing private chats', async ({
  page,
  isMobile,
  baseURL,
}) => {
  const suffix = `${isMobile ? 'm' : 'd'}-${Date.now()}`;
  const requestIDs = new Map<string, string>();
  for (const id of ['anton', 'lera', 'nikita', 'sofia', 'timur']) {
    const sender = await request.newContext({ baseURL });
    await sender.post('/api/session', { data: { profile_id: id } });
    const prepared = await sender.post('/api/requests', {
      data: {
        id: `${suffix}-${id}`,
        kind: 'mentor',
        subject: `Тесты ${suffix}-${id}`,
        body: 'Я проверил команду и рабочую папку. Нужна помощь с запуском тестов.',
      },
    });
    expect(prepared.ok()).toBeTruthy();
    const draft = await prepared.json();
    requestIDs.set(id, draft.id);
    expect(
      (
        await sender.post(`/api/requests/${draft.id}/change`, {
          data: { command: 'send', version: draft.version },
        })
      ).ok(),
    ).toBeTruthy();
    await sender.dispose();
  }
  await login(page, 'Даша Волкова', true);
  const inbox = await section(page, isMobile, 'Нужен мой ответ');
  await expect(inbox.getByRole('heading', { level: 3 })).toHaveCount(5);
  await inbox.getByRole('button', { name: new RegExp(`Тесты ${suffix}-sofia`) }).click();
  await expect(inbox).toContainText('София Ким');
  await expect(inbox).not.toContainText(`Тесты ${suffix}-anton`);
  await inbox
    .getByLabel('Ответ', { exact: true })
    .fill('Открой папку с go.mod и повтори go test ./...');
  await inbox.getByRole('button', { name: 'Ответить', exact: true }).click();
  await expect(inbox).toContainText('Ответ получен');
  await page.reload();
  const reloaded = await section(page, isMobile, 'Нужен мой ответ');
  await reloaded.getByRole('button', { name: new RegExp(`Тесты ${suffix}-sofia`) }).click();
  await expect(reloaded.getByLabel('Обсуждение запроса')).toContainText('Даша Волкова');
  const intern = await request.newContext({ baseURL });
  await intern.post('/api/session', { data: { profile_id: 'sofia' } });
  const result = await (await intern.get(`/api/requests/${requestIDs.get('sofia')}`)).json();
  expect(result.replies).toHaveLength(1);
  expect(result.status).toBe('answered');
  await intern.post('/api/session', { data: { profile_id: 'irina' } });
  expect((await intern.get(`/api/requests/${requestIDs.get('sofia')}`)).status()).toBe(404);
  await intern.dispose();
});

test('only the resource owner approves and grants an interns access', async ({
  page,
  isMobile,
  baseURL,
}) => {
  const sender = await request.newContext({ baseURL });
  const profile = isMobile ? 'timur' : 'nikita';
  await sender.post('/api/session', { data: { profile_id: profile } });
  const prepared = await (
    await sender.post('/api/actions', {
      data: { kind: 'access', reason: 'Изучить руководство команды' },
    })
  ).json();
  expect(
    (await sender.post(`/api/actions/${prepared.id}/confirm`, { data: {} })).ok(),
  ).toBeTruthy();
  await login(page, 'Алексей Смирнов', true);
  const inbox = await section(page, isMobile, 'Нужен мой ответ');
  await inbox
    .getByRole('region', { name: isMobile ? 'Тимур Алиев' : 'Никита Морозов' })
    .getByRole('button', { name: /Доступ к CATALOG/ })
    .click();
  await inbox.getByRole('button', { name: 'Согласовать', exact: true }).click();
  await expect(inbox).toContainText('Согласовано, ждём выдачи');
  expect((await sender.get('/api/documents/catalog-guide')).status()).toBe(404);
  await inbox.getByRole('button', { name: 'Выдать доступ', exact: true }).click();
  await expect(inbox).toContainText('Доступ выдан');
  expect((await sender.get('/api/documents/catalog-guide')).status()).toBe(200);
  await sender.dispose();
});

test('live official search saves a source and grounds a separate Buddy answer', async ({
  page,
  isMobile,
}) => {
  test.skip(process.env.RUN_LIVE_BROWSER !== 'true', 'opt-in real external documentation fetch');
  await login(page, isMobile ? 'Лера Петрова' : 'София Ким');
  const search = await section(page, isMobile, 'Найти в документации');
  await search
    .getByLabel('С чем нужна помощь')
    .fill('go test ./...: directory prefix does not contain main module go.mod');
  await search.getByRole('button', { name: 'Найти', exact: true }).click();
  await expect(search.getByRole('button', { name: /Go Modules Reference/ })).toBeVisible({
    timeout: 25000,
  });
  await search.getByRole('button', { name: 'Разобрать с Buddy' }).click();
  await expect(page.getByRole('log')).toContainText('Go Modules Reference');
  await page.reload();
  await page
    .getByRole('log')
    .getByRole('button', { name: /Go Modules Reference/ })
    .click();
  await expect(page.getByRole('dialog')).toContainText('module root directory');
});
