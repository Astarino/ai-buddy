import { createRef } from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Conversation } from './conversation';
import { action, workspace } from './test/fixtures';

describe('Продолжение диалога', () => {
  it('restores an unlinked saved card and confirms the same action', async () => {
    const state = workspace();
    state.actions = [action()];
    const onConfirm = vi.fn();
    const onPrepare = vi.fn();
    const props = {
      workspace: state,
      busy: '',
      sending: false,
      endRef: createRef<HTMLDivElement>(),
      onSource: vi.fn(),
      onConfirm,
      onPrepare,
      onInbox: vi.fn(),
      onSend: vi.fn(),
    };
    const { rerender } = render(<Conversation {...props} />);
    expect(onConfirm).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Отправить заявку' }));
    expect(onConfirm).toHaveBeenCalledWith(state.actions[0].id);
    state.actions[0].expires_at = '2000-01-01T00:00:00Z';
    rerender(<Conversation {...props} workspace={{ ...state }} />);
    await userEvent.click(screen.getByRole('button', { name: 'Подготовить заново' }));
    expect(onPrepare).toHaveBeenCalledWith('access');
  });

  it('opens the precise prepared question and shows actual backend progress', async () => {
    const state = workspace();
    state.messages = [
      {
        id: 'answer',
        role: 'assistant',
        text: 'Проверь вопрос',
        created_at: '2026-11-16T09:00:00Z',
        source_ids: ['access-policy'],
        request_id: 'help-exact-card',
        search_mode: 'bm25_fallback',
      },
    ];
    state.progress = ['Проверил маршрут и операции', 'Проверил доступные материалы'];
    const onInbox = vi.fn();
    const onSource = vi.fn();
    render(
      <Conversation
        workspace={state}
        busy=""
        sending
        endRef={createRef<HTMLDivElement>()}
        onSource={onSource}
        onConfirm={vi.fn()}
        onPrepare={vi.fn()}
        onInbox={onInbox}
        onSend={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Проверить вопрос и получателя' }));
    expect(onInbox).toHaveBeenCalledWith('help-exact-card');
    await userEvent.click(screen.getByRole('button', { name: /Как получить доступ/ }));
    expect(onSource).toHaveBeenCalledWith('access-policy');
    await userEvent.click(screen.getByText('Как найден ответ'));
    expect(screen.getByText(/Смысловой поиск временно недоступен/)).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('Проверил доступные материалы');
    await userEvent.click(screen.getByText('Выполненные шаги'));
    expect(screen.getByText('Проверил маршрут и операции')).toBeVisible();
  });
});
