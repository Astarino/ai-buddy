import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from './api';
import { BuddyMark, Dialog, Icon, ProfilePicker, RoutePanel } from './components';
import {
  DemoPanel,
  DiagnosticsPanel,
  RequestsPanel,
  SourcePanel,
  SourcesPanel,
  TaskPanel,
} from './panels';
import { HelpRequestsPanel, PublicSearchPanel } from './collaboration';
import type { Bootstrap, Profile, Source, Workspace } from './types';
import { Conversation } from './conversation';
import { WorkspaceNavigation, WorkspaceOverview, type Panel } from './navigation';

export default function App() {
  const [bootstrap, setBootstrap] = useState<Bootstrap | null>(null);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [error, setError] = useState('');
  const [syncError, setSyncError] = useState(false);
  const [busy, setBusy] = useState('');
  const [panel, setPanel] = useState<Panel>(null);
  const [composer, setComposer] = useState('');
  const [sending, setSending] = useState(false);
  const [retry, setRetry] = useState<{ id: string; text: string } | null>(null);
  const [pendingExternal, setPendingExternal] = useState<{
    text: string;
    pending: { id: string; text: string } | null;
  } | null>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setLoading(true);
    setError('');
    Promise.allSettled([api.bootstrap(controller.signal), api.state(controller.signal)]).then(
      ([boot, state]) => {
        if (!active) return;
        if (boot.status === 'fulfilled') setBootstrap(boot.value);
        else setError((boot.reason as Error).message);
        if (state.status === 'fulfilled') setWorkspace(state.value);
        else if (!(state.reason instanceof ApiError && state.reason.status === 401))
          setError((state.reason as Error).message);
        setLoading(false);
      },
    );
    return () => {
      active = false;
      controller.abort();
    };
  }, [reload]);

  useEffect(() => {
    if (workspace?.messages.length || sending)
      endRef.current?.scrollIntoView({
        behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
        block: 'end',
      });
  }, [workspace?.messages.length, sending]);

  const ownerID = workspace?.profile.id;
  useEffect(() => {
    if (!ownerID) return;
    let active = true;
    const controller = new AbortController();
    const timer = setInterval(
      () => {
        if (document.hidden) return;
        void api
          .state(controller.signal)
          .then((value) => {
            if (active && value.profile.id === ownerID) {
              setWorkspace(value);
              setSyncError(false);
            }
          })
          .catch(() => {
            if (active && !controller.signal.aborted) setSyncError(true);
          });
      },
      sending ? 1500 : 15000,
    );
    return () => {
      active = false;
      controller.abort();
      clearInterval(timer);
    };
  }, [ownerID, sending]);

  async function login(profile: Profile) {
    setBusy('login');
    setError('');
    try {
      await api.login(profile.id);
      setWorkspace(await api.state());
      setPanel(null);
      setComposer('');
      setRetry(null);
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setBusy('');
    }
  }

  async function perform(key: string, action: () => Promise<unknown>, close = false) {
    if (busy) return;
    setBusy(key);
    setError('');
    try {
      await action();
      if (key !== 'logout') setWorkspace(await api.state());
      if (close) setPanel(null);
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setBusy('');
    }
  }

  async function send(text = composer, pending = retry, consentTo = '') {
    text = text.trim();
    if (!text || sending || !workspace) return;
    if (bootstrap?.external_model && !consentTo) {
      setPendingExternal({ text, pending });
      setPanel({ kind: 'external' });
      return;
    }
    const id = pending?.text === text ? pending.id : crypto.randomUUID();
    setSending(true);
    setError('');
    setRetry({ id, text });
    setComposer((draft) => (draft.trim() === text ? '' : draft));
    if (!workspace.messages.some((message) => message.id === id))
      setWorkspace({
        ...workspace,
        messages: [
          ...workspace.messages,
          { id, role: 'user', text, created_at: new Date().toISOString(), source_ids: [] },
        ],
      });
    try {
      if (consentTo) await api.message(id, text, consentTo);
      else await api.message(id, text);
      setWorkspace(await api.state());
      setRetry(null);
    } catch (reason) {
      setError((reason as Error).message);
      if (reason instanceof ApiError && reason.code === 'external_consent') {
        try {
          const current = await api.bootstrap();
          setBootstrap(current);
          setPendingExternal({ text, pending: { id, text } });
          setPanel({ kind: 'external' });
        } catch (refreshError) {
          setError((refreshError as Error).message);
        }
      }
    } finally {
      setSending(false);
      composerRef.current?.focus();
    }
  }

  function openSource(id: string, source?: Source) {
    setPanel({
      kind: 'source',
      id,
      title:
        source?.title ?? workspace?.documents.find((item) => item.id === id)?.title ?? 'Источник',
    });
  }
  function prepare(kind: 'access' | 'issue') {
    void perform(
      'prepare',
      async () => {
        const action = await api.prepare(
          kind,
          kind === 'access'
            ? 'Изучить руководство команды и подготовиться к первой задаче'
            : 'Первая учебная задача после изучения руководства команды',
        );
        const text =
          kind === 'access' ? 'Нужен доступ к командной вики' : 'Хочу взять первую задачу';
        await api.message(`action-${action.id}`, text);
      },
      true,
    );
  }

  const dismissError = () => setError('');
  const closePanel = () => setPanel(null);

  if (loading)
    return (
      <main className="launch-screen">
        <BuddyMark />
        <p role="status">Открываем рабочее пространство…</p>
      </main>
    );
  if (!workspace)
    return (
      <main className="login-screen">
        <div className="login-brand">
          <BuddyMark />
          <strong>AI Buddy</strong>
        </div>
        <section className="login-card">
          <span className="eyebrow">
            {bootstrap?.demo === false ? 'AI BUDDY' : 'ДЕМОНСТРАЦИОННЫЙ КОНТУР'}
          </span>
          <h1>
            Твой первый шаг
            <br />в новой команде
          </h1>
          <p>
            Рабочие вопросы, материалы и действия —<br className="desktop-break" /> в одном личном
            диалоге.
          </p>
          {bootstrap?.demo !== false && <h2>Выбери тестовый профиль</h2>}
          {bootstrap?.demo === false ? (
            <a className="primary-button" href="/api/login">
              Войти через компанию
            </a>
          ) : (
            bootstrap && (
              <ProfilePicker
                profiles={bootstrap.profiles}
                onSelect={(profile) => void login(profile)}
                busy={busy !== ''}
              />
            )
          )}
          {error && (
            <div className="inline-error" role="alert">
              {error}
              <button className="text-button" onClick={() => setReload((value) => value + 1)}>
                Повторить загрузку
              </button>
            </div>
          )}
          {bootstrap?.demo !== false && (
            <div className="login-note">
              <Icon name="shield" size={18} />
              <span>
                Профили и командные инструкции вымышлены.
                <br />
                Настоящие корпоративные данные не используются.
              </span>
            </div>
          )}
        </section>
        {bootstrap?.demo !== false && (
          <span className="login-caption">AI Buddy · прототип для Hack&amp;Change 2026</span>
        )}
      </main>
    );

  const profile = workspace.profile;
  const requestAttention = (workspace.requests ?? []).filter((item) =>
    item.requester_id === profile.id ? item.status === 'answered' : item.status === 'waiting',
  ).length;
  const pendingCount = workspace.operations.filter((operation) =>
    ['pending', 'approved', 'assigned'].includes(operation.status),
  ).length;
  const selectedTask =
    panel?.kind === 'task' ? workspace.tasks.find((task) => task.id === panel.id) : undefined;
  const guideTask = workspace.tasks.find((task) => task.id === 'read-guide');
  const panelNames: Record<string, string> = {
    sources: 'База знаний',
    requests: 'Мои заявки',
    mentor: 'Вопрос наставнику',
    demo: 'Пульт демонстрации',
    diagnostics: 'Диагностика',
    help: 'О рабочем пространстве',
    profiles: workspace.demo ? 'Тестовый профиль' : 'Учётная запись',
    external: 'Отправить вопрос внешней модели?',
    route: 'Мой маршрут',
    menu: 'Рабочее пространство',
    search: 'Найти в документации',
    inbox: profile.audience === 'responsible' ? 'Нужен мой ответ' : 'Вопросы команде',
  };
  const panelTitle =
    panel?.kind === 'source'
      ? panel.title
      : (selectedTask?.title ?? panelNames[panel?.kind ?? 'help'] ?? 'Шаг маршрута');

  return (
    <div className="app-shell">
      <WorkspaceNavigation
        workspace={workspace}
        syncError={syncError}
        busy={busy}
        sending={sending}
        pendingCount={pendingCount}
        requestAttention={requestAttention}
        onNavigate={setPanel}
        onRefresh={() =>
          void perform('refresh', async () => {
            setWorkspace(await api.state());
            setSyncError(false);
          })
        }
      />

      <main className="chat-main">
        <header className="chat-header">
          <div className="buddy-heading">
            <BuddyMark />
            <div>
              <h1>AI Buddy</h1>
            </div>
          </div>
          <div className="chat-header-actions">
            <button
              className="secondary-button compact requests-trigger"
              onClick={() =>
                setPanel({ kind: profile.audience === 'responsible' ? 'inbox' : 'requests' })
              }
            >
              <Icon name="clock" size={17} />
              {profile.audience === 'responsible' ? 'Нужен мой ответ' : 'Мои заявки'}
              {pendingCount > 0 && <span>{pendingCount}</span>}
            </button>
            {profile.audience !== 'responsible' && (
              <button
                className="icon-button route-trigger"
                aria-label="Открыть маршрут"
                onClick={() => setPanel({ kind: 'route' })}
              >
                <Icon name="route" />
              </button>
            )}
            <button
              className="icon-button desktop-diagnostics"
              aria-label="Открыть диагностику"
              onClick={() => setPanel({ kind: 'diagnostics' })}
            >
              <Icon name="more" />
            </button>
            <button
              className="icon-button mobile-only"
              aria-label="Рабочие разделы"
              onClick={() => setPanel({ kind: 'menu' })}
            >
              <Icon name="more" />
            </button>
          </div>
        </header>
        <Conversation
          workspace={workspace}
          busy={busy}
          sending={sending}
          endRef={endRef}
          onSource={openSource}
          onConfirm={(id) => void perform(id, () => api.confirm(id))}
          onPrepare={prepare}
          onInbox={(id) => setPanel({ kind: 'inbox', id })}
          onSend={(text) => void send(text)}
        />
        <div className="composer-area">
          {error && !panel && (
            <div className="error-banner" role="alert">
              <Icon name="info" size={18} />
              <div>
                <p>{error}</p>
                <div className="button-row">
                  {retry && (
                    <button
                      className="text-button"
                      disabled={sending}
                      onClick={() => void send(retry.text, retry)}
                    >
                      Повторить вопрос
                    </button>
                  )}
                  <button
                    className="text-button"
                    disabled={!!busy}
                    onClick={() => void perform('refresh', () => api.refresh())}
                  >
                    Обновить состояние
                  </button>
                </div>
              </div>
              <button className="icon-button" onClick={dismissError} aria-label="Скрыть ошибку">
                <Icon name="close" size={16} />
              </button>
            </div>
          )}
          {!workspace.service_fresh && !error && (
            <p className="service-warning" role="status">
              <Icon name="info" size={16} />
              Сервис доступов недоступен. Закрытые материалы пока скрыты.
              <button
                className="text-button"
                onClick={() => void perform('refresh', () => api.refresh())}
              >
                Проверить
              </button>
            </p>
          )}
          <div className="quick-prompts">
            {(profile.audience === 'responsible'
              ? ['Кому нужна помощь из моих стажёров?']
              : ['Что делать дальше?', 'Не открывается вики', 'Как подготовиться к ревью?']
            ).map((prompt) => (
              <button key={prompt} disabled={sending} onClick={() => void send(prompt)}>
                {prompt}
                <Icon name="chevron" size={13} />
              </button>
            ))}
          </div>
          <form
            className="composer"
            onSubmit={(event) => {
              event.preventDefault();
              void send();
            }}
          >
            <label htmlFor="message" className="sr-only">
              Сообщение AI Buddy
            </label>
            <textarea
              id="message"
              ref={composerRef}
              rows={2}
              maxLength={2000}
              value={composer}
              placeholder="Спроси о работе…"
              onChange={(event) => setComposer(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  void send();
                }
              }}
            />
            <button
              type="submit"
              className="send-button"
              disabled={!composer.trim() || sending}
              aria-label="Отправить сообщение"
            >
              <Icon name="arrow" size={22} />
            </button>
          </form>
          {workspace.model === 'fixture' && (
            <div className="composer-footer">Тестовые ответы · без LLM</div>
          )}
        </div>
      </main>

      <WorkspaceOverview workspace={workspace} onNavigate={setPanel} />

      {panel && (
        <Dialog
          title={panelTitle}
          eyebrow={
            panel.kind === 'source'
              ? 'ИСТОЧНИК ОТВЕТА'
              : panel.kind === 'demo'
                ? 'ДЕМОНСТРАЦИОННЫЙ КОНТУР'
                : undefined
          }
          onClose={closePanel}
        >
          {error && (
            <div className="error-banner" role="alert">
              <Icon name="info" size={18} />
              <div>
                <p>{error}</p>
                <button
                  className="text-button"
                  disabled={!!busy}
                  onClick={() => void perform('refresh', () => api.refresh())}
                >
                  Обновить состояние
                </button>
              </div>
              <button className="icon-button" onClick={dismissError} aria-label="Скрыть ошибку">
                <Icon name="close" size={16} />
              </button>
            </div>
          )}
          {panel.kind === 'source' && <SourcePanel id={panel.id} />}
          {panel.kind === 'source' && panel.id === `${profile.team}-guide` && (
            <div className="source-action-footer">
              {guideTask?.status === 'available' && (
                <>
                  <button
                    className="primary-button"
                    disabled={!!busy}
                    onClick={() => void perform('task', () => api.complete('read-guide'))}
                  >
                    Отметить руководство изученным
                    <Icon name="check" size={16} />
                  </button>
                  <p className="fine-print">Отметь, когда сам ознакомишься с материалом.</p>
                </>
              )}
              {guideTask?.status === 'completed' && (
                <>
                  <p className="guide-completed">
                    <Icon name="check" size={17} />
                    Изучение отмечено в маршруте
                  </p>
                  {workspace.tasks.find((task) => task.id === 'first-issue')?.status ===
                    'available' && (
                    <button
                      className="primary-button"
                      disabled={!!busy}
                      onClick={() => prepare('issue')}
                    >
                      Подготовить первую задачу
                      <Icon name="chevron" size={16} />
                    </button>
                  )}
                </>
              )}
            </div>
          )}
          {panel.kind === 'menu' && (
            <div className="navigation-links mobile-menu">
              <button onClick={() => setPanel({ kind: 'search' })}>
                <Icon name="search" />
                Найти в документации
              </button>
              <button onClick={() => setPanel({ kind: 'inbox' })}>
                <Icon name="chat" />
                {profile.audience === 'responsible' ? 'Нужен мой ответ' : 'Вопросы команде'}
              </button>
              <button onClick={() => setPanel({ kind: 'sources' })}>
                <Icon name="book" />
                База знаний
              </button>
              {profile.mentor && (
                <button onClick={() => setPanel({ kind: 'mentor' })}>
                  <Icon name="chat" />
                  Вопрос наставнику
                </button>
              )}
              <button disabled={sending || !!busy} onClick={() => setPanel({ kind: 'profiles' })}>
                <Icon name="logout" />
                {workspace.demo ? 'Сменить профиль' : 'Учётная запись'}
              </button>
              {workspace.demo && (
                <button onClick={() => setPanel({ kind: 'demo' })}>
                  <Icon name="shield" />
                  Пульт демонстрации
                </button>
              )}
              <button onClick={() => setPanel({ kind: 'diagnostics' })}>
                <Icon name="info" />
                Диагностика
              </button>
            </div>
          )}
          {panel.kind === 'sources' && (
            <SourcesPanel
              sources={workspace.documents}
              onSource={(source) => openSource(source.id, source)}
            />
          )}
          {panel.kind === 'requests' && (
            <RequestsPanel
              workspace={workspace}
              busy={busy === 'refresh'}
              onRefresh={() => void perform('refresh', () => api.refresh())}
            />
          )}
          {panel.kind === 'task' && selectedTask && (
            <TaskPanel
              task={selectedTask}
              workspace={workspace}
              busy={!!busy}
              onSource={openSource}
              onAction={prepare}
              onComplete={() => void perform('task', () => api.complete(selectedTask.id))}
            />
          )}
          {panel.kind === 'mentor' && (
            <HelpRequestsPanel
              key={profile.id}
              workspace={workspace}
              initialCompose
              onRefresh={async () => setWorkspace(await api.state())}
            />
          )}
          {panel.kind === 'inbox' && (
            <HelpRequestsPanel
              key={`${profile.id}-${panel.id ?? ''}`}
              initialSelected={panel.id}
              workspace={workspace}
              onRefresh={async () => setWorkspace(await api.state())}
            />
          )}
          {panel.kind === 'search' && (
            <PublicSearchPanel
              key={profile.id}
              onRefresh={async () => setWorkspace(await api.state())}
              onSource={openSource}
              onAsk={(question) => {
                setPanel(null);
                void send(question);
              }}
            />
          )}
          {panel.kind === 'demo' && workspace.demo && (
            <DemoPanel
              workspace={workspace}
              busy={!!busy}
              onEvent={(token, key, status) =>
                perform('event', () => api.event(token, key, status))
              }
            />
          )}
          {panel.kind === 'diagnostics' && <DiagnosticsPanel />}
          {panel.kind === 'profiles' && (
            <>
              {workspace.demo ? (
                <>
                  <p className="panel-lead">
                    У каждого профиля собственные история, права и маршрут.
                  </p>
                  <ProfilePicker
                    profiles={bootstrap?.profiles ?? []}
                    busy={!!busy || sending}
                    onSelect={(selected) => void login(selected)}
                  />
                </>
              ) : (
                <p className="panel-lead">
                  {profile.name} · {profile.role}
                </p>
              )}
              <button
                className="text-button logout-button"
                onClick={() =>
                  void perform('logout', async () => {
                    await api.logout();
                    setWorkspace(null);
                    setPanel(null);
                  })
                }
              >
                <Icon name="logout" size={17} />
                {workspace.demo ? 'Выйти из тестового профиля' : 'Выйти'}
              </button>
            </>
          )}
          {panel.kind === 'external' && pendingExternal && (
            <>
              <p className="panel-lead">
                {bootstrap?.model} · {bootstrap?.model_endpoint}
              </p>
              <p className="panel-description">
                Отправим этот вопрос и публичные источники. История диалога и командный контекст не
                передаются.
              </p>
              <blockquote>{pendingExternal.text}</blockquote>
              <div className="button-row">
                <button
                  className="secondary-button"
                  onClick={() => {
                    setPendingExternal(null);
                    closePanel();
                  }}
                >
                  Отмена
                </button>
                <button
                  className="primary-button"
                  disabled={!bootstrap?.consent_to}
                  onClick={() => {
                    const request = pendingExternal;
                    setPendingExternal(null);
                    closePanel();
                    void send(request.text, request.pending, bootstrap?.consent_to);
                  }}
                >
                  Отправить вопрос
                </button>
              </div>
            </>
          )}
          {panel.kind === 'route' && (
            <RoutePanel
              workspace={workspace}
              onTask={(task) => setPanel({ kind: 'task', id: task.id })}
              onMentor={() => setPanel({ kind: 'mentor' })}
            />
          )}
          {panel.kind === 'help' && (
            <>
              <div className="about-brand">
                <BuddyMark />
                <h3>AI Buddy</h3>
              </div>
              <p className="panel-description">
                Личный помощник стажёра: доступные источники, понятный следующий шаг и действия с
                подтверждением.
              </p>
              {workspace.demo && (
                <div className="provenance">
                  <Icon name="info" />
                  <span>
                    Это самостоятельный веб-прототип. Настоящие Mattermost, Confluence и Jira не
                    подключены. Корпоративные действия выполняет отдельный тестовый HTTP-сервис.
                  </span>
                </div>
              )}
              <h3 className="panel-section-title">Что сохраняется</h3>
              <p className="panel-lead">
                Диалог, подготовленные действия, заявки, черновик наставнику и отметки маршрута
                хранятся в PostgreSQL. Модель не определяет твои права и не выполняет операции
                самостоятельно.
              </p>
              <h3 className="panel-section-title">Как проверить результат</h3>
              <p className="panel-lead">
                Источники открываются из ответа. Состояния операций находятся в «Моих заявках».
                События согласования и выдачи воспроизводятся через пульт оператора.
              </p>
              <p className="fine-print">
                Модель в конфигурации: {workspace.model}. Публичные материалы могут быть
                историческими; тестовые инструкции помечены отдельно.
              </p>
            </>
          )}
        </Dialog>
      )}
    </div>
  );
}
