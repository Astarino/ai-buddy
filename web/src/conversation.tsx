import type { RefObject } from 'react';
import { ActionCard, Avatar, BuddyMark, Icon } from './components';
import type { Workspace } from './types';

type ConversationProps = {
  workspace: Workspace;
  busy: string;
  sending: boolean;
  endRef: RefObject<HTMLDivElement | null>;
  onSource: (id: string) => void;
  onConfirm: (id: string) => void;
  onPrepare: (kind: 'access' | 'issue') => void;
  onInbox: () => void;
  onSend: (text: string) => void;
};

export function Conversation({
  workspace,
  busy,
  sending,
  endRef,
  onSource,
  onConfirm,
  onPrepare,
  onInbox,
  onSend,
}: ConversationProps) {
  const profile = workspace.profile;
  const guideID = `${profile.team}-guide`;
  const hasGuide = workspace.documents.some((source) => source.id === guideID);
  const accessCompleted = workspace.tasks.some(
    (task) => task.id === 'space-access' && task.status === 'completed',
  );
  return (
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
          {profile.audience === 'responsible' ? (
            <div className="next-step">
              <div>
                <h3>Запросы стажёров</h3>
                <button className="text-button" onClick={onInbox}>
                  Нужен мой ответ
                  <Icon name="chevron" size={15} />
                </button>
              </div>
            </div>
          ) : (
            <div className="next-step">
              <div>
                <h3>
                  {accessCompleted ? 'Доступ к материалам открыт' : 'Доступ к командной базе'}
                </h3>
                <button
                  className="text-button"
                  disabled={sending}
                  onClick={() =>
                    hasGuide ? onSource(guideID) : onSend('Не открывается командная вики')
                  }
                >
                  {hasGuide ? 'Открыть руководство' : 'Разобраться с доступом'}
                  <Icon name="chevron" size={15} />
                </button>
              </div>
              <span className="next-step-art" aria-hidden="true">
                <Icon name="book" size={36} />
              </span>
            </div>
          )}
        </div>
        {workspace.messages.map((message) => (
          <article className={`message message-${message.role}`} key={message.id}>
            {message.role === 'assistant' ? <BuddyMark small /> : <Avatar name={profile.name} />}
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
                {message.kind === 'fixture' && <span className="fixture-tag">Тестовый ответ</span>}
              </div>
              <p className="message-text">{message.text}</p>
              {message.source_ids.length > 0 && (
                <div className="message-sources">
                  {message.source_ids.map((id) => (
                    <button key={id} onClick={() => onSource(id)}>
                      <Icon name="book" size={13} />
                      {workspace.documents.find((source) => source.id === id)?.title ?? 'Источник'}
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
                      onConfirm={() => onConfirm(action.id)}
                      onRecreate={() => onPrepare(action.kind)}
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
  );
}
