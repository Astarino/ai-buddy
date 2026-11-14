import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { api } from './api';
import { HelpRequestsPanel, PublicSearchPanel } from './collaboration';
import type { HelpRequest, Workspace } from './types';
import { workspace } from './test/fixtures';

vi.mock('./api', async () => {
  const original = await vi.importActual<typeof import('./api')>('./api');
  return {
    ...original,
    api: Object.fromEntries(Object.keys(original.api).map((key) => [key, vi.fn()])),
  };
});
const mock = vi.mocked(api);
let state: Workspace;
function item(id = 'request-one'): HelpRequest {
  return {
    id,
    requester_id: 'anton',
    requester: 'Антон Соколов',
    assignee_id: 'dasha',
    assignee: 'Даша Волкова',
    kind: 'mentor',
    subject: 'Не запускаются тесты',
    body: 'Проверил команду и папку с модулем',
    status: 'waiting',
    version: 2,
    created_at: '2026-11-14T12:00:00Z',
    updated_at: '2026-11-14T12:00:00Z',
    replies: [],
  };
}
function Harness({ compose = false }: { compose?: boolean }) {
  const [value, setValue] = useState(structuredClone(state));
  return (
    <HelpRequestsPanel
      workspace={value}
      initialCompose={compose}
      onRefresh={async () => setValue(structuredClone(state))}
    />
  );
}
beforeEach(() => {
  vi.resetAllMocks();
  state = workspace();
});

it('fetches public sources, keeps input and the search id after failure, and asks Buddy using the searched question', async () => {
  const user = userEvent.setup();
  const refresh = vi.fn();
  const ask = vi.fn();
  const source = vi.fn();
  mock.search.mockRejectedValueOnce(new Error('Нет связи с документацией')).mockResolvedValue({
    id: 'search-one',
    topic: 'go',
    query: 'Go: не видит go.mod',
    fetched_at: '2026-11-14T12:00:00Z',
    documents: [
      {
        id: 'web-one',
        title: 'Go modules',
        url: 'https://go.dev/ref/mod',
        body: 'module root',
        scope: 'public',
        team: '',
        origin: 'Go',
        version: 'hash',
        updated_at: '2026-11-14',
        synthetic: false,
      },
    ],
  });
  render(<PublicSearchPanel onRefresh={refresh} onAsk={ask} onSource={source} />);
  await user.type(screen.getByLabelText('С чем нужна помощь'), 'Go: не видит go.mod');
  await user.click(screen.getByRole('button', { name: 'Найти' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Нет связи');
  expect(screen.getByLabelText('С чем нужна помощь')).toHaveValue('Go: не видит go.mod');
  await user.click(screen.getByRole('button', { name: 'Найти' }));
  await user.click(await screen.findByRole('button', { name: /Go modules/ }));
  expect(source).toHaveBeenCalledWith('web-one');
  expect(mock.search.mock.calls[0][0]).toBe(mock.search.mock.calls[1][0]);
  expect(refresh).toHaveBeenCalledOnce();
  await user.click(screen.getByRole('button', { name: 'Разобрать с Buddy' }));
  expect(ask).toHaveBeenCalledWith('Go: не видит go.mod');
});

it('distinguishes empty search from a network error and creates a new key for a different topic', async () => {
  const user = userEvent.setup();
  mock.search.mockResolvedValue({
    id: 'empty',
    topic: 'git',
    query: 'unknown',
    fetched_at: '2026-11-14T12:00:00Z',
    documents: [],
  });
  render(<PublicSearchPanel onRefresh={vi.fn()} onAsk={vi.fn()} onSource={vi.fn()} />);
  await user.type(screen.getByLabelText('С чем нужна помощь'), 'unknown');
  await user.click(screen.getByRole('button', { name: 'Найти' }));
  await screen.findByText(/Подходящих фрагментов нет/);
  expect(screen.queryByRole('button', { name: 'Разобрать с Buddy' })).not.toBeInTheDocument();
  await user.selectOptions(screen.getByLabelText('Инструмент'), 'git');
  await user.click(screen.getByRole('button', { name: 'Найти' }));
  await screen.findByText(/Подходящих фрагментов нет/);
  expect(mock.search.mock.calls[0][0]).not.toBe(mock.search.mock.calls[1][0]);
});

it('previews the recipient and shared text before sending, then allows the intern to close the problem', async () => {
  const user = userEvent.setup();
  const draft = { ...item(), status: 'draft', version: 1 };
  mock.prepareRequest.mockImplementation(async () => {
    state.requests = [draft];
    return draft;
  });
  mock.changeRequest.mockImplementation(async (_id, command) => {
    state.requests = state.requests.map((r) => ({
      ...r,
      status: command === 'send' ? 'waiting' : 'resolved',
      version: r.version + 1,
    }));
    return state.requests[0];
  });
  render(<Harness compose />);
  await user.type(screen.getByLabelText('Тема'), draft.subject);
  await user.type(screen.getByLabelText('Что передать'), draft.body);
  await user.click(screen.getByRole('button', { name: 'Проверить перед отправкой' }));
  await screen.findByText('Даша Волкова');
  expect(mock.changeRequest).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Изменить' }));
  expect(screen.getByLabelText('Что передать')).toHaveValue(draft.body);
  await user.selectOptions(screen.getByLabelText('Кому'), 'owner');
  await user.click(screen.getByRole('button', { name: 'Проверить перед отправкой' }));
  await user.click(await screen.findByRole('button', { name: 'Отправить вопрос' }));
  await screen.findByText('Ответственный: Даша Волкова');
  await user.click(screen.getByRole('button', { name: 'Проблема решена' }));
  await screen.findByText('Закрыт');
  await user.click(screen.getByRole('button', { name: '← Все запросы' }));
  await user.click(screen.getByRole('button', { name: 'История' }));
  expect(screen.getByRole('button', { name: /Не запускаются тесты/ })).toBeInTheDocument();
});

it('groups five interns and keeps a failed reply for an idempotent retry', async () => {
  const user = userEvent.setup();
  state.profile = { ...state.profile, id: 'dasha', name: 'Даша Волкова', audience: 'responsible' };
  state.requests = ['Антон', 'Лера', 'Никита', 'София', 'Тимур'].map((name, i) => ({
    ...item(`request-${i}`),
    requester: name,
    requester_id: `intern-${i}`,
    subject: `Проблема ${i}`,
  }));
  mock.replyRequest
    .mockRejectedValueOnce(new Error('Временная ошибка'))
    .mockImplementation(async (id, replyID, text) => {
      state.requests = state.requests.map((r) =>
        r.id === id
          ? {
              ...r,
              status: 'answered',
              version: r.version + 1,
              replies: [
                {
                  id: replyID,
                  author_id: 'dasha',
                  author: 'Даша Волкова',
                  text,
                  created_at: '2026-11-14T12:05:00Z',
                },
              ],
            }
          : r,
      );
      return state.requests.find((r) => r.id === id)!;
    });
  render(<Harness />);
  expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(5);
  await user.click(screen.getByRole('button', { name: /Проблема 3/ }));
  expect(screen.getByText('София')).toBeVisible();
  expect(screen.queryByText('Проблема 0')).not.toBeInTheDocument();
  await user.type(screen.getByLabelText('Ответ'), 'Проверь папку с go.mod');
  await user.click(screen.getByRole('button', { name: 'Ответить' }));
  await screen.findByRole('alert');
  expect(screen.getByLabelText('Ответ')).toHaveValue('Проверь папку с go.mod');
  await user.click(screen.getByRole('button', { name: 'Ответить' }));
  await screen.findByText('Ответ получен');
  expect(screen.getByLabelText('Ответ')).toHaveValue('');
  expect(mock.replyRequest.mock.calls[0][1]).toBe(mock.replyRequest.mock.calls[1][1]);
  expect(screen.getByLabelText('Обсуждение запроса')).toHaveTextContent('Даша Волкова');
  await user.click(screen.getByRole('button', { name: '← Все запросы' }));
  await user.click(screen.getByRole('button', { name: 'История' }));
  await screen.findByText('Закрытых запросов пока нет.');
  await user.click(screen.getByRole('button', { name: 'Обновить запросы' }));
});

it('shows approval and grant as separate owner actions and keeps service failures visible', async () => {
  const user = userEvent.setup();
  state.profile = { ...state.profile, id: 'wiki-owner', audience: 'responsible' };
  state.requests = [
    {
      ...item(),
      assignee_id: 'wiki-owner',
      assignee: 'Алексей Смирнов',
      kind: 'access',
      operation_status: 'pending',
    },
  ];
  mock.decideRequest
    .mockRejectedValueOnce(new Error('Сервис не ответил'))
    .mockImplementation(async (_id, status) => {
      state.requests = [
        {
          ...state.requests[0],
          operation_status: status,
          version: state.requests[0].version + 1,
          status: ['granted', 'revoked', 'rejected'].includes(status) ? 'resolved' : 'waiting',
        },
      ];
      return state.requests[0];
    });
  render(<Harness />);
  await user.click(screen.getByRole('button', { name: /Не запускаются тесты/ }));
  await user.click(screen.getByRole('button', { name: 'Согласовать' }));
  await screen.findByRole('alert');
  expect(screen.queryByRole('button', { name: 'Выдать доступ' })).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Согласовать' }));
  await user.click(await screen.findByRole('button', { name: 'Выдать доступ' }));
  await screen.findByRole('button', { name: 'Отозвать доступ' });
  expect(screen.queryByRole('button', { name: 'Проблема решена' })).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Отозвать доступ' }));
  await screen.findByText('Доступ отозван');
});

it('recovers a saved draft and never exposes a send button to the assignee', async () => {
  const user = userEvent.setup();
  state.requests = [{ ...item(), status: 'draft', version: 1 }];
  mock.changeRequest.mockImplementation(async () => {
    state.requests[0].status = 'waiting';
    return state.requests[0];
  });
  render(<Harness />);
  await user.click(screen.getByRole('button', { name: /Не запускаются тесты/ }));
  await user.click(screen.getByRole('button', { name: 'Отправить вопрос' }));
  await screen.findByLabelText('Ответ');
  expect(
    within(screen.getByLabelText('Обсуждение запроса')).queryAllByText('AI Buddy'),
  ).toHaveLength(0);
});
