import { useRef, useState } from 'react';
import { api } from './api';
import { Avatar, Icon, Status } from './components';
import type { HelpRequest, PublicSearch, Workspace } from './types';

const labels: Record<string, string> = {
  draft: 'Черновик',
  waiting: 'Ждёт ответа',
  answered: 'Ответ получен',
  resolved: 'Закрыт',
};

function stamp(value: string) {
  return new Date(value).toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function PublicSearchPanel({
  onRefresh,
  onAsk,
  onSource,
}: {
  onRefresh: () => Promise<void>;
  onAsk: (question: string) => void;
  onSource: (id: string) => void;
}) {
  const [topic, setTopic] = useState('go');
  const [query, setQuery] = useState('');
  const [result, setResult] = useState<PublicSearch | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const request = useRef({ params: '', id: '' });

  async function search() {
    if (busy || query.trim().length < 3) return;
    const params = JSON.stringify([topic, query]);
    if (request.current.params !== params) request.current = { params, id: crypto.randomUUID() };
    setBusy(true);
    setError('');
    setResult(null);
    try {
      const found = await api.search(request.current.id, topic, query);
      setResult(found);
      await onRefresh();
      request.current = { params: '', id: '' };
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="work-form">
      <p className="panel-lead">
        Поиск в официальной документации Go и Git. Получаем свежие фрагменты по сети; твой вопрос
        остаётся в AI Buddy.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void search();
        }}
      >
        <label>
          Инструмент
          <select value={topic} disabled={busy} onChange={(event) => setTopic(event.target.value)}>
            <option value="go">Go</option>
            <option value="git">Git</option>
          </select>
        </label>
        <label>
          С чем нужна помощь
          <textarea
            value={query}
            disabled={busy}
            maxLength={2000}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Команда, ошибка или рабочий вопрос"
            rows={4}
          />
        </label>
        <button className="primary-button" disabled={busy || query.trim().length < 3}>
          <Icon name="search" size={17} />
          {busy ? 'Ищем в документации…' : 'Найти'}
        </button>
      </form>
      {busy && (
        <p role="status" className="fine-print">
          Загружаем официальные источники…
        </p>
      )}
      {error && (
        <p className="inline-error" role="alert">
          {error}
        </p>
      )}
      {result && (
        <section className="search-results" aria-label="Результаты поиска">
          <p className="fine-print">Получено {stamp(result.fetched_at)}</p>
          {result.documents.length === 0 ? (
            <p>Подходящих фрагментов нет. Уточни вопрос или выбери другой инструмент.</p>
          ) : (
            <>
              <div className="source-list">
                {result.documents.map((doc) => (
                  <button key={doc.id} onClick={() => onSource(doc.id)}>
                    <Icon name="book" />
                    <span>
                      <strong>{doc.title}</strong>
                      <small>{new URL(doc.url!).hostname}</small>
                    </span>
                    <Icon name="chevron" />
                  </button>
                ))}
              </div>
              <button className="primary-button" onClick={() => onAsk(result.query)}>
                Разобрать с Buddy
                <Icon name="chat" size={17} />
              </button>
            </>
          )}
        </section>
      )}
    </div>
  );
}

export function HelpRequestsPanel({
  workspace,
  onRefresh,
  initialCompose = false,
  initialSelected = '',
}: {
  workspace: Workspace;
  onRefresh: () => Promise<void>;
  initialCompose?: boolean;
  initialSelected?: string;
}) {
  const [selected, setSelected] = useState(initialSelected);
  const [compose, setCompose] = useState(initialCompose);
  const [history, setHistory] = useState(false);
  const [kind, setKind] = useState('mentor');
  const [subject, setSubject] = useState('');
  const [body, setBody] = useState('');
  const [prepared, setPrepared] = useState<HelpRequest | null>(null);
  const [reply, setReply] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const createKey = useRef({ params: '', id: '' });
  const replyKey = useRef({ params: '', id: '' });
  const responsible = workspace.profile.audience === 'responsible';
  const requests = workspace.requests ?? [];
  const current = requests.find((item) => item.id === selected);
  const currentTarget = workspace.request_targets?.find(
    (target) => target.kind === (current?.kind === 'access' ? 'owner' : current?.kind),
  );

  async function act(action: () => Promise<unknown>, after?: () => void) {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      await action();
      await onRefresh();
      after?.();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setBusy(false);
    }
  }
  const feedback = error && (
    <p role="alert" className="inline-error">
      {error}
    </p>
  );

  if (compose)
    return (
      <div className="work-form">
        <button
          className="text-button"
          onClick={() => {
            setCompose(false);
            setError('');
          }}
        >
          ← Все запросы
        </button>
        {prepared ? (
          <>
            <div className="request-recipient">
              <Avatar name={prepared.assignee} />
              <div>
                <strong>{prepared.assignee}</strong>
                <span>Получатель</span>
              </div>
            </div>
            <h3>{prepared.subject}</h3>
            <p className="shared-text">{prepared.body}</p>
            <p className="fine-print">
              Передадим только этот текст. Личный диалог с Buddy остаётся у тебя.
            </p>
            {feedback}
            <div className="button-row">
              <button
                className="secondary-button"
                disabled={busy}
                onClick={() => setPrepared(null)}
              >
                Изменить
              </button>
              <button
                className="primary-button"
                disabled={busy}
                onClick={() =>
                  void act(
                    () => api.changeRequest(prepared.id, 'send', prepared.version),
                    () => {
                      setSelected(prepared.id);
                      setCompose(false);
                      setPrepared(null);
                      setSubject('');
                      setBody('');
                    },
                  )
                }
              >
                {busy ? 'Отправляем…' : 'Отправить вопрос'}
              </button>
            </div>
          </>
        ) : (
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const params = JSON.stringify([kind, subject, body]);
              if (createKey.current.params !== params)
                createKey.current = { params, id: crypto.randomUUID() };
              void act(async () =>
                setPrepared(await api.prepareRequest(createKey.current.id, kind, subject, body)),
              );
            }}
          >
            <label>
              Кому
              <select
                value={kind}
                onChange={(event) => setKind(event.target.value)}
                disabled={busy}
              >
                <option value="mentor">Наставник · {workspace.profile.mentor}</option>
                <option value="owner">Владелец вики</option>
              </select>
            </label>
            <label>
              Тема
              <input
                value={subject}
                onChange={(event) => setSubject(event.target.value)}
                maxLength={120}
                minLength={3}
                required
                disabled={busy}
                placeholder="Например, не запускаются тесты"
              />
            </label>
            <label>
              Что передать
              <textarea
                value={body}
                onChange={(event) => setBody(event.target.value)}
                maxLength={2000}
                minLength={3}
                required
                disabled={busy}
                rows={6}
                placeholder="Что делаешь, что уже проверил и где нужна помощь"
              />
            </label>
            {feedback}
            <button
              className="primary-button"
              disabled={busy || subject.trim().length < 3 || body.trim().length < 3}
            >
              Проверить перед отправкой
              <Icon name="chevron" size={16} />
            </button>
          </form>
        )}
      </div>
    );

  if (current)
    return (
      <article className="request-detail work-form">
        <button
          className="text-button"
          onClick={() => {
            setSelected('');
            setReply('');
            setError('');
          }}
        >
          ← Все запросы
        </button>
        <div className="request-recipient">
          <Avatar name={current.requester} />
          <div>
            <strong>{current.requester}</strong>
            <span>{stamp(current.created_at)}</span>
          </div>
          <span className={`request-status request-status-${current.status}`}>
            {labels[current.status]}
          </span>
        </div>
        <h3>{current.subject}</h3>
        <p className="fine-print">Ответственный: {current.assignee}</p>
        {current.requester_id === workspace.profile.id &&
          current.status !== 'resolved' &&
          currentTarget &&
          currentTarget.id !== current.assignee_id && (
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                void act(() =>
                  api.changeRequest(current.id, 'reassign', current.version, currentTarget.id),
                )
              }
            >
              Передать: {currentTarget.name}
            </button>
          )}
        <p className="shared-text">{current.body}</p>
        {current.operation_status && (
          <div className="request-operation">
            <span>Доступ к вики</span>
            <Status value={current.operation_status} />
          </div>
        )}
        <div className="request-replies" aria-label="Обсуждение запроса">
          {current.replies.map((message) => (
            <div className="request-reply" key={message.id}>
              <div>
                <strong>{message.author}</strong>
                <time dateTime={message.created_at}>{stamp(message.created_at)}</time>
              </div>
              <p>{message.text}</p>
            </div>
          ))}
        </div>
        {current.pending_decision && (
          <p className="fine-print" role="status">
            Проверяем результат операции. Повтор использует ту же заявку.
          </p>
        )}
        {feedback}
        {current.status === 'draft' && current.requester_id === workspace.profile.id && (
          <button
            className="primary-button"
            disabled={busy}
            onClick={() => void act(() => api.changeRequest(current.id, 'send', current.version))}
          >
            Отправить вопрос
          </button>
        )}
        {current.kind === 'access' && current.assignee_id === workspace.profile.id && (
          <div className="button-row">
            {(current.operation_status === 'pending' ||
              current.operation_status === 'approved') && (
              <button
                className="primary-button"
                disabled={busy}
                onClick={() =>
                  void act(() =>
                    api.decideRequest(
                      current.id,
                      current.operation_status === 'pending' ? 'approved' : 'granted',
                      current.version,
                    ),
                  )
                }
              >
                {current.operation_status === 'pending' ? 'Согласовать' : 'Выдать доступ'}
              </button>
            )}
            {(current.operation_status === 'pending' ||
              current.operation_status === 'approved') && (
              <button
                className="secondary-button"
                disabled={
                  busy ||
                  Boolean(current.pending_decision && current.pending_decision !== 'rejected')
                }
                onClick={() =>
                  void act(() => api.decideRequest(current.id, 'rejected', current.version))
                }
              >
                Отклонить
              </button>
            )}
            {current.operation_status === 'granted' && (
              <button
                className="secondary-button"
                disabled={busy}
                onClick={() =>
                  void act(() => api.decideRequest(current.id, 'revoked', current.version))
                }
              >
                Отозвать доступ
              </button>
            )}
          </div>
        )}
        {current.status !== 'draft' && current.status !== 'resolved' && (
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const params = JSON.stringify([current.id, reply]);
              if (replyKey.current.params !== params)
                replyKey.current = { params, id: crypto.randomUUID() };
              void act(
                () => api.replyRequest(current.id, replyKey.current.id, reply, current.version),
                () => setReply(''),
              );
            }}
          >
            <label>
              Ответ
              <textarea
                value={reply}
                onChange={(event) => setReply(event.target.value)}
                rows={4}
                maxLength={2000}
                disabled={busy}
              />
            </label>
            <div className="button-row">
              <button className="primary-button" disabled={busy || !reply.trim()}>
                {busy ? 'Отправляем…' : 'Ответить'}
              </button>
              {current.requester_id === workspace.profile.id && current.kind !== 'access' && (
                <button
                  type="button"
                  className="secondary-button"
                  disabled={busy}
                  onClick={() =>
                    void act(() => api.changeRequest(current.id, 'resolve', current.version))
                  }
                >
                  Проблема решена
                </button>
              )}
            </div>
          </form>
        )}
      </article>
    );

  const visible = requests.filter((item) =>
    history ? item.status === 'resolved' : item.status !== 'resolved',
  );
  const groups = new Map<string, HelpRequest[]>();
  for (const item of visible) {
    const id = responsible ? item.requester_id : item.assignee_id;
    groups.set(id, [...(groups.get(id) ?? []), item]);
  }
  return (
    <div className="request-inbox">
      <div className="inbox-toolbar">
        <div className="segmented-control" aria-label="Список запросов">
          <button className={!history ? 'selected' : ''} onClick={() => setHistory(false)}>
            Активные
          </button>
          <button className={history ? 'selected' : ''} onClick={() => setHistory(true)}>
            История
          </button>
        </div>
        <button
          className="icon-button"
          aria-label="Обновить запросы"
          disabled={busy}
          onClick={() => void act(onRefresh)}
        >
          <Icon name="refresh" size={18} />
        </button>
      </div>
      {!responsible && (
        <button className="primary-button" onClick={() => setCompose(true)}>
          Задать вопрос
          <Icon name="chat" size={17} />
        </button>
      )}
      {feedback}
      {visible.length === 0 && !workspace.service_fresh && (
        <p role="status">Не удалось обновить запросы. Повтори проверку чуть позже.</p>
      )}
      {visible.length === 0 && workspace.service_fresh && (
        <p className="empty-state">
          {history
            ? 'Закрытых запросов пока нет.'
            : responsible
              ? 'Пока никто не ждёт твоего ответа.'
              : 'Открытых вопросов пока нет.'}
        </p>
      )}
      {Array.from(groups, ([id, items]) => (
        <section
          className="request-group"
          key={id}
          aria-label={responsible ? items[0].requester : items[0].assignee}
        >
          <h3>
            {responsible ? items[0].requester : items[0].assignee}
            <span>{items.length}</span>
          </h3>
          {items.map((item) => (
            <button
              className="request-row"
              key={item.id}
              onClick={() => {
                setSelected(item.id);
                setError('');
              }}
            >
              <span>
                <strong>{item.subject}</strong>
                <small>
                  {item.operation_status
                    ? `Вики · ${item.operation_status === 'approved' ? 'согласовано, ждём выдачи' : item.operation_status === 'pending' ? 'ждёт согласования' : labels[item.status]}`
                    : labels[item.status]}
                </small>
              </span>
              <Icon name="chevron" size={17} />
            </button>
          ))}
        </section>
      ))}
    </div>
  );
}
