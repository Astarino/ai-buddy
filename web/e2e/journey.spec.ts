import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Workspace } from '../src/types';

const profile = (mobile: boolean) =>
  mobile
    ? {
        id: 'irina',
        name: 'Ирина',
        full: 'Ирина Белова',
        team: 'delivery',
        space: 'DELIVERY',
        project: 'DLV',
      }
    : {
        id: 'anton',
        name: 'Антон',
        full: 'Антон Соколов',
        team: 'catalog',
        space: 'CATALOG',
        project: 'CAT',
      };

async function enter(page: Page, mobile: boolean) {
  const person = profile(mobile);
  await page.goto('/');
  await page.getByRole('button', { name: new RegExp(person.full) }).click();
  await expect(page.getByRole('heading', { name: `Привет, ${person.name}.` })).toBeVisible();
  return person;
}

async function demo(page: Page, mobile: boolean) {
  if (mobile) await page.getByRole('button', { name: 'Рабочие разделы' }).click();
  await page.getByRole('button', { name: 'Пульт демонстрации', exact: true }).click();
  return page.getByRole('dialog');
}

test('access, reload, approval, guide and first Jira task', async ({ page, context, isMobile }) => {
  const person = await enter(page, isMobile);
  await page.getByRole('button', { name: 'Не открывается вики', exact: true }).click();
  const confirmation = page.getByRole('region', { name: 'Заявка на доступ' });
  await expect(confirmation).toContainText(person.space);
  await expect(confirmation).toContainText('Чтение');
  let state: Workspace = await (await context.request.get('/api/state')).json();
  expect(state.operations).toHaveLength(0);
  await confirmation.getByRole('button', { name: 'Отправить заявку' }).click();
  await expect(confirmation).toContainText('Отправлено · REQ-');
  await page.reload();
  await expect(page.getByRole('log')).toContainText('Отправлено · REQ-');
  state = await (await context.request.get('/api/state')).json();
  const access = state.operations.find((operation) => operation.kind === 'access')!;
  expect(access.status).toBe('pending');
  expect(state.tasks.find((task) => task.id === 'space-access')?.status).toBe('waiting');

  const operator = await demo(page, isMobile);
  const token = operator.getByLabel('Токен оператора из конфигурации запуска');
  await expect(operator.getByRole('button', { name: 'Согласовать', exact: true })).toBeDisabled();
  await token.fill('incorrect');
  await operator.getByRole('button', { name: 'Согласовать', exact: true }).click();
  await expect(operator.getByRole('alert')).toBeVisible();
  await token.fill(process.env.E2E_OPERATOR_TOKEN!);
  await operator.getByRole('button', { name: 'Согласовать', exact: true }).click();
  await expect(operator).toContainText('Согласовано, ждём выдачи');
  expect((await context.request.get(`/api/documents/${person.team}-guide`)).status()).toBe(404);
  await operator.getByRole('button', { name: 'Выдать права' }).click();
  await expect(operator).toContainText('Доступ выдан');
  await operator.getByRole('button', { name: 'Закрыть панель' }).click();

  await page.getByRole('button', { name: 'Открыть руководство', exact: true }).click();
  const source = page.getByRole('dialog');
  await expect(source).toContainText('Тестовая инструкция. Не регламент Авито.');
  await expect(source).toContainText('Тестовое руководство команды');
  await source.getByRole('button', { name: 'Отметить руководство изученным' }).click();
  await expect(source).toContainText('Изучение отмечено в маршруте');
  await source.getByRole('button', { name: 'Подготовить первую задачу' }).click();
  const assignment = page.getByRole('region', { name: 'Назначение задачи' });
  await expect(assignment).toContainText(`${person.project}-101`);
  await assignment.getByRole('button', { name: 'Взять задачу', exact: true }).click();
  await expect(assignment).toContainText(`Отправлено · ${person.project}-101`);
  await page.reload();
  await expect(page.getByRole('log')).toContainText(
    `Задача ${person.project}-101 назначена на тебя`,
  );
  state = await (await context.request.get('/api/state')).json();
  expect(state.operations).toHaveLength(2);
  expect(state.tasks.find((task) => task.id === 'first-issue')?.status).toBe('completed');
  expect(state.tasks.find((task) => task.id === 'finish-issue')?.status).toBe('in_progress');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('separate session cannot read another profile or confirmation', async ({
  browser,
  page,
  context,
  isMobile,
}) => {
  const person = await enter(page, isMobile);
  const state: Workspace = await (await context.request.get('/api/state')).json();
  const target =
    state.actions[0] ??
    (await (
      await context.request.post('/api/actions', {
        data: { kind: 'access', reason: 'Изучить руководство команды' },
      })
    ).json());
  const other = await browser.newContext();
  try {
    const otherID = person.id === 'anton' ? 'irina' : 'anton';
    await other.request.post(`${process.env.E2E_BASE_URL}/api/session`, {
      data: { profile_id: otherID },
    });
    const forbidden = await other.request.post(
      `${process.env.E2E_BASE_URL}/api/actions/${target.id}/confirm`,
      { data: {} },
    );
    expect(forbidden.status()).toBe(404);
    expect(
      (
        await other.request.get(`${process.env.E2E_BASE_URL}/api/documents/${person.team}-guide`)
      ).status(),
    ).toBe(404);
    const isolated: Workspace = await (
      await other.request.get(`${process.env.E2E_BASE_URL}/api/state`)
    ).json();
    expect(isolated.profile.id).toBe(otherID);
    expect(isolated.operations.every((operation) => operation.user_id === otherID)).toBe(true);
  } finally {
    await other.close();
  }
});

test('route supports keyboard navigation and sources remain accessible on mobile', async ({
  page,
  isMobile,
}) => {
  await enter(page, isMobile);
  if (isMobile) await page.getByRole('button', { name: 'Открыть маршрут' }).click();
  const route = isMobile
    ? page.getByRole('dialog')
    : page.getByRole('complementary', { name: 'Маршрут адаптации' });
  await route.getByRole('tab', { name: '1 неделя' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(route.getByRole('tab', { name: '2 неделя' })).toBeFocused();
  await expect(route.getByRole('tab', { name: '2 неделя' })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  if (isMobile) {
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Рабочие разделы' }).click();
  }
  await page.getByRole('button', { name: 'База знаний', exact: true }).click();
  const sources = page.getByRole('dialog');
  await sources.getByRole('textbox', { name: 'Поиск по источникам' }).fill('неизвестный материал');
  await expect(sources).toContainText('Такого материала среди доступных нет');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('a lost response is reconciled by refresh without resending the action', async ({
  page,
  context,
  isMobile,
}) => {
  const person = await enter(page, isMobile);
  const state: Workspace = await (await context.request.get('/api/state')).json();
  const active = state.operations.find(
    (operation) => operation.kind === 'access' && operation.status === 'granted',
  )!;
  const headers = { 'X-Demo-Token': process.env.E2E_OPERATOR_TOKEN! };
  await context.request.post('/api/demo/events', {
    headers,
    data: { key: active.key, status: 'revoked' },
  });
  await page.reload();
  await page.getByRole('button', { name: 'Не открывается вики', exact: true }).click();
  const cards = page.getByRole('region', { name: 'Заявка на доступ' });
  const latest = cards.last();
  await expect(latest).toContainText(person.space);
  const ready: Workspace = await (await context.request.get('/api/state')).json();
  const action = ready.actions.find((action) => action.status === 'ready')!;
  await context.request.post('/api/demo/events', {
    headers,
    data: { key: '', status: 'lost_response' },
  });
  await latest.getByRole('button', { name: 'Отправить заявку' }).click();
  await expect(page.getByRole('alert')).toContainText('результат мог сохраниться');
  await context.request.post('/api/demo/events', { headers, data: { key: '', status: 'normal' } });
  await page.getByRole('button', { name: 'Обновить состояние', exact: true }).click();
  await expect(latest).toContainText('Отправлено · REQ-');
  expect(await latest.getByRole('button', { name: 'Отправить заявку' }).count()).toBe(0);
  const restored: Workspace = await (await context.request.get('/api/state')).json();
  expect(restored.operations.filter((operation) => operation.key === action.id)).toHaveLength(1);
});
