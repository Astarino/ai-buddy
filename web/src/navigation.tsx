import { Avatar, BuddyMark, StartMark, Icon, RoutePanel } from './components';
import type { Workspace } from './types';

export type Panel =
  | { kind: 'inbox'; id?: string }
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
        | 'external'
        | 'route'
        | 'menu'
        | 'search';
    }
  | null;

type NavigationProps = {
  workspace: Workspace;
  syncError: boolean;
  busy: string;
  sending: boolean;
  pendingCount: number;
  requestAttention: number;
  onNavigate: (panel: Panel) => void;
  onRefresh: () => void;
};

export function WorkspaceNavigation({
  workspace,
  syncError,
  busy,
  sending,
  pendingCount,
  requestAttention,
  onNavigate,
  onRefresh,
}: NavigationProps) {
  const profile = workspace.profile;
  return (
    <>
      <header className="workspace-bar">
        {syncError && (
          <button className="sync-warning" onClick={onRefresh}>
            Статусы не обновились · повторить
          </button>
        )}
        <a href="/" className="brand" aria-label="Avito Start — главная">
          <StartMark />
          <strong>Avito Start</strong>
        </a>
        <div className="workspace-bar-right">
          {workspace.demo && (
            <span className="demo-label">
              <span />
              Тестовая среда
            </span>
          )}
          <button
            className="icon-button"
            aria-label="О прототипе"
            onClick={() => onNavigate({ kind: 'help' })}
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
            <span>{profile.role}</span>
          </div>
        </div>
        <div className="sidebar-section">
          <span className="eyebrow">ЛИЧНЫЕ СООБЩЕНИЯ</span>
          <button className="contact active" aria-current="page" onClick={() => onNavigate(null)}>
            <BuddyMark small />
            <span>
              <strong>AI Buddy</strong>
            </span>
          </button>
          {profile.mentor && (
            <button className="contact" onClick={() => onNavigate({ kind: 'mentor' })}>
              <Avatar name={profile.mentor} mentor />
              <span>
                <strong>{profile.mentor}</strong>
                <span>Твой наставник</span>
              </span>
            </button>
          )}
        </div>
        <div className="sidebar-section navigation-links">
          <span className="eyebrow">ПОД РУКОЙ</span>
          {profile.audience !== 'responsible' && (
            <button onClick={() => onNavigate({ kind: 'route' })}>
              <Icon name="route" />
              Мой маршрут
              <span className="nav-count">
                {workspace.tasks.filter((task) => task.status === 'completed').length}/
                {workspace.tasks.length}
              </span>
            </button>
          )}
          <button onClick={() => onNavigate({ kind: 'sources' })}>
            <Icon name="book" />
            База знаний
          </button>
          <button onClick={() => onNavigate({ kind: 'search' })}>
            <Icon name="search" />
            Найти в документации
          </button>
          <button onClick={() => onNavigate({ kind: 'inbox' })}>
            <Icon name="chat" />
            {profile.audience === 'responsible' ? 'Нужен мой ответ' : 'Вопросы команде'}
            {requestAttention > 0 && <span className="nav-count">{requestAttention}</span>}
          </button>
          {profile.audience !== 'responsible' && (
            <button onClick={() => onNavigate({ kind: 'requests' })}>
              <Icon name="clock" />
              Мои заявки{pendingCount > 0 && <span className="nav-count">{pendingCount}</span>}
            </button>
          )}
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
          {workspace.demo && (
            <button className="sidebar-demo" onClick={() => onNavigate({ kind: 'demo' })}>
              <Icon name="shield" size={16} />
              Пульт демонстрации
            </button>
          )}
          <button
            className="profile-switch"
            onClick={() => onNavigate({ kind: 'profiles' })}
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
    </>
  );
}

export function WorkspaceOverview({
  workspace,
  onNavigate,
}: {
  workspace: Workspace;
  onNavigate: (panel: Panel) => void;
}) {
  const profile = workspace.profile;
  const assignedRequests = (workspace.requests ?? []).filter(
    (item) => item.assignee_id === profile.id && item.status === 'waiting',
  );
  const waitingPeople = Array.from(new Set(assignedRequests.map((item) => item.requester_id)));
  return (
    <div className="desktop-route">
      {profile.audience === 'responsible' ? (
        <section className="responsible-overview">
          <h2>Нужен мой ответ</h2>
          {waitingPeople.length === 0 && <p className="fine-print">Новых запросов пока нет.</p>}
          {waitingPeople.map((id) => {
            const items = assignedRequests.filter((item) => item.requester_id === id);
            return (
              <button
                className="request-row"
                key={id}
                onClick={() => onNavigate({ kind: 'inbox', id: items[0].id })}
              >
                <span>
                  <strong>{items[0].requester}</strong>
                  <small>
                    {items[0].subject}
                    {items.length > 1 ? ` · ещё ${items.length - 1}` : ''}
                  </small>
                </span>
                <Icon name="chevron" size={16} />
              </button>
            );
          })}
        </section>
      ) : (
        <RoutePanel
          workspace={workspace}
          onTask={(task) => onNavigate({ kind: 'task', id: task.id })}
          onMentor={() => onNavigate({ kind: 'mentor' })}
        />
      )}
    </div>
  );
}
