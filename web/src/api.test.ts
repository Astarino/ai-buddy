import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { api, ApiError, request } from './api';
import { action, fullSource, operation, profiles, workspace } from './test/fixtures';

const fetchMock = vi.fn<typeof fetch>();
beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (input) => {
    const path = String(input);
    const body = path.endsWith('/bootstrap')
      ? { profiles, model: 'fixture', demo: true }
      : path.endsWith('/state') ||
          path.endsWith('/refresh') ||
          path.includes('/demo/events') ||
          path.includes('/tasks/')
        ? workspace()
        : path.includes('/documents/')
          ? fullSource
          : path.endsWith('/actions')
            ? action()
            : path.includes('/confirm')
              ? operation()
              : path.endsWith('/draft')
                ? { draft: 'Черновик' }
                : { ok: true };
    return Response.json(body);
  });
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it('uses same-origin credentials and the documented API verbs, payloads and context', async () => {
  await api.login('anton');
  expect(fetchMock).toHaveBeenLastCalledWith(
    '/api/session',
    expect.objectContaining({
      method: 'POST',
      body: '{"profile_id":"anton"}',
      credentials: 'same-origin',
    }),
  );
  await api.bootstrap();
  await api.state();
  await api.message('message-001', 'Вопрос');
  expect(fetchMock).toHaveBeenLastCalledWith(
    '/api/messages',
    expect.objectContaining({
      body: '{"id":"message-001","text":"Вопрос"}',
      headers: expect.objectContaining({ 'X-Buddy-Context': 'anton' }),
    }),
  );
  await api.prepare('access', 'Изучить руководство');
  await api.confirm('action/id');
  expect(fetchMock).toHaveBeenLastCalledWith(
    '/api/actions/action%2Fid/confirm',
    expect.objectContaining({ method: 'POST', body: '{}' }),
  );
  await api.complete('read-guide');
  await api.refresh();
  await api.source('source/id');
  expect(fetchMock).toHaveBeenLastCalledWith('/api/documents/source%2Fid', expect.anything());
  await api.draft();
  await api.draft('Вопрос наставнику');
  await api.event('operator-token', 'key', 'approved');
  expect(fetchMock).toHaveBeenLastCalledWith(
    '/api/demo/events',
    expect.objectContaining({
      headers: expect.objectContaining({ 'X-Demo-Token': 'operator-token' }),
    }),
  );
  await api.diagnostics();
  await api.metrics('owner');
  await api.logout();
  expect(fetchMock).toHaveBeenLastCalledWith(
    '/api/session',
    expect.objectContaining({ method: 'DELETE' }),
  );
});

it('preserves server error codes and makes malformed and network responses explicit', async () => {
  fetchMock.mockResolvedValueOnce(
    Response.json({ code: 'forbidden', message: 'Недостаточно прав' }, { status: 403 }),
  );
  await expect(request('/test')).rejects.toMatchObject({
    code: 'forbidden',
    message: 'Недостаточно прав',
    status: 403,
  });
  fetchMock.mockResolvedValueOnce(Response.json({}, { status: 500 }));
  await expect(request('/test')).rejects.toMatchObject({ code: 'request_failed' });
  fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'));
  await expect(request('/test')).rejects.toMatchObject({ code: 'network', status: 0 });
  fetchMock.mockResolvedValueOnce(new Response('invalid json'));
  await expect(request('/test')).rejects.toBeInstanceOf(ApiError);
});

it('cancels requests on timeout and respects an external abort signal', async () => {
  vi.useFakeTimers();
  fetchMock.mockImplementation(
    (_input, options) =>
      new Promise((_resolve, reject) => {
        options?.signal?.addEventListener('abort', () =>
          reject(new DOMException('Stopped', 'AbortError')),
        );
      }),
  );
  const pending = request('/slow');
  const assertion = expect(pending).rejects.toMatchObject({ code: 'timeout' });
  await vi.advanceTimersByTimeAsync(95_001);
  await assertion;
  const controller = new AbortController();
  const stopped = request('/slow', { signal: controller.signal });
  const stoppedAssertion = expect(stopped).rejects.toMatchObject({ code: 'timeout' });
  controller.abort();
  await stoppedAssertion;
});
