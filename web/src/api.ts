import type { Action, Bootstrap, Message, Operation, Source, Workspace } from './types';

let contextID = '';

export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 95_000);
  try {
    const response = await fetch(`/api${path}`, {
      ...options,
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(contextID ? { 'X-Buddy-Context': contextID } : {}),
        ...options.headers,
      },
      signal: options.signal
        ? AbortSignal.any([options.signal, controller.signal])
        : controller.signal,
    });
    const body = await response.json();
    if (!response.ok)
      throw new ApiError(
        body.code ?? 'request_failed',
        body.message ?? 'Не удалось выполнить запрос.',
        response.status,
      );
    return body as T;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    if (error instanceof DOMException && error.name === 'AbortError')
      throw new ApiError('timeout', 'Запрос остановлен. Обнови состояние перед повтором.', 0);
    throw new ApiError('network', 'Нет связи с сервером. Введённый текст сохранён в поле.', 0);
  } finally {
    clearTimeout(timeout);
  }
}

const post = <T>(path: string, body: unknown, headers?: Record<string, string>) =>
  request<T>(path, { method: 'POST', body: JSON.stringify(body), headers });

export const api = {
  bootstrap: (signal?: AbortSignal) => request<Bootstrap>('/bootstrap', { signal }),
  state: async (signal?: AbortSignal) => {
    const state = await request<Workspace>('/state', { signal });
    contextID = state.profile.id;
    return state;
  },
  login: (profile: string) => {
    contextID = '';
    return post<{ ok: boolean }>('/session', { profile_id: profile });
  },
  logout: () => {
    contextID = '';
    return request<{ ok: boolean }>('/session', { method: 'DELETE', body: '{}' });
  },
  message: (id: string, text: string, externalConsent = '') =>
    post<Message>('/messages', {
      id,
      text,
      ...(externalConsent ? { external_consent: externalConsent } : {}),
    }),
  prepare: (kind: 'access' | 'issue', reason: string) => post<Action>('/actions', { kind, reason }),
  confirm: (id: string) => post<Operation>(`/actions/${encodeURIComponent(id)}/confirm`, {}),
  complete: (id: string) => post<Workspace>(`/tasks/${encodeURIComponent(id)}/complete`, {}),
  refresh: () => post<Workspace>('/refresh', {}),
  source: (id: string) => request<Source>(`/documents/${encodeURIComponent(id)}`),
  draft: (question = '') => post<{ draft: string }>('/draft', { question }),
  event: (token: string, key: string, status: string) =>
    post<Workspace>('/demo/events', { key, status }, { 'X-Demo-Token': token }),
  diagnostics: () => request<Record<string, unknown>>('/diagnostics'),
  metrics: (token: string) =>
    request<Record<string, number>>('/demo/metrics', { headers: { 'X-Demo-Token': token } }),
};
