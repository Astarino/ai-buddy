import type {
  Action,
  Bootstrap,
  HelpRequest,
  Message,
  Operation,
  PublicSearch,
  Source,
  Workspace,
} from './types';

let contextID = '';
let sessionEpoch = 0;

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
  const timeout = setTimeout(() => controller.abort(), 125_000);
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
    throw new ApiError('network', 'Нет связи с сервером. Попробуй ещё раз.', 0);
  } finally {
    clearTimeout(timeout);
  }
}

const post = <T>(path: string, body: unknown, headers?: Record<string, string>) =>
  request<T>(path, { method: 'POST', body: JSON.stringify(body), headers });

export const api = {
  bootstrap: (signal?: AbortSignal) => request<Bootstrap>('/bootstrap', { signal }),
  state: async (signal?: AbortSignal) => {
    const epoch = sessionEpoch;
    const state = await request<Workspace>('/state', { signal });
    if (epoch === sessionEpoch) contextID = state.profile.id;
    return state;
  },
  login: (profile: string) => {
    sessionEpoch++;
    contextID = '';
    return post<{ ok: boolean }>('/session', { profile_id: profile });
  },
  logout: () => {
    sessionEpoch++;
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
  search: (id: string, topic: string, query: string) =>
    post<PublicSearch>('/search', { id, topic, query }),
  prepareRequest: (id: string, kind: string, subject: string, body: string) =>
    post<HelpRequest>('/requests', { id, kind, subject, body }),
  changeRequest: (id: string, command: string, version: number, recipient = '') =>
    post<HelpRequest>(`/requests/${encodeURIComponent(id)}/change`, {
      command,
      version,
      ...(recipient ? { recipient } : {}),
    }),
  replyRequest: (id: string, replyID: string, text: string, version: number) =>
    post<HelpRequest>(`/requests/${encodeURIComponent(id)}/replies`, {
      id: replyID,
      text,
      version,
    }),
  decideRequest: (id: string, status: string, version: number) =>
    post<HelpRequest>(`/requests/${encodeURIComponent(id)}/decision`, { status, version }),
  event: (token: string, key: string, status: string) =>
    post<Workspace>('/demo/events', { key, status }, { 'X-Demo-Token': token }),
  diagnostics: () => request<Record<string, unknown>>('/diagnostics'),
  metrics: (token: string) =>
    request<Record<string, number>>('/demo/metrics', { headers: { 'X-Demo-Token': token } }),
};
