import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from './api';
import {
  ActionCard,
  Avatar,
  BuddyMark,
  Dialog,
  Icon,
  ProfilePicker,
  RoutePanel,
} from './components';
import {
  DemoPanel,
  DiagnosticsPanel,
  MentorPanel,
  RequestsPanel,
  SourcePanel,
  SourcesPanel,
  TaskPanel,
} from './panels';
import type { Bootstrap, Profile, Source, Workspace } from './types';

type Panel =
  | { kind: 'source'; id: string; title: string }
  | { kind: 'task'; id: string }
  | {
      kind:
        | 'sources'
        | 'requests'
        | 'mentor'
        | 'demo'
        | 'diagnostics'
        | 'help'
        | 'profiles'
        | 'route'
        | 'menu';
    }
  | null;

export default function App() {
  const [bootstrap, setBootstrap] = useState<Bootstrap | null>(null);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [panel, setPanel] = useState<Panel>(null);
  const [composer, setComposer] = useState('');
  const [sending, setSending] = useState(false);
  const [retry, setRetry] = useState<{ id: string; text: string } | null>(null);
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

  async function send(text = composer, pending = retry) {
    text = text.trim();
    if (!text || sending || !workspace) return;
    const id = pending?.text === text ? pending.id : crypto.randomUUID();
    setSending(true);
    setError('');
    setRetry({ id, text });
    if (!workspace.messages.some((message) => message.id === id))
      setWorkspace({
        ...workspace,
        messages: [
          ...workspace.messages,
          { id, role: 'user', text, created_at: new Date().toISOString(), source_ids: [] },
        ],
      });
    try {
      await api.message(id, text);
      setWorkspace(await api.state());
      setComposer('');
      setRetry(null);
    } catch (reason) {
      setError((reason as Error).message);
      setComposer(text);
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
          <span className="eyebrow">ДЕМОНСТРАЦИОННЫЙ КОНТУР</span>
          <h1>
            Твой первый шаг
            <br />в новой команде
          </h1>
          <p>
            Рабочие вопросы, материалы и действия —<br className="desktop-break" /> в одном личном
            диалоге.
          </p>
          <h2>Выбери тестовый профиль</h2>
          {bootstrap && (
            <ProfilePicker
              profiles={bootstrap.profiles}
              onSelect={(profile) => void login(profile)}
              busy={busy !== ''}
            />
          )}
          {error && (
            <div className="inline-error" role="alert">
              {error}
              <button className="text-button" onClick={() => setReload((value) => value + 1)}>
                Повторить загрузку
              </button>
            </div>
          )}
          <div className="login-note">
            <Icon name="shield" size={18} />
            <span>
              Профили и командные инструкции вымышлены.
              <br />
              Настоящие корпоративные данные не используются.
            </span>
          </div>
        </section>
        <span className="login-caption">AI Buddy · прототип для Hack&amp;Change 2026</span>
      </main>
    );

  const profile = workspace.profile;
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
    profiles: 'Тестовый профиль',
    route: 'Мой маршрут',
    menu: 'Рабочее пространство',
  };
  const panelTitle =
    panel?.kind === 'source'
      ? panel.title
      : (selectedTask?.title ?? panelNames[panel?.kind ?? 'help'] ?? 'Шаг маршрута');

  return (
    <div className="app-shell">
      <header className="workspace-bar">
        <a href="/" className="brand" aria-label="AI Buddy — главная">
          <BuddyMark small />
          <strong>AI Buddy</strong>
        </a>
        <div className="workspace-bar-right">
          <span className="demo-label">
            <span />
            Демонстрационный контур
          </span>
          <button
            className="icon-button"
            aria-label="О прототипе"
            onClick={() => setPanel({ kind: 'help' })}
          >
            <Icon name="info" size={19} />
          </button>
        </div>
      </header>

      <nav className="sidebar" aria-label="Рабочее пространство">
        <div className="team-switch">
          <span className="team-logo">{profile.project[0]}</span>
          <div>
            <strong>Команда {profile.team_name}</strong>
            <span>Backend · стажировка</span>
          </div>
        </div>
        <div className="sidebar-section">
          <span className="eyebrow">ЛИЧНЫЕ СООБЩЕНИЯ</span>
          <button className="contact active" aria-current="page" onClick={closePanel}>
            <BuddyMark small />
            <span>
              <strong>AI Buddy</strong>
            </span>
          </button>
          <button className="contact" onClick={() => setPanel({ kind: 'mentor' })}>
            <Avatar name={profile.mentor} mentor />
            <span>
              <strong>{profile.mentor}</strong>
              <span>Твой наставник</span>
            </span>
          </button>
        </div>
        <div className="sidebar-section navigation-links">
          <span className="eyebrow">ПОД РУКОЙ</span>
          <button onClick={() => setPanel({ kind: 'route' })}>
            <Icon name="route" />
            Мой маршрут
            <span className="nav-count">
              {workspace.tasks.filter((task) => task.status === 'completed').length}/
              {workspace.tasks.length}
            </span>
          </button>
          <button onClick={() => setPanel({ kind: 'sources' })}>
            <Icon name="book" />
            База знаний
          </button>
          <button onClick={() => setPanel({ kind: 'requests' })}>
            <Icon name="clock" />
            Мои заявки{pendingCount > 0 && <span className="nav-count">{pendingCount}</span>}
          </button>
        </div>
        <div className="workspace-context">
          <p>
            <span className="day-chip">{profile.day}</span>
            <span>
              <strong>День в команде</strong>
            </span>
          </p>
        </div>
        <div className="sidebar-bottom">
          <button className="sidebar-demo" onClick={() => setPanel({ kind: 'demo' })}>
            <Icon name="shield" size={16} />
            Пульт демонстрации
          </button>
          <button
            className="profile-switch"
            onClick={() => setPanel({ kind: 'profiles' })}
            disabled={sending || !!busy}
          >
            <Avatar name={profile.name} />
            <span>
              <strong>{profile.name}</strong>
              <span>{profile.role}</span>
            </span>
            <Icon name="more" size={18} />
          </button>
        </div>
      </nav>

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
              onClick={() => setPanel({ kind: 'requests' })}
            >
              <Icon name="clock" size={17} />
              Мои заявки{pendingCount > 0 && <span>{pendingCount}</span>}
            </button>
            <button
              className="icon-button route-trigger"
              aria-label="Открыть маршрут"
              onClick={() => setPanel({ kind: 'route' })}
            >
              <Icon name="route" />
            </button>
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
        <div
          className="conversation"
          role="log"
          aria-label="Диалог с AI Buddy"
          aria-live="polite"
          aria-relevant="additions text"
        >
          <div className="conversation-inner">
            <div className="welcome">
              <h2>
                Привет, {profile.name.split(' ')[0]}
                <span className="greeting-dot">.</span>
              </h2>
              <div className="next-step">
                <div>
                  <h3>
                    {workspace.tasks.find((task) => task.id === 'space-access')?.status ===
                    'completed'
                      ? 'Доступ к материалам открыт'
                      : 'Доступ к командной базе'}
                  </h3>
                  <button
                    className="text-button"
                    disabled={sending}
                    onClick={() =>
                      workspace.documents.some((source) => source.id === `${profile.team}-guide`)
                        ? openSource(`${profile.team}-guide`)
                        : void send('Не открывается командная вики')
                    }
                  >
                    {workspace.documents.some((source) => source.id === `${profile.team}-guide`)
                      ? 'Открыть руководство'
                      : 'Разобраться с доступом'}
                    <Icon name="chevron" size={15} />
                  </button>
                </div>
                <span className="next-step-art" aria-hidden="true">
                  <Icon name="book" size={36} />
                </span>
              </div>
            </div>
            {workspace.messages.map((message) => (
              <article className={`message message-${message.role}`} key={message.id}>
                {message.role === 'assistant' ? (
                  <BuddyMark small />
                ) : (
                  <Avatar name={profile.name} />
                )}
                <div className="message-content">
                  <div className="message-author">
                    <strong>
                      {message.role === 'assistant' ? 'AI Buddy' : profile.name.split(' ')[0]}
                    </strong>
                    <time dateTime={message.created_at}>
                      {new Date(message.created_at).toLocaleTimeString('ru-RU', {
                        hour: '2-digit',
                        minute: '2-digit',
                      })}
                    </time>
                    {message.kind === 'fixture' && (
                      <span className="fixture-tag">Тестовый ответ</span>
                    )}
                  </div>
                  <p className="message-text">{message.text}</p>
                  {message.source_ids.length > 0 && (
                    <div className="message-sources">
                      {message.source_ids.map((id) => (
                        <button key={id} onClick={() => openSource(id)}>
                          <Icon name="book" size={13} />
                          {workspace.documents.find((source) => source.id === id)?.title ??
                            'Источник'}
                          <Icon name="chevron" size={12} />
                        </button>
                      ))}
                    </div>
                  )}
                  {message.action_id &&
                    workspace.actions
                      .filter((action) => action.id === message.action_id)
                      .map((action) => (
                        <ActionCard
                          action={action}
                          key={action.id}
                          busy={busy === action.id}
                          onConfirm={() => void perform(action.id, () => api.confirm(action.id))}
                          onRecreate={() => prepare(action.kind)}
                        />
                      ))}
                </div>
              </article>
            ))}
            {sending && (
              <div className="message message-assistant waiting-message" role="status">
                <BuddyMark small />
                <div>
                  <strong>AI Buddy</strong>
                  <p>
                    <span className="typing-dots">
                      <i />
                      <i />
                      <i />
                    </span>
                    Проверяю материалы и следующий шаг…
                  </p>
                </div>
              </div>
            )}
            <div ref={endRef} />
          </div>
        </div>
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
            {['Что делать дальше?', 'Не открывается вики', 'Как подготовиться к ревью?'].map(
              (prompt) => (
                <button key={prompt} disabled={sending} onClick={() => void send(prompt)}>
                  {prompt}
                  <Icon name="chevron" size={13} />
                </button>
              ),
            )}
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

      <div className="desktop-route">
        <RoutePanel
          workspace={workspace}
          onTask={(task) => setPanel({ kind: 'task', id: task.id })}
          onMentor={() => setPanel({ kind: 'mentor' })}
        />
      </div>

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
              <button onClick={() => setPanel({ kind: 'sources' })}>
                <Icon name="book" />
                База знаний
              </button>
              <button onClick={() => setPanel({ kind: 'mentor' })}>
                <Icon name="chat" />
                Вопрос наставнику
              </button>
              <button disabled={sending || !!busy} onClick={() => setPanel({ kind: 'profiles' })}>
                <Icon name="logout" />
                Сменить профиль
              </button>
              <button onClick={() => setPanel({ kind: 'demo' })}>
                <Icon name="shield" />
                Пульт демонстрации
              </button>
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
            <MentorPanel
              workspace={workspace}
              busy={busy === 'draft'}
              onDraft={() => void perform('draft', () => api.draft())}
            />
          )}
          {panel.kind === 'demo' && (
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
              <p className="panel-lead">У каждого профиля собственные история, права и маршрут.</p>
              <ProfilePicker
                profiles={bootstrap?.profiles ?? []}
                busy={!!busy || sending}
                onSelect={(selected) => void login(selected)}
              />
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
                Выйти из тестового профиля
              </button>
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
                Личный помощник Backend-стажёра в первые две недели: разрешённые источники, понятный
                следующий шаг и действия с подтверждением.
              </p>
              <div className="provenance">
                <Icon name="info" />
                <span>
                  Это самостоятельный веб-прототип. Настоящие Mattermost, Confluence и Jira не
                  подключены. Корпоративные действия выполняет отдельный тестовый HTTP-сервис.
                </span>
              </div>
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
