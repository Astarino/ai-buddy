import corpus from '../../../internal/buddy/corpus.json';
import type { Action, Operation, Workspace } from '../types';

export function workspace(): Workspace {
  return {
    profile: { ...corpus.profiles[0] },
    messages: [],
    actions: [],
    operations: [],
    draft: '',
    tasks: corpus.tasks.map((task) => ({
      ...task,
      blocker: task.requires[0],
      status:
        task.id === 'meet-team' ? 'completed' : task.requires.length > 0 ? 'blocked' : 'available',
    })),
    documents: corpus.documents
      .filter(
        (document) =>
          document.scope !== 'space' && document.scope !== 'mentor' && document.team !== 'delivery',
      )
      .map((document) => ({ ...document, body: '' })),
    next_task_id: 'space-access',
    service_fresh: true,
    model: 'ollama:qwen3.5:4b',
    demo: true,
    requests: [],
  };
}
export const profiles = corpus.profiles;
export const fullSource = corpus.documents.find((document) => document.id === 'access-policy')!;
export function action(kind: 'access' | 'issue' = 'access'): Action {
  return {
    id: 'prepared-action-1',
    kind,
    resource: kind === 'access' ? 'CATALOG' : 'CAT-101',
    level: kind === 'access' ? 'Чтение' : 'Назначить на себя',
    reason: 'Изучить руководство команды',
    status: 'ready',
    expires_at: '2099-01-01T00:00:00Z',
  };
}
export function operation(kind: 'access' | 'issue' = 'access'): Operation {
  return {
    id: kind === 'access' ? 'REQ-123456' : 'CAT-101',
    key: 'prepared-action-1',
    user_id: 'anton',
    kind,
    resource: kind === 'access' ? 'CATALOG' : 'CAT-101',
    level: 'Чтение',
    reason: 'Изучить руководство команды',
    status: kind === 'access' ? 'pending' : 'assigned',
    note: 'Ожидает владельца пространства. Срок не установлен.',
    updated_at: '2026-11-11T12:00:00Z',
  };
}
