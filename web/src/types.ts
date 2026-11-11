export interface Profile {
  id: string;
  name: string;
  team: string;
  team_name: string;
  role: string;
  mentor: string;
  day: number;
  space: string;
  project: string;
}
export interface Source {
  id: string;
  title: string;
  body: string;
  team: string;
  scope: string;
  origin: string;
  url?: string;
  version: string;
  updated_at: string;
  synthetic: boolean;
}
export interface Task {
  id: string;
  title: string;
  description: string;
  period: string;
  owner: string;
  requires: string[];
  completion: string;
  document_id: string;
  source: string;
  source_url: string;
  status: 'available' | 'blocked' | 'completed' | 'waiting' | 'in_progress';
  blocker?: string;
}
export interface Message {
  id: string;
  role: 'user' | 'assistant';
  text: string;
  created_at: string;
  source_ids: string[];
  action_id?: string;
  reply_to?: string;
  kind?: string;
  duration_ms?: number;
}
export interface Action {
  id: string;
  kind: 'access' | 'issue';
  resource: string;
  level: string;
  reason: string;
  status: string;
  expires_at: string;
  result_id?: string;
}
export interface Operation {
  id: string;
  key: string;
  user_id: string;
  kind: 'access' | 'issue';
  resource: string;
  level: string;
  reason: string;
  status: string;
  note: string;
  updated_at: string;
}
export interface Workspace {
  profile: Profile;
  messages: Message[];
  actions: Action[];
  tasks: Task[];
  operations: Operation[];
  documents: Source[];
  next_task_id: string;
  draft: string;
  service_fresh: boolean;
  model: string;
  demo: boolean;
}
export interface Bootstrap {
  profiles: Profile[];
  model: string;
  demo: boolean;
}
