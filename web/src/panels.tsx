import { useEffect, useState } from 'react';
import { api } from './api';
import { Icon, OperationCard, Status } from './components';
import type { Source, Task, Workspace } from './types';

export function SourcePanel({ id }: { id: string }) {
  const [source, setSource] = useState<Source | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    setSource(null);
    setError('');
    api
      .source(id)
      .then((value) => {
        if (active) setSource(value);
      })
      .catch((reason: Error) => {
        if (active) setError(reason.message);
      });
    return () => {
      active = false;
    };
  }, [id]);
  if (error)
    return (
      <p className="inline-error" role="alert">
        {error}
      </p>
    );
  if (!source)
    return (
      <p className="loading-state" role="status">
        Открываем источник…
      </p>
    );
  return (
    <article className="source-content">
      <div className={`provenance ${source.synthetic ? 'synthetic' : ''}`}>
        <Icon name="info" size={17} />
        <span>
          {source.synthetic
            ? 'Тестовая инструкция. Не регламент Авито.'
            : 'Публичный материал. Проверяй актуальность для команды.'}
        </span>
      </div>
      <dl className="source-metadata">
        <div>
          <dt>Происхождение</dt>
          <dd>{source.origin}</dd>
        </div>
        <div>
          <dt>Версия</dt>
          <dd>
            {source.version.slice(0, 16)} · снимок{' '}
            {new Date(
              source.updated_at.includes('T') ? source.updated_at : `${source.updated_at}T00:00:00`,
            ).toLocaleDateString('ru-RU')}
          </dd>
        </div>
        <div>
          <dt>Область</dt>
          <dd>
            {source.team ? 'Твоя команда' : 'Общий материал'}
            {source.scope === 'space' && ' · по выданному доступу'}
          </dd>
        </div>
      </dl>
      <div className="source-text">
        {source.body.split('\n').map((paragraph, index) => (
          <p key={index}>{paragraph}</p>
        ))}
      </div>
      {source.url && (
        <a className="text-link" href={source.url} target="_blank" rel="noreferrer">
          Открыть оригинал
          <Icon name="external" size={16} />
        </a>
      )}
    </article>
  );
}

export function SourcesPanel({
  sources,
  onSource,
}: {
  sources: Source[];
  onSource: (source: Source) => void;
}) {
  const [search, setSearch] = useState('');
  const filtered = sources.filter((source) =>
    source.title.toLocaleLowerCase('ru-RU').includes(search.toLocaleLowerCase('ru-RU')),
  );
  return (
    <>
      <p className="panel-lead">
        Только материалы, доступные твоей роли и команде. Закрытые документы появятся после выдачи
        прав.
      </p>
      <label className="source-search">
        <Icon name="search" />
        <input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Название материала"
          aria-label="Поиск по источникам"
        />
      </label>
      <div className="source-list">
        {filtered.map((source) => (
          <button key={source.id} onClick={() => onSource(source)}>
            <span className="resource-icon">
              <Icon name="book" />
            </span>
            <span>
              <strong>{source.title}</strong>
              <span>
                {source.synthetic ? 'Тестовый материал' : 'Публичный источник'} · v{source.version}
              </span>
            </span>
            <Icon name="chevron" size={16} />
          </button>
        ))}
      </div>
      {filtered.length === 0 && (
        <p className="empty-state">
          Такого материала среди доступных нет. Попробуй другое название.
        </p>
      )}
    </>
  );
}

export function TaskPanel({
  task,
  workspace,
  busy,
  onComplete,
  onAction,
  onSource,
}: {
  task: Task;
  workspace: Workspace;
  busy: boolean;
  onComplete: () => void;
  onAction: (kind: 'access' | 'issue') => void;
  onSource: (id: string) => void;
}) {
  const blocker = workspace.tasks.find((item) => item.id === task.blocker);
  const hasSource = workspace.documents.some((source) => source.id === task.document_id);
  return (
    <>
      <Status value={task.status} />
      <p className="panel-description">{task.description}</p>
      <dl className="source-metadata">
        <div>
          <dt>Когда</dt>
          <dd>{task.period}</dd>
        </div>
        <div>
          <dt>Участник</dt>
          <dd>{task.owner}</dd>
        </div>
        <div>
          <dt>Основание</dt>
          <dd>
            <a href={task.source_url} target="_blank" rel="noreferrer">
              {task.source}
              <Icon name="external" size={14} />
            </a>
          </dd>
        </div>
      </dl>
      {blocker && (
        <p className="dependency-note">
          <Icon name="lock" size={18} />
          <span>Сначала: {blocker.title.toLocaleLowerCase('ru-RU')}.</span>
        </p>
      )}
      {hasSource && (
        <button className="source-open-button" onClick={() => onSource(task.document_id)}>
          <Icon name="book" />
          <span>Открыть материал</span>
          <Icon name="chevron" size={16} />
        </button>
      )}
      {task.status === 'available' && task.completion === 'access' && (
        <button className="primary-button" onClick={() => onAction('access')} disabled={busy}>
          Подготовить заявку
          <Icon name="chevron" size={16} />
        </button>
      )}
      {task.status === 'available' && task.completion === 'issue' && (
        <button className="primary-button" onClick={() => onAction('issue')} disabled={busy}>
          Подготовить назначение
          <Icon name="chevron" size={16} />
        </button>
      )}
      {task.status === 'available' && task.completion === 'self' && (
        <>
          <button className="primary-button" disabled={busy} onClick={onComplete}>
            Отметить выполненным
            <Icon name="check" size={16} />
          </button>
          <p className="fine-print">
            Это твоя отметка об изучении. Buddy не проверяет знания автоматически.
          </p>
        </>
      )}
      {(task.status === 'waiting' || task.status === 'in_progress') && (
        <p className="fine-print">
          Результат подтвердит корпоративный сервис. Состояние можно обновить в «Моих заявках».
        </p>
      )}
    </>
  );
}

export function RequestsPanel({
  workspace,
  busy,
  onRefresh,
}: {
  workspace: Workspace;
  busy: boolean;
  onRefresh: () => void;
}) {
  return (
    <>
      <div className="panel-toolbar">
        <p>Фактические результаты сервисов</p>
        <button className="secondary-button compact" onClick={onRefresh} disabled={busy}>
          <Icon name="refresh" size={16} />
          {busy ? 'Проверяем…' : 'Обновить'}
        </button>
      </div>
      {!workspace.service_fresh && (
        <p className="inline-error" role="alert">
          Сервис недоступен. Права на закрытые материалы временно не подтверждены.
        </p>
      )}
      {workspace.operations.map((operation) => (
        <OperationCard key={operation.key} operation={operation} />
      ))}
      {workspace.operations.length === 0 && (
        <div className="empty-state">
          <Icon name="route" size={32} />
          <h3>Пока нет отправленных заявок</h3>
          <p>После подтверждения действия здесь появятся его номер и состояние.</p>
        </div>
      )}
      <p className="fine-print">
        «Согласовано» и «доступ выдан» — разные этапы. В тестовом контуре события воспроизводит
        оператор демонстрации.
      </p>
    </>
  );
}

export function DemoPanel({
  workspace,
  busy,
  onEvent,
}: {
  workspace: Workspace;
  busy: boolean;
  onEvent: (token: string, key: string, status: string) => Promise<void>;
}) {
  const [token, setToken] = useState('');
  const [counts, setCounts] = useState<Record<string, number> | null>(null);
  const [error, setError] = useState('');
  async function metrics() {
    try {
      setCounts(await api.metrics(token));
      setError('');
    } catch (reason) {
      setError((reason as Error).message);
    }
  }
  return (
    <>
      <div className="provenance">
        <Icon name="info" />
        <span>
          Пульт оператора имитации. Эти события не являются действиями стажёра и не меняют настоящие
          системы.
        </span>
      </div>
      <label className="field-label" htmlFor="operator-token">
        Токен оператора из конфигурации запуска
      </label>
      <input
        id="operator-token"
        type="password"
        autoComplete="off"
        className="text-input"
        value={token}
        onChange={(event) => setToken(event.target.value)}
        placeholder="DEMO_OPERATOR_TOKEN"
      />
      <p className="fine-print">Токен хранится только в памяти этой панели.</p>
      <h3 className="panel-section-title">Событие владельца ресурса</h3>
      {workspace.operations.length === 0 && (
        <p className="panel-lead">Сначала отправь заявку из диалога.</p>
      )}
      {workspace.operations.map((operation) => (
        <section className="sim-operation" key={operation.key}>
          <OperationCard operation={operation} />
          <div className="button-row">
            {(
              {
                pending: ['approved', 'rejected'],
                approved: ['granted', 'rejected'],
                granted: ['revoked'],
                assigned: ['done'],
              }[operation.status] ?? []
            ).map((status) => (
              <button
                key={status}
                className="secondary-button compact"
                disabled={busy || !token}
                onClick={() => void onEvent(token, operation.key, status)}
              >
                {
                  {
                    approved: 'Согласовать',
                    granted: 'Выдать права',
                    rejected: 'Отклонить',
                    revoked: 'Отозвать права',
                    done: 'Завершить задачу',
                  }[status]
                }
              </button>
            ))}
          </div>
        </section>
      ))}
      <h3 className="panel-section-title">Сбой сервиса</h3>
      <p className="panel-lead">
        Потерянный ответ сохраняет операцию. Повтор сверяется с тем же ключом и не создаёт дубль.
      </p>
      <div className="button-row">
        {[
          ['normal', 'Обычный режим'],
          ['unavailable', 'Недоступность'],
          ['lost_response', 'Потеря ответа'],
        ].map(([status, label]) => (
          <button
            className="secondary-button compact"
            key={status}
            disabled={busy || !token}
            onClick={() => void onEvent(token, '', status)}
          >
            {label}
          </button>
        ))}
      </div>
      <h3 className="panel-section-title">Технические события</h3>
      <button className="secondary-button" disabled={!token} onClick={() => void metrics()}>
        Показать счётчики
      </button>
      {counts && <pre className="diagnostics">{JSON.stringify(counts, null, 2)}</pre>}
      {error && (
        <p className="inline-error" role="alert">
          {error}
        </p>
      )}
    </>
  );
}

export function DiagnosticsPanel() {
  const [result, setResult] = useState<Record<string, unknown> | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    api
      .diagnostics()
      .then((value) => {
        if (active) setResult(value);
      })
      .catch((reason: Error) => {
        if (active) setError(reason.message);
      });
    return () => {
      active = false;
    };
  }, []);
  return (
    <>
      <p className="panel-lead">
        Безопасная диагностика текущей сессии. Промпты, ключи и тексты документов сюда не
        записываются.
      </p>
      {error ? (
        <p className="inline-error" role="alert">
          {error}
        </p>
      ) : result ? (
        <pre className="diagnostics">{JSON.stringify(result, null, 2)}</pre>
      ) : (
        <p role="status">Загружаем события…</p>
      )}
    </>
  );
}
