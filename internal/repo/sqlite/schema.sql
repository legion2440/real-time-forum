CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  username TEXT NOT NULL UNIQUE,
  display_name TEXT,
  first_name TEXT NOT NULL DEFAULT '',
  last_name TEXT NOT NULL DEFAULT '',
  age INTEGER NOT NULL DEFAULT 0,
  gender TEXT NOT NULL DEFAULT '',
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
  created_at INTEGER NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(attachment_id) REFERENCES attachments(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  post_id INTEGER NOT NULL,
  parent_id INTEGER,
  user_id INTEGER NOT NULL,
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(parent_id) REFERENCES comments(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS categories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
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
  PRIMARY KEY (post_id, user_id),
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS comment_reactions (
  comment_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  value INTEGER NOT NULL,
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
