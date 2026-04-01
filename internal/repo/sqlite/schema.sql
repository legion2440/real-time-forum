CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  username TEXT NOT NULL UNIQUE,
  display_name TEXT,
  first_name TEXT NOT NULL DEFAULT '',
  last_name TEXT NOT NULL DEFAULT '',
  age INTEGER NOT NULL DEFAULT 0,
  gender TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'user',
  pass_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  profile_initialized INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS auth_identities (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  provider TEXT NOT NULL,
  provider_user_id TEXT NOT NULL,
  provider_email TEXT NOT NULL DEFAULT '',
  provider_email_verified INTEGER NOT NULL DEFAULT 0,
  provider_display_name TEXT NOT NULL DEFAULT '',
  provider_avatar_url TEXT NOT NULL DEFAULT '',
  linked_at INTEGER NOT NULL,
  last_login_at INTEGER NOT NULL,
  UNIQUE(provider, provider_user_id),
  UNIQUE(user_id, provider),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS auth_flows (
  token TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  user_id INTEGER,
  payload_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS attachments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  owner_user_id INTEGER NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL,
  storage_key TEXT NOT NULL,
  original_name TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  FOREIGN KEY(owner_user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS posts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  attachment_id INTEGER,
  is_under_review INTEGER NOT NULL DEFAULT 0,
  approved_by INTEGER,
  approved_at INTEGER,
  delete_protected INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER,
  deleted_by INTEGER,
  deleted_by_role TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(attachment_id) REFERENCES attachments(id) ON DELETE SET NULL,
  FOREIGN KEY(approved_by) REFERENCES users(id) ON DELETE SET NULL,
  FOREIGN KEY(deleted_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  post_id INTEGER NOT NULL,
  parent_id INTEGER,
  user_id INTEGER NOT NULL,
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  deleted_at INTEGER,
  deleted_body TEXT NOT NULL DEFAULT '',
  deleted_by INTEGER,
  deleted_by_role TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(parent_id) REFERENCES comments(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(deleted_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS categories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL UNIQUE,
  is_system INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS post_categories (
  post_id INTEGER NOT NULL,
  category_id INTEGER NOT NULL,
  PRIMARY KEY (post_id, category_id),
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS post_reactions (
  post_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  value INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (post_id, user_id),
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS comment_reactions (
  comment_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  value INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (comment_id, user_id),
  FOREIGN KEY(comment_id) REFERENCES comments(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS private_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  from_user_id INTEGER NOT NULL,
  to_user_id INTEGER NOT NULL,
  body TEXT NOT NULL,
  attachment_id INTEGER,
  created_at INTEGER NOT NULL,
  FOREIGN KEY(from_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(to_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(attachment_id) REFERENCES attachments(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS dm_read_state (
  user_id INTEGER NOT NULL,
  peer_id INTEGER NOT NULL,
  last_read_message_id INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, peer_id),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(peer_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  actor_user_id INTEGER,
  bucket TEXT NOT NULL,
  type TEXT NOT NULL,
  entity_type TEXT NOT NULL DEFAULT '',
  entity_id INTEGER NOT NULL DEFAULT 0,
  secondary_entity_type TEXT NOT NULL DEFAULT '',
  secondary_entity_id INTEGER NOT NULL DEFAULT 0,
  payload_json TEXT NOT NULL DEFAULT '{}',
  is_read INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  read_at INTEGER,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(actor_user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS post_subscriptions (
  user_id INTEGER NOT NULL,
  post_id INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, post_id),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS user_follows (
  follower_user_id INTEGER NOT NULL,
  followed_user_id INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (follower_user_id, followed_user_id),
  FOREIGN KEY(follower_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(followed_user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS moderation_role_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  requester_user_id INTEGER NOT NULL,
  requested_role TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  reviewed_at INTEGER,
  reviewed_by INTEGER,
  review_note TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(requester_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(reviewed_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS moderation_reports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  target_type TEXT NOT NULL,
  target_id INTEGER NOT NULL,
  reporter_user_id INTEGER NOT NULL,
  reporter_role TEXT NOT NULL,
  content_author_user_id INTEGER NOT NULL,
  reason TEXT NOT NULL,
  note TEXT NOT NULL,
  status TEXT NOT NULL,
  route_to_roles TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  closed_at INTEGER,
  closed_by INTEGER,
  closed_by_role TEXT NOT NULL DEFAULT '',
  decision_reason TEXT NOT NULL DEFAULT '',
  decision_note TEXT NOT NULL DEFAULT '',
  linked_previous_decision_id INTEGER,
  FOREIGN KEY(reporter_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(content_author_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(closed_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS moderation_appeals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  target_type TEXT NOT NULL,
  target_id INTEGER NOT NULL,
  requester_user_id INTEGER NOT NULL,
  target_role TEXT NOT NULL,
  status TEXT NOT NULL,
  note TEXT NOT NULL,
  source_history_id INTEGER NOT NULL,
  linked_previous_decision_id INTEGER,
  created_at INTEGER NOT NULL,
  closed_at INTEGER,
  closed_by INTEGER,
  closed_by_role TEXT NOT NULL DEFAULT '',
  decision_note TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(requester_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(closed_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS moderation_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  acted_at INTEGER NOT NULL,
  action_type TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id INTEGER NOT NULL,
  content_author_user_id INTEGER NOT NULL DEFAULT 0,
  content_author_name TEXT NOT NULL DEFAULT '',
  actor_user_id INTEGER NOT NULL DEFAULT 0,
  actor_username TEXT NOT NULL DEFAULT '',
  actor_display_name TEXT NOT NULL DEFAULT '',
  actor_role TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  current_status TEXT NOT NULL DEFAULT '',
  route_to_role TEXT NOT NULL DEFAULT '',
  linked_previous_decision_id INTEGER,
  post_title_snapshot TEXT NOT NULL DEFAULT '',
  post_body_snapshot TEXT NOT NULL DEFAULT '',
  comment_body_snapshot TEXT NOT NULL DEFAULT ''
);
