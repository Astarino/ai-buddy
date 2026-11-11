import { useEffect, useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { Action, Operation, Profile, Task, Workspace } from './types';

export type IconName =
  | 'chat'
  | 'book'
  | 'route'
  | 'arrow'
  | 'chevron'
  | 'close'
  | 'check'
  | 'lock'
  | 'clock'
  | 'refresh'
  | 'more'
  | 'external'
  | 'copy'
  | 'info'
  | 'logout'
  | 'search'
  | 'shield';
const paths: Record<IconName, ReactNode> = {
  chat: <path d="M19 10a7 7 0 0 1-7 7H5l-3 3V10a8 8 0 0 1 16-2" />,
  book: (
    <>
      <path d="M4 3h12a2 2 0 0 1 2 2v15H6a3 3 0 0 1 0-6h12M4 3v14" />
      <path d="M8 7h6M8 10h4" />
    </>
  ),
  route: (
    <>
      <circle cx="5" cy="4" r="2" />
      <circle cx="17" cy="18" r="2" />
      <path d="M5 6v5h12V6M17 11v5M13 6l4-4 4 4" />
    </>
  ),
  arrow: <path d="m5 11 6-6 6 6M11 5v14" />,
  chevron: <path d="m9 5 6 6-6 6" />,
  close: <path d="m5 5 12 12M17 5 5 17" />,
  check: <path d="m5 11 4 4 8-8" />,
  lock: (
    <>
      <rect x="5" y="9" width="12" height="10" rx="2" />
      <path d="M8 9V6a3 3 0 0 1 6 0v3M11 13v2" />
    </>
  ),
  clock: (
    <>
      <circle cx="11" cy="11" r="8" />
      <path d="M11 6v5l3 2" />
    </>
  ),
  refresh: (
    <>
      <path d="M18 8a7 7 0 1 0 0 7M18 3v5h-5" />
    </>
  ),
  more: (
    <>
      <circle cx="4" cy="11" r="1" />
      <circle cx="11" cy="11" r="1" />
      <circle cx="18" cy="11" r="1" />
    </>
  ),
  external: <path d="M12 3h7v7M19 3 9 13M9 4H4v15h15v-5" />,
  copy: (
    <>
      <rect x="7" y="7" width="12" height="12" rx="2" />
      <path d="M14 7V3H3v11h4" />
    </>
  ),
  info: (
    <>
      <circle cx="11" cy="11" r="8" />
      <path d="M11 10v6M11 6v1" />
    </>
  ),
  logout: <path d="M9 3H3v16h6M8 11h12m-4-4 4 4-4 4" />,
  search: (
    <>
      <circle cx="9" cy="9" r="6" />
      <path d="m14 14 6 6" />
    </>
  ),
  shield: (
    <>
      <path d="m11 2 8 3v6c0 5-8 9-8 9s-8-4-8-9V5z" />
      <path d="m7 10 3 3 5-5" />
    </>
  ),
};
export function Icon({ name, size = 20 }: { name: IconName; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 22 22"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}

export function BuddyMark({ small = false }: { small?: boolean }) {
  return (
    <span className={`buddy-mark ${small ? 'small' : ''}`} aria-hidden="true">
      <i />
      <i />
      <i />
      <i />
    </span>
  );
}

export function initials(name: string) {
  return name
    .split(' ')
    .map((part) => part[0])
    .slice(0, 2)
    .join('');
}

export function Avatar({ name, mentor = false }: { name: string; mentor?: boolean }) {
  return (
    <span className={`avatar ${mentor ? 'mentor' : ''}`} aria-hidden="true">
      {initials(name)}
    </span>
  );
}

export function Dialog({
  title,
  eyebrow,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  eyebrow?: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    const cancel = (event: Event) => {
      event.preventDefault();
      closeRef.current();
    };
    dialog.addEventListener('cancel', cancel);
    return () => {
      dialog.removeEventListener('cancel', cancel);
      dialog.close();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className={`drawer ${wide ? 'wide' : ''}`}
      aria-labelledby="drawer-title"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="drawer-inner">
        <header className="drawer-header">
          <div>
            {eyebrow && <span className="eyebrow">{eyebrow}</span>}
            <h2 id="drawer-title">{title}</h2>
          </div>
          <button className="icon-button" onClick={onClose} aria-label="Закрыть панель">
            <Icon name="close" />
          </button>
        </header>
        <div className="drawer-body">{children}</div>
      </div>
    </dialog>
  );
}

export const statusLabels: Record<string, string> = {
  available: 'Можно начать',
  blocked: 'Есть зависимость',
  completed: 'Завершено',
  waiting: 'Ждём согласования',
  in_progress: 'В работе',
  pending: 'Ожидает согласования',
  approved: 'Согласовано, ждём выдачи',
  granted: 'Доступ выдан',
  rejected: 'Отклонено',
  revoked: 'Доступ отозван',
  assigned: 'Назначена на тебя',
  done: 'Завершена',
};

export function Status({ value }: { value: string }) {
  return <span className={`status status-${value}`}>{statusLabels[value] ?? value}</span>;
}

export function ActionCard({
  action,
  busy,
  onConfirm,
  onRecreate,
}: {
  action: Action;
  busy: boolean;
  onConfirm: () => void;
  onRecreate: () => void;
}) {
  const expired = Date.parse(action.expires_at) <= Date.now();
  return (
    <section
      className="action-card"
      aria-label={action.kind === 'access' ? 'Заявка на доступ' : 'Назначение задачи'}
    >
      <div className="action-card-heading">
        <span className="resource-icon">
          <Icon name={action.kind === 'access' ? 'lock' : 'route'} />
        </span>
        <div>
          <span className="eyebrow">
            {action.kind === 'access' ? 'CONFLUENCE · ЗАПРОС ДОСТУПА' : 'JIRA · ПЕРВАЯ ЗАДАЧА'}
          </span>
          <h3>{action.kind === 'access' ? `Пространство ${action.resource}` : action.resource}</h3>
        </div>
      </div>
      <dl className="action-details">
        <div>
          <dt>Действие</dt>
          <dd>{action.level}</dd>
        </div>
        <div>
          <dt>Зачем</dt>
          <dd>{action.reason}</dd>
        </div>
      </dl>
      {action.status === 'completed' ? (
        <div className="action-receipt">
          <Icon name="check" size={17} />
          <span>Отправлено · {action.result_id}</span>
        </div>
      ) : (
        <>
          <p className="action-note">
            {action.kind === 'access'
              ? 'Будет создана заявка владельцу пространства. Доступ появится после выдачи прав.'
              : 'Учебная задача будет назначена на тебя. Её завершение подтвердит сервис Jira.'}
          </p>
          <button
            className="primary-button"
            disabled={busy}
            onClick={expired ? onRecreate : onConfirm}
          >
            {busy
              ? 'Проверяем результат…'
              : expired
                ? 'Подготовить заново'
                : action.kind === 'access'
                  ? 'Отправить заявку'
                  : 'Взять задачу'}
            <Icon name={expired ? 'refresh' : 'chevron'} size={16} />
          </button>
          <span className="confirmation-hint">
            {expired ? 'Срок подтверждения истёк' : 'Нажатие подтверждает эти параметры'}
          </span>
        </>
      )}
    </section>
  );
}

export function OperationCard({ operation }: { operation: Operation }) {
  return (
    <article className="operation-card">
      <div className="operation-heading">
        <span className="mono-label">{operation.id}</span>
        <Status value={operation.status} />
      </div>
      <h3>
        {operation.kind === 'access'
          ? `Чтение пространства ${operation.resource}`
          : 'Проверка пустого поискового запроса'}
      </h3>
      <p>{operation.note}</p>
      <div className="operation-meta">
        {operation.kind === 'access' ? 'Сервис доступов' : 'Jira'} · тестовый результат
      </div>
    </article>
  );
}

export function RoutePanel({
  workspace,
  onTask,
  onMentor,
  onClose,
}: {
  workspace: Workspace;
  onTask: (task: Task) => void;
  onMentor: () => void;
  onClose?: () => void;
}) {
  const [week, setWeek] = useState('1 неделя');
  const panelID = useId();
  const complete = workspace.tasks.filter((task) => task.status === 'completed').length;
  return (
    <aside className="route-panel" aria-label="Маршрут адаптации">
      <header className="route-heading">
        <div>
          <span className="eyebrow">АДАПТАЦИЯ</span>
          <h2>Мой маршрут</h2>
        </div>
        {onClose ? (
          <button
            className="icon-button mobile-only"
            onClick={onClose}
            aria-label="Свернуть маршрут"
          >
            <Icon name="close" />
          </button>
        ) : (
          <span className="route-count">
            {complete}
            <span> / {workspace.tasks.length}</span>
          </span>
        )}
      </header>
      <div
        className="route-progress"
        role="progressbar"
        aria-label="Прогресс адаптации"
        aria-valuenow={complete}
        aria-valuemin={0}
        aria-valuemax={workspace.tasks.length}
      >
        <span style={{ width: `${(complete / workspace.tasks.length) * 100}%` }} />
      </div>
      <div className="week-tabs" role="tablist" aria-label="Неделя адаптации">
        {['1 неделя', '2 неделя'].map((value, index) => (
          <button
            role="tab"
            id={`${panelID}-${index}`}
            aria-controls={panelID}
            aria-selected={week === value}
            tabIndex={week === value ? 0 : -1}
            key={value}
            onClick={() => setWeek(value)}
            onKeyDown={(event) => {
              if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
                event.preventDefault();
                const next = index === 0 ? 1 : 0;
                setWeek(next === 0 ? '1 неделя' : '2 неделя');
                event.currentTarget.parentElement
                  ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
                  [next].focus();
              }
            }}
          >
            {value}
          </button>
        ))}
      </div>
      <div
        className="route-list"
        id={panelID}
        role="tabpanel"
        aria-labelledby={`${panelID}-${week === '1 неделя' ? 0 : 1}`}
      >
        {workspace.tasks
          .filter((task) => task.period === week)
          .map((task) => (
            <button
              className={`route-task ${task.status} ${task.id === workspace.next_task_id ? 'next-task' : ''}`}
              key={task.id}
              onClick={() => onTask(task)}
            >
              <span className="task-bullet">
                <Icon
                  name={
                    task.status === 'completed'
                      ? 'check'
                      : task.status === 'blocked'
                        ? 'lock'
                        : task.status === 'waiting'
                          ? 'clock'
                          : 'route'
                  }
                  size={14}
                />
              </span>
              <span>
                <span className="task-title">{task.title}</span>
                <span className="task-caption">
                  {task.id === workspace.next_task_id ? 'Ближайший шаг' : statusLabels[task.status]}
                </span>
              </span>
              <Icon name="chevron" size={15} />
            </button>
          ))}
      </div>
      <p className="route-footnote">
        Сроки из чек-листа. Командные шаги и зависимости — тестовый пример.
      </p>
      <div className="mentor-card">
        <span className="eyebrow">РЯДОМ, ЕСЛИ ПОНАДОБИТСЯ</span>
        <div className="mentor-person">
          <Avatar name={workspace.profile.mentor} mentor />
          <div>
            <strong>{workspace.profile.mentor}</strong>
            <span>Твой наставник</span>
          </div>
        </div>
        <button className="secondary-button" onClick={onMentor}>
          Подготовить вопрос
          <Icon name="chevron" size={16} />
        </button>
        <p>
          Buddy поможет с рутиной.
          <br />
          Важное обсудите лично.
        </p>
      </div>
    </aside>
  );
}

export function ProfilePicker({
  profiles,
  onSelect,
  busy,
}: {
  profiles: Profile[];
  onSelect: (profile: Profile) => void;
  busy: boolean;
}) {
  return (
    <div className="profile-options">
      {profiles.map((profile) => (
        <button
          className="profile-option"
          key={profile.id}
          disabled={busy}
          onClick={() => onSelect(profile)}
        >
          <Avatar name={profile.name} />
          <span>
            <strong>{profile.name}</strong>
            <span>
              {profile.role} · {profile.team_name}
            </span>
          </span>
          <Icon name="chevron" />
        </button>
      ))}
    </div>
  );
}
