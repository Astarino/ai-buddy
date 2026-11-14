import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import App from './App';
import { ActionCard } from './components';
import { api, ApiError } from './api';
import { action, fullSource, operation, profiles, workspace } from './test/fixtures';
import type { Workspace } from './types';

vi.mock('./api', async () => {
  const original = await vi.importActual<typeof import('./api')>('./api');
  return {
    ...original,
    api: Object.fromEntries(Object.keys(original.api).map((key) => [key, vi.fn()])),
  };
});
const mock = vi.mocked(api);
let state: Workspace;

beforeEach(() => {
  vi.resetAllMocks();
  state = workspace();
  mock.bootstrap.mockResolvedValue({ profiles, model: state.model, demo: true });
  mock.state.mockImplementation(async () => structuredClone(state));
  mock.login.mockResolvedValue({ ok: true });
  mock.logout.mockResolvedValue({ ok: true });
  mock.source.mockResolvedValue(fullSource);
  mock.refresh.mockImplementation(async () => structuredClone(state));
  mock.prepare.mockImplementation(async (kind) => {
    const prepared = action(kind);
    state.actions = [prepared];
    return prepared;
  });
  mock.message.mockImplementation(async (id, text) => {
    state.messages.push({
      id,
      role: 'user',
      text,
      created_at: new Date().toISOString(),
      source_ids: [],
    });
    const reply = {
      id: `${id}-reply`,
      role: 'assistant' as const,
      text: 'Проверь параметры перед отправкой.',
      created_at: new Date().toISOString(),
      source_ids: ['access-policy'],
      action_id: state.actions[0]?.id,
    };
    state.messages.push(reply);
    return reply;
  });
  mock.confirm.mockImplementation(async () => {
    const result = operation();
    state.operations = [result];
    state.actions[0] = { ...state.actions[0], status: 'completed', result_id: result.id };
    return result;
  });
  mock.complete.mockImplementation(async (id) => {
    state.tasks = state.tasks.map((task) =>
      task.id === id ? { ...task, status: 'completed' } : task,
    );
    return state;
  });
  mock.draft.mockImplementation(async () => {
    state.draft = 'Даша, привет! Нужна помощь с рабочей задачей.';
    return { draft: state.draft };
  });
  mock.event.mockImplementation(async (_token, key, status) => {
    state.operations = state.operations.map((op) => (op.key === key ? { ...op, status } : op));
    return state;
  });
  mock.diagnostics.mockResolvedValue({
    model: state.model,
    prompt_version: 'buddy-grounded-v2',
    events: [],
  });
  mock.metrics.mockResolvedValue({ action_completed: 1 });
});

async function start() {
  render(<App />);
  await screen.findByRole('heading', { name: 'Привет, Антон.' });
}

describe('Корпоративный вход и передача данных', () => {
  it('uses corporate login without showing demo identities', async () => {
    mock.state.mockRejectedValue(new ApiError('unauthorized', 'Войди', 401));
    mock.bootstrap.mockResolvedValue({
      profiles: [],
      model: state.model,
      demo: false,
      login_url: '/api/login',
    });
    render(<App />);
    expect(await screen.findByRole('link', { name: 'Войти через компанию' })).toHaveAttribute(
      'href',
      '/api/login',
    );
    expect(
      screen.queryByRole('heading', { name: 'Выбери тестовый профиль' }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText('ДЕМОНСТРАЦИОННЫЙ КОНТУР')).not.toBeInTheDocument();
  });

  it('sends an external question only after reviewing that question and provider', async () => {
    mock.bootstrap.mockResolvedValue({
      profiles,
      model: 'approved-model',
      demo: true,
      external_model: true,
      model_endpoint: 'https://approved.example/v1',
      consent_to: 'destination-one',
    });
    await start();
    const user = userEvent.setup();
    const composer = screen.getByRole('textbox', { name: 'Сообщение AI Buddy' });
    await user.type(composer, 'Как проверить публичный пример?');
    await user.click(screen.getByRole('button', { name: 'Отправить сообщение' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('https://approved.example/v1');
    expect(dialog).toHaveTextContent('Как проверить публичный пример?');
    expect(mock.message).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole('button', { name: 'Отмена' }));
    expect(composer).toHaveValue('Как проверить публичный пример?');
    await user.click(screen.getByRole('button', { name: 'Отправить сообщение' }));
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Отправить вопрос' }),
    );
    await waitFor(() =>
      expect(mock.message).toHaveBeenCalledWith(
        expect.any(String),
        'Как проверить публичный пример?',
        'destination-one',
      ),
    );
  });

  it('disables a replaced confirmation card', () => {
    const confirm = vi.fn();
    render(
      <ActionCard
        action={{ ...action(), status: 'superseded' }}
        busy={false}
        onConfirm={confirm}
        onRecreate={vi.fn()}
      />,
    );
    expect(screen.getByText('Параметры изменились. Используй новую карточку.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Отправить заявку' })).not.toBeInTheDocument();
    expect(confirm).not.toHaveBeenCalled();
  });
});
function close() {
  return userEvent.click(screen.getByRole('button', { name: 'Закрыть панель' }));
}

describe('Рабочее пространство стажёра', () => {
  it('opens the route, an accessible source and completes a self-reported task', async () => {
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /Изучить принципы разработки/ }));
    expect(screen.getByRole('dialog')).toHaveTextContent('1 неделя');
    await user.click(screen.getByRole('button', { name: 'Отметить выполненным' }));
    await waitFor(() => expect(mock.complete).toHaveBeenCalledWith('engineering'));
    await close();
    await user.click(screen.getByRole('button', { name: 'База знаний' }));
    await user.type(
      screen.getByRole('textbox', { name: 'Поиск по источникам' }),
      'получить доступ',
    );
    await user.click(
      screen.getByRole('button', { name: /Как получить доступ к пространству команды/ }),
    );
    await screen.findByText('Тестовая инструкция. Не регламент Авито.');
    expect(mock.source).toHaveBeenCalledWith('access-policy');
    await close();
    await user.click(screen.getByRole('tab', { name: '2 неделя' }));
    await user.click(screen.getByRole('button', { name: /Изучить руководство команды/ }));
    expect(screen.getByRole('dialog')).toHaveTextContent('Сначала: получить доступ');
  });

  it('prepares, confirms and restores a corporate operation', async () => {
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /Получить доступ к командной базе/ }));
    await user.click(screen.getByRole('button', { name: 'Подготовить заявку' }));
    await screen.findByRole('button', { name: 'Отправить заявку' });
    expect(mock.confirm).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Отправить заявку' }));
    await screen.findByText('Отправлено · REQ-123456');
    await user.click(screen.getAllByRole('button', { name: /Мои заявки/ })[0]);
    expect(screen.getByRole('dialog')).toHaveTextContent('Ожидает согласования');
    await user.click(screen.getByRole('button', { name: 'Обновить' }));
    await waitFor(() => expect(mock.refresh).toHaveBeenCalled());
    await close();
    await user.click(screen.getByRole('button', { name: 'О прототипе' }));
    expect(screen.getByRole('dialog')).toHaveTextContent('PostgreSQL');
    await close();
    await user.click(screen.getByRole('button', { name: 'Открыть диагностику' }));
    await screen.findByText(/buddy-grounded-v2/);
  });

  it('keeps input on model failure and retries with the same message id', async () => {
    await start();
    const user = userEvent.setup();
    mock.message.mockRejectedValueOnce(new ApiError('unavailable', 'Модель не ответила', 503));
    const input = screen.getByRole('textbox', { name: 'Сообщение AI Buddy' });
    await user.type(input, 'Что означает ревью?');
    await user.keyboard('{Shift>}{Enter}{/Shift}');
    expect(input).toHaveValue('Что означает ревью?\n');
    await user.keyboard('{Enter}');
    await screen.findByRole('alert');
    expect(input).toHaveValue('Что означает ревью?');
    const firstID = mock.message.mock.calls[0][0];
    await user.click(screen.getByRole('button', { name: 'Повторить вопрос' }));
    await screen.findByText('Проверь параметры перед отправкой.');
    expect(mock.message.mock.calls[1][0]).toBe(firstID);
    expect(input).toHaveValue('');
    await user.click(
      screen.getByRole('button', { name: /Как получить доступ к пространству команды/ }),
    );
    await screen.findByText('Тестовая инструкция. Не регламент Авито.');
  });

  it('stays usable while a model is answering', async () => {
    await start();
    const user = userEvent.setup();
    let resolve!: (value: Awaited<ReturnType<typeof api.message>>) => void;
    mock.message.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    await user.click(screen.getByRole('button', { name: 'Что делать дальше?' }));
    expect(screen.getByRole('status')).toHaveTextContent('Проверяю материалы');
    await user.click(screen.getByRole('button', { name: 'База знаний' }));
    expect(screen.getByRole('dialog')).toHaveTextContent('Только материалы');
    await close();
    await act(async () =>
      resolve({
        id: 'answer',
        role: 'assistant',
        text: 'Готово',
        created_at: new Date().toISOString(),
        source_ids: [],
      }),
    );
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument());
  });

  it('opens a private question form and allows a profile change and sign-out', async () => {
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Подготовить вопрос' }));
    await screen.findByRole('textbox', { name: 'Что передать' });
    expect(screen.getByRole('textbox', { name: 'Что передать' })).toHaveValue('');
    expect(mock.prepareRequest).not.toHaveBeenCalled();
    await close();
    await user.click(screen.getByRole('button', { name: /Антон Соколов.*Backend-стажёр/ }));
    state.profile = profiles[1];
    await user.click(screen.getByRole('button', { name: /Ирина Белова/ }));
    await screen.findByRole('heading', { name: 'Привет, Ирина.' });
    await user.click(screen.getByRole('button', { name: /Ирина Белова.*Backend-стажёр/ }));
    await user.click(screen.getByRole('button', { name: 'Выйти из тестового профиля' }));
    await screen.findByRole('heading', { name: 'Выбери тестовый профиль' });
  });

  it('makes simulated ownership events explicit and token-protected', async () => {
    state.operations = [operation()];
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Пульт демонстрации' }));
    expect(screen.getByRole('button', { name: 'Согласовать' })).toBeDisabled();
    await user.type(screen.getByLabelText('Токен оператора из конфигурации запуска'), 'owner');
    mock.event.mockRejectedValueOnce(new ApiError('forbidden', 'Токен оператора неверен', 403));
    await user.click(screen.getByRole('button', { name: 'Согласовать' }));
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent(
      'Токен оператора неверен',
    );
    await user.click(screen.getByRole('button', { name: 'Согласовать' }));
    await waitFor(() =>
      expect(mock.event).toHaveBeenCalledWith('owner', 'prepared-action-1', 'approved'),
    );
    await user.click(screen.getByRole('button', { name: 'Выдать права' }));
    await screen.findByText('Доступ выдан');
    await user.click(screen.getByRole('button', { name: 'Показать счётчики' }));
    await screen.findByText(/action_completed/);
    await user.click(screen.getByRole('button', { name: 'Недоступность' }));
    await waitFor(() => expect(mock.event).toHaveBeenCalledWith('owner', '', 'unavailable'));
  });

  it('continues from reading the guide to a separately confirmed first task', async () => {
    state.documents.push({
      ...fullSource,
      id: 'catalog-guide',
      scope: 'space',
      title: 'Руководство команды',
    });
    state.tasks = state.tasks.map((task) =>
      task.id === 'read-guide' ? { ...task, status: 'available' } : task,
    );
    mock.source.mockResolvedValue({
      ...fullSource,
      id: 'catalog-guide',
      body: 'Тестовое руководство команды',
    });
    mock.complete.mockImplementation(async () => {
      state.tasks = state.tasks.map((task) =>
        task.id === 'read-guide'
          ? { ...task, status: 'completed' }
          : task.id === 'first-issue'
            ? { ...task, status: 'available' }
            : task,
      );
      return state;
    });
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Открыть руководство' }));
    await screen.findByText('Тестовое руководство команды');
    await user.click(screen.getByRole('button', { name: 'Отметить руководство изученным' }));
    await screen.findByText('Изучение отмечено в маршруте');
    expect(mock.confirm).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Подготовить первую задачу' }));
    await screen.findByRole('region', { name: 'Назначение задачи' });
    expect(mock.prepare).toHaveBeenCalledWith('issue', expect.any(String));
    expect(mock.confirm).not.toHaveBeenCalled();
  });

  it('provides all workspace sections through the mobile menu and arrow-key tabs', async () => {
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: '1 неделя' }));
    await user.keyboard('{ArrowRight}');
    expect(screen.getByRole('tab', { name: '2 неделя' })).toHaveFocus();
    await user.keyboard('{ArrowLeft}');
    expect(screen.getByRole('tab', { name: '1 неделя' })).toHaveFocus();
    for (const label of [
      'База знаний',
      'Вопрос наставнику',
      'Сменить профиль',
      'Пульт демонстрации',
      'Диагностика',
    ]) {
      await user.click(screen.getByRole('button', { name: 'Рабочие разделы' }));
      await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: label }));
      expect(screen.getByRole('dialog')).toBeVisible();
      await close();
    }
  });

  it('shows unavailable-service and fixture states without claiming a real model', async () => {
    state.service_fresh = false;
    state.model = 'fixture';
    state.messages = [
      {
        id: 'fixture-answer',
        role: 'assistant',
        text: 'Тестовый текст',
        created_at: new Date().toISOString(),
        source_ids: [],
        kind: 'fixture',
      },
    ];
    await start();
    expect(screen.getByText('Тестовые ответы · без LLM')).toBeVisible();
    expect(screen.getByText('Тестовый ответ')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Проверить' }));
    await waitFor(() => expect(mock.refresh).toHaveBeenCalled());
    await userEvent.click(screen.getAllByRole('button', { name: 'Мои заявки' })[0]);
    expect(within(screen.getByRole('dialog')).getByRole('alert')).toHaveTextContent(
      'Права на закрытые материалы',
    );
  });
});

describe('Вход и восстановление', () => {
  it('asks for a demo profile when no session exists', async () => {
    mock.state.mockRejectedValueOnce(new ApiError('unauthorized', 'Нужен вход', 401));
    render(<App />);
    await screen.findByRole('heading', { name: 'Выбери тестовый профиль' });
    await userEvent.click(screen.getByRole('button', { name: /Антон Соколов/ }));
    await screen.findByRole('heading', { name: 'Привет, Антон.' });
    expect(mock.login).toHaveBeenCalledWith('anton');
  });

  it('allows retrying a failed initial load and reports login errors', async () => {
    mock.bootstrap.mockRejectedValueOnce(new Error('Сервер недоступен'));
    mock.state.mockRejectedValueOnce(new Error('Сервер недоступен'));
    render(<App />);
    await screen.findByRole('alert');
    await userEvent.click(screen.getByRole('button', { name: 'Повторить загрузку' }));
    await screen.findByRole('heading', { name: 'Привет, Антон.' });
    await userEvent.click(screen.getByRole('button', { name: /Антон Соколов.*Backend-стажёр/ }));
    mock.login.mockRejectedValueOnce(new Error('Не удалось войти'));
    await userEvent.click(screen.getByRole('button', { name: /Ирина Белова/ }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось войти');
  });

  it('closes panels with Escape and keeps an explicit action error', async () => {
    await start();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /Мой маршрут.*1\/12/ }));
    fireEvent(screen.getByRole('dialog'), new Event('cancel', { bubbles: true, cancelable: true }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    mock.message.mockRejectedValueOnce(new Error('Сеть недоступна'));
    await user.click(screen.getByRole('button', { name: 'Не открывается вики' }));
    await screen.findByRole('alert');
    await user.click(screen.getByRole('button', { name: 'Скрыть ошибку' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
