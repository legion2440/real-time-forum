const app = document.getElementById("app");
const THEME_KEY = "theme";
const DEBUG_WS = false;
const DM_HISTORY_PAGE_SIZE = 10;
const DM_HISTORY_THROTTLE_MS = 400;
const MAX_ATTACHMENT_BYTES = 20 * 1024 * 1024;
const ATTACHMENT_TOO_BIG_MESSAGE = "image is too big (max 20MB)";
const ATTACHMENT_INVALID_TYPE_MESSAGE = "Only JPEG/PNG/GIF allowed";
const DM_UNREAD_STORAGE_KEY_PREFIX = "forum:dm-unread:v1:";
const MAX_UNREAD_PEERS = 200;
const TYPING_IDLE_TIMEOUT_MS = 1500;
const TYPING_HEARTBEAT_MS = 2000;
const TYPING_REMOTE_TTL_MS = 5000;
const TYPING_CLEAR_DELAY_MS = 180;
const TYPING_SCOPE_DM = "dm";
const TYPING_SCOPE_POST = "post";
const TYPING_STATUS_START = "start";
const TYPING_STATUS_STOP = "stop";
const CENTER_NOTIFICATION_BUCKETS = ["all", "deleted", "reports", "appeals"];
const CENTER_MODERATION_TABS = ["queue", "reports", "history"];
const CENTER_MANAGEMENT_TABS = ["requests", "roles", "categories", "journal"];
const CENTER_DM_PREVIEW_LIMIT = 5;
const MODERATION_REASONS = ["irrelevant", "obscene", "illegal", "insulting", "other"];

const state = {
  user: null,
  users: [],
  usersLoaded: false,
  onlineUserIDs: new Set(),
  dmPeerID: "",
  dmPeers: [],
  dmPeersLoaded: false,
  dmMessages: [],
  dmUnreadByPeer: {},
  dmLoading: false,
  dmLoadingOlder: false,
  dmHasMore: false,
  dmOlderCursor: null,
  dmOlderLoadAt: 0,
  dmDraftAttachment: null,
  dmAttachmentUploading: false,
  dmReturnPath: "",
  postDraftAttachment: null,
  postAttachmentUploading: false,
  categories: [],
  filters: {
    cat: new Set(),
    mine: false,
    liked: false,
    q: "",
  },
  commentSearchByPost: {},
  pendingCommentReply: null,
  authSessionNoticeOpen: false,
  authProviders: [],
  authProvidersLoaded: false,
  editingCommentID: "",
  editingCommentDraft: "",
  center: createEmptyCenterState(),
  theme: getInitialTheme(),
};

const typingRuntime = {
  routeDMTargetID: "",
  routePostID: "",
  postSubscriptionID: "",
  localDM: createLocalTypingController(TYPING_SCOPE_DM),
  localPost: createLocalTypingController(TYPING_SCOPE_POST),
  remoteDMByPeer: new Map(),
  remotePostByPost: new Map(),
};

let realtimeSocket = null;

applyTheme(state.theme);

function createEmptyCenterState() {
  return {
    summaryLoaded: false,
    summary: {
      total: 0,
      dm: 0,
      myContent: 0,
      subscriptions: 0,
      deleted: 0,
      reports: 0,
      appeals: 0,
      management: 0,
    },
    activityLoaded: false,
    activity: {
      posts: [],
      postsHasMore: false,
      reactions: [],
      reactionsHasMore: false,
      comments: [],
      commentsHasMore: false,
    },
    notifications: CENTER_NOTIFICATION_BUCKETS.reduce((acc, bucket) => {
      acc[bucket] = {
        loaded: false,
        loading: false,
        items: [],
        hasMore: false,
      };
      return acc;
    }, {}),
    myReportsLoaded: false,
    myReportsLoading: false,
    myReports: [],
    appealsLoaded: false,
    appealsLoading: false,
    appeals: [],
    appealInboxLoaded: false,
    appealInboxLoading: false,
    appealInbox: [],
    moderation: {
      queueLoaded: false,
      queueLoading: false,
      queue: [],
      reportsLoaded: false,
      reportsLoading: false,
      reports: [],
      historyLoaded: false,
      historyLoading: false,
      history: [],
    },
    management: {
      requestsLoaded: false,
      requestsLoading: false,
      requests: [],
      categoriesLoaded: false,
      categoriesLoading: false,
      categories: [],
      journalLoaded: false,
      journalLoading: false,
      journal: [],
    },
  };
}

function resetCenterState() {
  state.center = createEmptyCenterState();
}

function debugWS(...args) {
  if (DEBUG_WS) console.log(...args);
}

function debugWSWarn(...args) {
  if (DEBUG_WS) console.warn(...args);
}

function normalizeUserID(value) {
  return String(value ?? "").trim();
}

function normalizeUsername(value) {
  return String(value ?? "").trim().replace(/^@+/, "");
}

function getCurrentUserID() {
  return normalizeUserID(state.user && state.user.id);
}

function getCurrentUsername() {
  return normalizeUsername(state.user && state.user.username);
}

function getProfilePath(username) {
  const normalized = normalizeUsername(username);
  return normalized ? `/u/${encodeURIComponent(normalized)}` : "/";
}

function getProfileSetupPath() {
  if (!state.user || !state.user.needsProfileSetup) return "";
  const username = getCurrentUsername();
  return username ? `${getProfilePath(username)}?setup=1` : "";
}

function maybeRedirectToProfileSetup() {
  const target = getProfileSetupPath();
  if (!target) return false;
  const current = `${location.pathname || "/"}${location.search || ""}`;
  if (current === target) return false;
  navigate(target);
  return true;
}

function getDisplayName(value) {
  if (!value || typeof value !== "object") return "";
  const candidates = [value.displayName, value.display_name, value.name];
  for (const candidate of candidates) {
    const normalized = String(candidate ?? "").trim();
    if (normalized) return normalized;
  }
  return "";
}

function getDisplayNameOrUsername(profile) {
  const displayName = getDisplayName(profile);
  const username = normalizeUsername(profile && profile.username);
  return displayName || username || "user";
}

function normalizeRole(value) {
  switch (String(value || "").trim().toLowerCase()) {
    case "guest":
      return "guest";
    case "moderator":
      return "moderator";
    case "admin":
      return "admin";
    case "owner":
      return "owner";
    case "user":
    default:
      return "user";
  }
}

function normalizeBadges(value) {
  if (!Array.isArray(value)) return [];
  return value
    .map((entry) => String(entry || "").trim().toLowerCase())
    .filter((entry, index, items) => entry && items.indexOf(entry) === index);
}

function getCurrentUserRole() {
  return normalizeRole(state.user && state.user.role);
}

function getRoleLevel(role) {
  switch (normalizeRole(role)) {
    case "owner":
      return 4;
    case "admin":
      return 3;
    case "moderator":
      return 2;
    case "user":
      return 1;
    default:
      return 0;
  }
}

function isStaffRole(role) {
  return getRoleLevel(role) >= 2;
}

function isAdminRole(role) {
  return getRoleLevel(role) >= 3;
}

function isOwnerRole(role) {
  return normalizeRole(role) === "owner";
}

function humanizeRole(role) {
  switch (normalizeRole(role)) {
    case "moderator":
      return "Moderator";
    case "admin":
      return "Admin";
    case "owner":
      return "Owner";
    case "guest":
      return "Guest";
    default:
      return "User";
  }
}

function getUserRole(entity) {
  if (!entity || typeof entity !== "object") return "user";
  if (entity.author && typeof entity.author === "object" && entity.author.role) {
    return normalizeRole(entity.author.role);
  }
  return normalizeRole(entity.role);
}

function getUserBadges(entity) {
  if (!entity || typeof entity !== "object") return [];
  if (entity.author && typeof entity.author === "object" && Array.isArray(entity.author.badges)) {
    return normalizeBadges(entity.author.badges);
  }
  return normalizeBadges(entity.badges);
}

function renderStaffBadges(badges) {
  const items = normalizeBadges(badges);
  if (!items.length) return "";
  return `
    <span class="staff-badge-row">
      ${items
        .map((badge) => `<span class="staff-badge staff-badge-${escapeHTML(badge)}">${escapeHTML(badge)}</span>`)
        .join("")}
    </span>
  `;
}

function truncateInline(value, maxLength) {
  const text = String(value || "").trim().replace(/\s+/g, " ");
  if (!text || !Number.isFinite(maxLength) || maxLength <= 0 || text.length <= maxLength) return text;
  return `${text.slice(0, Math.max(1, maxLength - 3)).trimEnd()}...`;
}

function normalizePersonRecord(user) {
  if (!user || typeof user !== "object") return null;
  const id = normalizeUserID(user.id);
  const username = normalizeUsername(user.username);
  if (!id && !username) return null;
  return {
    ...user,
    id: id || normalizeUserID(user.user_id),
    username,
    displayName: String(user.displayName ?? user.display_name ?? "").trim(),
    role: normalizeRole(user.role),
    badges: normalizeBadges(user.badges),
  };
}

function normalizeAttachment(attachment) {
  if (!attachment || typeof attachment !== "object") return null;
  const id = normalizeUserID(attachment.id);
  const url = String(attachment.url || "").trim();
  const mime = String(attachment.mime || "").trim();
  const size = Number(attachment.size || 0);
  if (!id || !url || !mime) return null;
  return {
    id,
    url,
    mime,
    size: Number.isFinite(size) && size > 0 ? size : 0,
  };
}

function getAttachmentNumericID(attachment) {
  const id = Number.parseInt(normalizeUserID(attachment && attachment.id), 10);
  return Number.isFinite(id) && id > 0 ? id : null;
}

function getDMUnreadStorageKey(userID = getCurrentUserID()) {
  const id = normalizeUserID(userID);
  return state.user && id ? `${DM_UNREAD_STORAGE_KEY_PREFIX}${id}` : "";
}

function normalizeDMUnreadMap(value) {
  if (!value || typeof value !== "object") return {};

  const entries = Object.entries(value).reduce((acc, [peerID, unread]) => {
    const normalizedPeerID = normalizeUserID(peerID);
    const count = Math.max(0, Math.trunc(Number(unread) || 0));
    if (!normalizedPeerID || count <= 0) return acc;
    acc.push([normalizedPeerID, count]);
    return acc;
  }, []);

  entries.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0], undefined, { numeric: true, sensitivity: "base" }));

  return entries.slice(0, MAX_UNREAD_PEERS).reduce((acc, [peerID, unread]) => {
    acc[peerID] = unread;
    return acc;
  }, {});
}

function clearPersistedDMUnreadState(userID = getCurrentUserID()) {
  const key = getDMUnreadStorageKey(userID);
  if (!key) return;

  try {
    localStorage.removeItem(key);
  } catch (_) {
    // ignore storage errors
  }
}

function persistDMUnreadState(userID = getCurrentUserID()) {
  const key = getDMUnreadStorageKey(userID);
  if (!key) return;

  try {
    const payload = normalizeDMUnreadMap(state.dmUnreadByPeer);
    state.dmUnreadByPeer = payload;
    if (Object.keys(payload).length === 0) {
      clearPersistedDMUnreadState(userID);
      return;
    }
    localStorage.setItem(key, JSON.stringify(payload));
  } catch (_) {
    // ignore storage errors
  }
}

function loadPersistedDMUnreadState(userID) {
  const key = getDMUnreadStorageKey(userID);
  if (!key) return {};

  try {
    const raw = localStorage.getItem(key);
    if (!raw) return {};
    return normalizeDMUnreadMap(JSON.parse(raw));
  } catch (_) {
    return {};
  }
}

function syncDMUnreadCache(nextUnreadByPeer) {
  state.dmUnreadByPeer = normalizeDMUnreadMap(nextUnreadByPeer);
  persistDMUnreadState();
  syncNotificationButton();
}

function setDMPeerUnreadCount(peerID, unreadCount) {
  const id = normalizeUserID(peerID);
  if (!id) return;

  const nextUnreadByPeer = { ...(state.dmUnreadByPeer || {}) };
  const nextCount = Math.max(0, Math.trunc(Number(unreadCount) || 0));
  if (nextCount > 0) {
    nextUnreadByPeer[id] = nextCount;
  } else {
    delete nextUnreadByPeer[id];
  }

  syncDMUnreadCache(nextUnreadByPeer);
}

function normalizeNotificationSummary(value) {
  if (!value || typeof value !== "object") {
    return { total: 0, dm: 0, myContent: 0, subscriptions: 0, deleted: 0, reports: 0, appeals: 0, management: 0 };
  }
  const total = Math.max(0, Math.trunc(Number(value.total ?? value.Total ?? 0) || 0));
  const dm = Math.max(0, Math.trunc(Number(value.dm ?? value.DM ?? 0) || 0));
  const myContent = Math.max(0, Math.trunc(Number(value.myContent ?? value.my_content ?? value.MyContent ?? 0) || 0));
  const subscriptions = Math.max(0, Math.trunc(Number(value.subscriptions ?? value.Subscriptions ?? 0) || 0));
  const deleted = Math.max(0, Math.trunc(Number(value.deleted ?? value.Deleted ?? 0) || 0));
  const reports = Math.max(0, Math.trunc(Number(value.reports ?? value.Reports ?? 0) || 0));
  const appeals = Math.max(0, Math.trunc(Number(value.appeals ?? value.Appeals ?? 0) || 0));
  const management = Math.max(0, Math.trunc(Number(value.management ?? value.Management ?? 0) || 0));
  return {
    total,
    dm,
    myContent,
    subscriptions,
    deleted,
    reports,
    appeals,
    management,
  };
}

function setNotificationSummary(summary) {
  state.center.summary = normalizeNotificationSummary(summary);
  state.center.summaryLoaded = true;
  syncNotificationButton();
}

function getNotificationBucketUnreadCount(bucket) {
  const summary = state.center && state.center.summary ? state.center.summary : normalizeNotificationSummary();
  switch (String(bucket || "").trim()) {
    case "all":
      return Number(summary.total || 0);
    case "deleted":
      return Number(summary.deleted || 0);
    case "reports":
      return Number(summary.reports || 0);
    case "appeals":
      return Number(summary.appeals || 0);
    default:
      return 0;
  }
}

function getTotalUnreadCount() {
  const summary = state.center && state.center.summary ? state.center.summary : null;
  return summary ? Math.max(0, Number(summary.total || 0)) : 0;
}

function getNotificationsLabel(totalUnread = getTotalUnreadCount()) {
  return totalUnread > 0 ? `Notifications (${totalUnread} unread)` : "Notifications";
}

function getAttachmentErrorMessage(err) {
  const status = Number(err && err.status);
  const message = String((err && err.message) || "").trim();
  if (status === 413 || message.toLowerCase() === ATTACHMENT_TOO_BIG_MESSAGE) return ATTACHMENT_TOO_BIG_MESSAGE;
  if (message === "invalid image type" || message === ATTACHMENT_INVALID_TYPE_MESSAGE) return ATTACHMENT_INVALID_TYPE_MESSAGE;
  return message || "Failed to upload image.";
}

function getProfileAgeValue(profile) {
  const age = Number(profile && profile.age);
  if (!Number.isFinite(age) || age <= 0) return "";
  return String(age);
}

function getProfileFieldValue(profile, key) {
  return String((profile && profile[key]) || "").trim();
}

function getSearchParam(name, search = location.search) {
  return new URLSearchParams(String(search || "")).get(name) || "";
}

function getAuthProviders() {
  return Array.isArray(state.authProviders) ? state.authProviders : [];
}

function getProviderLabel(providerName) {
  const normalized = String(providerName || "").trim().toLowerCase();
  const match = getAuthProviders().find((provider) => String(provider && provider.name || "").trim().toLowerCase() === normalized);
  if (match && String(match.label || "").trim()) return String(match.label || "").trim();
  switch (normalized) {
    case "google":
      return "Google";
    case "github":
      return "GitHub";
    case "facebook":
      return "Facebook";
    default:
      return normalized || "Provider";
  }
}

function providerBadge(providerName) {
  const normalized = String(providerName || "").trim().toLowerCase();
  switch (normalized) {
    case "google":
      return "G";
    case "github":
      return "GH";
    case "facebook":
      return "f";
    default:
      return normalized.slice(0, 2).toUpperCase() || "?";
  }
}

function getOAuthStartPath(providerName, options = {}) {
  const provider = encodeURIComponent(String(providerName || "").trim().toLowerCase());
  const params = new URLSearchParams();
  if (options.intent) params.set("intent", String(options.intent).trim());
  if (options.nextPath) params.set("next", String(options.nextPath).trim());
  return `/auth/${provider}/login${params.toString() ? `?${params.toString()}` : ""}`;
}

function getLinkedAccounts() {
  return Array.isArray(state.user && state.user.linkedAccounts) ? state.user.linkedAccounts : [];
}

function getLinkedAccountStatus(providerName) {
  const normalized = String(providerName || "").trim().toLowerCase();
  return getLinkedAccounts().find((item) => String(item && item.provider || "").trim().toLowerCase() === normalized) || null;
}

function getProfileStatusNotice() {
  const linked = getSearchParam("linked");
  if (linked) return `${getProviderLabel(linked)} account linked.`;
  if (getSearchParam("merged") === "1") return "Accounts merged successfully.";
  const authError = getSearchParam("authError");
  if (authError) return authError;
  return "";
}

function getAuthStatusNotice() {
  const authError = getSearchParam("authError");
  if (authError) return authError;
  if (getSearchParam("linked")) return `${getProviderLabel(getSearchParam("linked"))} account linked.`;
  return "";
}

function renderOAuthButtons(options = {}) {
  const providers = getAuthProviders().filter((provider) => provider && provider.enabled);
  if (!providers.length) return "";

  const intent = String(options.intent || "login").trim();
  const nextPath = String(options.nextPath || "").trim();
  const title = String(options.title || "Continue with").trim();

  return `
    <div class="oauth-block">
      <div class="oauth-separator"><span>${escapeHTML(title)}</span></div>
      <div class="oauth-grid">
        ${providers.map((provider) => `
          <a class="btn btn-ghost oauth-btn" href="${escapeHTML(getOAuthStartPath(provider.name, { intent, nextPath }))}">
            <span class="oauth-badge oauth-badge-${escapeHTML(String(provider.name || "").trim().toLowerCase())}">${escapeHTML(providerBadge(provider.name))}</span>
            <span>Continue with ${escapeHTML(provider.label || getProviderLabel(provider.name))}</span>
          </a>
        `).join("")}
      </div>
    </div>
  `;
}

function renderLinkedAccountsPanel(username) {
  const linkedAccounts = getLinkedAccounts();
  const enabledProviders = getAuthProviders().filter((provider) => provider && provider.enabled);
  const providerMap = new Map(enabledProviders.map((provider) => [String(provider.name || "").trim().toLowerCase(), provider]));
  linkedAccounts.forEach((item) => {
    const provider = String(item && item.provider || "").trim().toLowerCase();
    if (!providerMap.has(provider)) {
      providerMap.set(provider, { name: provider, label: getProviderLabel(provider), enabled: true });
    }
  });

  const rows = Array.from(providerMap.values())
    .sort((a, b) => String(a.name || "").localeCompare(String(b.name || "")))
    .map((provider) => {
      const status = getLinkedAccountStatus(provider.name);
      const linked = Boolean(status && status.linked);
      const nextPath = getProfilePath(username);
      return `
        <div class="linked-account-row">
          <div class="linked-account-main">
            <div class="linked-account-name">
              <span class="oauth-badge oauth-badge-${escapeHTML(String(provider.name || "").trim().toLowerCase())}">${escapeHTML(providerBadge(provider.name))}</span>
              <strong>${escapeHTML(provider.label || getProviderLabel(provider.name))}</strong>
            </div>
            <div class="linked-account-meta">
              <span class="status-pill ${linked ? "is-linked" : ""}">${linked ? "linked" : "not linked"}</span>
              ${linked && status && status.email ? `<span>${escapeHTML(status.email)}</span>` : ""}
            </div>
          </div>
          <div class="linked-account-actions">
            ${
              linked
                ? `<button class="btn btn-ghost btn-compact" type="button" data-action="unlink-provider" data-provider="${escapeHTML(provider.name)}" ${status && status.canUnlink ? "" : "disabled"}>${status && status.canUnlink ? "Unlink" : "Protected"}</button>`
                : `<a class="btn btn-primary btn-compact" href="${escapeHTML(getOAuthStartPath(provider.name, { intent: "link", nextPath }))}">Link</a>`
            }
          </div>
        </div>
      `;
    })
    .join("");

  return `
    <section class="surface form-card linked-accounts-card">
      <div class="section-row">
        <h2>Linked accounts</h2>
        <p>Local auth stays available. External providers can be linked or unlinked safely.</p>
      </div>
      <div class="linked-accounts-list">
        ${rows || '<div class="side-note">No OAuth providers configured.</div>'}
      </div>
      <form id="local-merge-form" class="form-stack linked-accounts-merge-form">
        <div class="form-intro">
          <h2>Merge existing local account</h2>
          <p>Explicitly confirm the local account you want to merge into this profile.</p>
        </div>
        <label class="field"><span>Email or username</span><input type="text" name="login" required /></label>
        <label class="field"><span>Password</span><input type="password" name="password" required /></label>
        <div class="form-actions">
          <button class="btn btn-primary" type="submit">Review merge</button>
        </div>
        <div id="local-merge-error"></div>
      </form>
    </section>
  `;
}

function renderProfileField(label, value) {
  const content = String(value || "").trim() || "Not set";
  return `
    <div class="profile-field-row">
      <span class="profile-field-label">${escapeHTML(label)}</span>
      <strong>${escapeHTML(content)}</strong>
    </div>
  `;
}

function renderLinkedProviderPills(linkedAccounts) {
  if (!Array.isArray(linkedAccounts) || !linkedAccounts.length) {
    return '<span class="side-note">No linked providers</span>';
  }

  return linkedAccounts
    .map((item) => `
      <span class="provider-pill">
        <span class="oauth-badge oauth-badge-${escapeHTML(String(item.provider || "").trim().toLowerCase())}">${escapeHTML(providerBadge(item.provider))}</span>
        <span>${escapeHTML(getProviderLabel(item.provider))}</span>
      </span>
    `)
    .join("");
}

function renderFlowUserSummaryCard(title, account) {
  if (!account || typeof account !== "object") return "";
  return `
    <div class="flow-summary-card">
      <div class="flow-summary-head">
        <strong>${escapeHTML(title)}</strong>
        <span class="status-pill ${account.hasPassword ? "is-linked" : ""}">${account.hasPassword ? "local password" : "social only"}</span>
      </div>
      <div class="flow-summary-grid">
        ${renderProfileField("Display name", account.displayName || account.username || "user")}
        ${renderProfileField("Username", `@${normalizeUsername(account.username)}`)}
        ${renderProfileField("Email", account.email || "Not set")}
      </div>
      <div class="provider-pill-row">
        ${renderLinkedProviderPills(account.linkedAccounts)}
      </div>
    </div>
  `;
}

function getUserByID(userID) {
  const id = normalizeUserID(userID);
  return (state.users || []).find((user) => normalizeUserID(user && user.id) === id) || null;
}

function getUserUsername(userID) {
  const user = getUserByID(userID);
  return normalizeUsername(user && user.username);
}

function getUserDisplayName(userID) {
  const user = getUserByID(userID);
  if (!user) return `user-${normalizeUserID(userID)}`;
  return getDisplayNameOrUsername(user);
}

function getActiveDMPeerIDFromPath(pathname = location.pathname) {
  const match = String(pathname || "").match(/^\/dm\/([^/?#]+)/);
  return match ? normalizeUserID(match[1]) : "";
}

function getActiveProfileUsernameFromPath(pathname = location.pathname) {
  const match = String(pathname || "").match(/^\/u\/([^/?#]+)/);
  return match ? normalizeUsername(decodeURIComponent(match[1])) : "";
}

function isDMRoute(pathname = location.pathname) {
  return /^\/dm(?:\/|$)/.test(String(pathname || ""));
}

function createLocalTypingController(scope) {
  return {
    scope: normalizeTypingScope(scope),
    targetID: "",
    active: false,
    idleTimer: 0,
    heartbeatTimer: 0,
  };
}

function normalizeTypingScope(value) {
  const scope = String(value || "")
    .trim()
    .toLowerCase();
  if (scope === TYPING_SCOPE_DM || scope === TYPING_SCOPE_POST) return scope;
  return "";
}

function getLocalTypingController(scope) {
  const normalizedScope = normalizeTypingScope(scope);
  if (normalizedScope === TYPING_SCOPE_DM) return typingRuntime.localDM;
  if (normalizedScope === TYPING_SCOPE_POST) return typingRuntime.localPost;
  return null;
}

function sendRealtimeJSON(payload) {
  if (!realtimeSocket || realtimeSocket.readyState !== WebSocket.OPEN) return false;
  try {
    realtimeSocket.send(JSON.stringify(payload));
    return true;
  } catch (err) {
    debugWSWarn("ws send failed", err);
    return false;
  }
}

function sendTypingEvent(type, scope, targetID) {
  const normalizedScope = normalizeTypingScope(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  if (!normalizedScope || !normalizedTargetID) return false;
  return sendRealtimeJSON({
    type,
    scope: normalizedScope,
    targetId: normalizedTargetID,
  });
}

function clearLocalTypingController(controller) {
  if (!controller) return;
  if (controller.idleTimer) {
    clearTimeout(controller.idleTimer);
    controller.idleTimer = 0;
  }
  if (controller.heartbeatTimer) {
    clearInterval(controller.heartbeatTimer);
    controller.heartbeatTimer = 0;
  }
  controller.active = false;
  controller.targetID = "";
}

function scheduleLocalTypingIdleStop(controller) {
  if (!controller || !controller.active) return;
  if (controller.idleTimer) {
    clearTimeout(controller.idleTimer);
  }
  controller.idleTimer = setTimeout(() => {
    stopLocalTyping(controller.scope, { send: true });
  }, TYPING_IDLE_TIMEOUT_MS);
}

function ensureLocalTypingHeartbeat(controller) {
  if (!controller || !controller.active || controller.heartbeatTimer) return;
  controller.heartbeatTimer = setInterval(() => {
    if (!controller.active || !controller.targetID) return;
    if (!sendTypingEvent("typing:heartbeat", controller.scope, controller.targetID)) {
      clearLocalTypingController(controller);
    }
  }, TYPING_HEARTBEAT_MS);
}

function startLocalTyping(scope, targetID) {
  if (!state.user) return;
  const controller = getLocalTypingController(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  if (!controller || !normalizedTargetID) return;

  if (controller.targetID && controller.targetID !== normalizedTargetID) {
    stopLocalTyping(controller.scope, { send: true });
  }

  controller.targetID = normalizedTargetID;
  if (!controller.active) {
    if (!sendTypingEvent("typing:start", controller.scope, normalizedTargetID)) {
      return;
    }
    controller.active = true;
  }

  scheduleLocalTypingIdleStop(controller);
  ensureLocalTypingHeartbeat(controller);
}

function stopLocalTyping(scope, options = {}) {
  const controller = getLocalTypingController(scope);
  if (!controller) return;

  const normalizedTargetID = normalizeUserID(options.targetID || controller.targetID);
  const shouldSend = options.send !== false;
  if (shouldSend && controller.active && normalizedTargetID) {
    sendTypingEvent("typing:stop", controller.scope, normalizedTargetID);
  }

  clearLocalTypingController(controller);
}

function stopAllLocalTyping(sendStop = false) {
  stopLocalTyping(TYPING_SCOPE_DM, { send: sendStop });
  stopLocalTyping(TYPING_SCOPE_POST, { send: sendStop });
}

function getRemoteTypingMap(scope) {
  const normalizedScope = normalizeTypingScope(scope);
  if (normalizedScope === TYPING_SCOPE_DM) return typingRuntime.remoteDMByPeer;
  if (normalizedScope === TYPING_SCOPE_POST) return typingRuntime.remotePostByPost;
  return null;
}

function getRemoteTypingCollection(scope, targetID, create = false) {
  const map = getRemoteTypingMap(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  if (!map || !normalizedTargetID) return null;
  if (!map.has(normalizedTargetID) && create) {
    map.set(normalizedTargetID, new Map());
  }
  return map.get(normalizedTargetID) || null;
}

function clearRemoteTypingEntryTimers(entry) {
  if (!entry) return;
  if (entry.expireTimer) {
    clearTimeout(entry.expireTimer);
    entry.expireTimer = 0;
  }
  if (entry.removeTimer) {
    clearTimeout(entry.removeTimer);
    entry.removeTimer = 0;
  }
}

function finalizeRemoteTypingRemoval(scope, targetID, userID, expectedEntry) {
  const map = getRemoteTypingMap(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  const normalizedUserID = normalizeUserID(userID);
  if (!map || !normalizedTargetID || !normalizedUserID) return;

  const collection = map.get(normalizedTargetID);
  if (!collection) return;
  const currentEntry = collection.get(normalizedUserID);
  if (!currentEntry || (expectedEntry && currentEntry !== expectedEntry)) return;

  clearRemoteTypingEntryTimers(currentEntry);
  collection.delete(normalizedUserID);
  if (collection.size === 0) {
    map.delete(normalizedTargetID);
  }

  if (scope === TYPING_SCOPE_DM) {
    syncDMTypingIndicator();
    return;
  }
  syncPostTypingIndicator(normalizedTargetID);
}

function setRemoteTypingEntry(scope, targetID, userID, userName) {
  const normalizedScope = normalizeTypingScope(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  const normalizedUserID = normalizeUserID(userID);
  if (!normalizedScope || !normalizedTargetID || !normalizedUserID) return;

  const collection = getRemoteTypingCollection(normalizedScope, normalizedTargetID, true);
  if (!collection) return;

  const entry = collection.get(normalizedUserID) || {
    id: normalizedUserID,
    name: userName,
    expireTimer: 0,
    removeTimer: 0,
  };
  clearRemoteTypingEntryTimers(entry);
  entry.name = String(userName || "").trim() || getUserDisplayName(normalizedUserID);
  entry.expireTimer = setTimeout(() => {
    finalizeRemoteTypingRemoval(normalizedScope, normalizedTargetID, normalizedUserID, entry);
  }, TYPING_REMOTE_TTL_MS);

  collection.set(normalizedUserID, entry);
  if (normalizedScope === TYPING_SCOPE_DM) {
    syncDMTypingIndicator();
    return;
  }
  syncPostTypingIndicator(normalizedTargetID);
}

function removeRemoteTypingEntry(scope, targetID, userID, delayMs = 0) {
  const normalizedScope = normalizeTypingScope(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  const normalizedUserID = normalizeUserID(userID);
  if (!normalizedScope || !normalizedTargetID || !normalizedUserID) return;

  const collection = getRemoteTypingCollection(normalizedScope, normalizedTargetID, false);
  if (!collection) return;

  const entry = collection.get(normalizedUserID);
  if (!entry) return;

  if (entry.expireTimer) {
    clearTimeout(entry.expireTimer);
    entry.expireTimer = 0;
  }
  if (entry.removeTimer) {
    clearTimeout(entry.removeTimer);
    entry.removeTimer = 0;
  }

  if (delayMs > 0) {
    entry.removeTimer = setTimeout(() => {
      finalizeRemoteTypingRemoval(normalizedScope, normalizedTargetID, normalizedUserID, entry);
    }, delayMs);
    return;
  }

  finalizeRemoteTypingRemoval(normalizedScope, normalizedTargetID, normalizedUserID, entry);
}

function clearRemoteTypingScopeTarget(scope, targetID) {
  const map = getRemoteTypingMap(scope);
  const normalizedTargetID = normalizeUserID(targetID);
  if (!map || !normalizedTargetID) return;

  const collection = map.get(normalizedTargetID);
  if (!collection) return;

  collection.forEach((entry) => {
    clearRemoteTypingEntryTimers(entry);
  });
  map.delete(normalizedTargetID);

  if (normalizeTypingScope(scope) === TYPING_SCOPE_DM) {
    syncDMTypingIndicator();
    return;
  }
  syncPostTypingIndicator(normalizedTargetID);
}

function clearAllRemoteTypingState() {
  typingRuntime.remoteDMByPeer.forEach((collection) => {
    collection.forEach((entry) => {
      clearRemoteTypingEntryTimers(entry);
    });
  });
  typingRuntime.remotePostByPost.forEach((collection) => {
    collection.forEach((entry) => {
      clearRemoteTypingEntryTimers(entry);
    });
  });

  typingRuntime.remoteDMByPeer.clear();
  typingRuntime.remotePostByPost.clear();
  syncDMTypingIndicator();
  syncPostTypingIndicator();
}

function syncActivePostTypingSubscription(postID, force = false) {
  const normalizedPostID = normalizeUserID(postID);
  const previousPostID = normalizeUserID(typingRuntime.postSubscriptionID);

  if (!normalizedPostID) {
    if (previousPostID) {
      unsubscribePostTyping(previousPostID);
    }
    typingRuntime.postSubscriptionID = "";
    return;
  }

  if (previousPostID && previousPostID !== normalizedPostID) {
    unsubscribePostTyping(previousPostID);
  }

  typingRuntime.postSubscriptionID = normalizedPostID;
  if (!force && previousPostID === normalizedPostID) return;
  sendTypingEvent("typing:subscribe", TYPING_SCOPE_POST, normalizedPostID);
}

function unsubscribePostTyping(postID = typingRuntime.postSubscriptionID) {
  const normalizedPostID = normalizeUserID(postID);
  if (!normalizedPostID) return;
  sendTypingEvent("typing:unsubscribe", TYPING_SCOPE_POST, normalizedPostID);
  if (normalizeUserID(typingRuntime.postSubscriptionID) === normalizedPostID) {
    typingRuntime.postSubscriptionID = "";
  }
}

function handleTypingRouteTransition(nextPath = location.pathname) {
  const nextDMPeerID = getActiveDMPeerIDFromPath(nextPath);
  const nextPostID = normalizeUserID(getActivePostIDFromPath(nextPath) || "");
  const previousDMPeerID = normalizeUserID(typingRuntime.routeDMTargetID);
  const previousPostID = normalizeUserID(typingRuntime.routePostID);

  if (previousDMPeerID && previousDMPeerID !== nextDMPeerID) {
    stopLocalTyping(TYPING_SCOPE_DM, { send: true });
    clearRemoteTypingScopeTarget(TYPING_SCOPE_DM, previousDMPeerID);
  }

  if (previousPostID && previousPostID !== nextPostID) {
    stopLocalTyping(TYPING_SCOPE_POST, { send: true, targetID: previousPostID });
    unsubscribePostTyping(previousPostID);
    clearRemoteTypingScopeTarget(TYPING_SCOPE_POST, previousPostID);
  }

  typingRuntime.routeDMTargetID = nextDMPeerID;
  typingRuntime.routePostID = nextPostID;
}

function getTypingUserName(userID, value) {
  const normalized = String(value || "").trim();
  if (normalized) return normalized;
  return getUserDisplayName(userID);
}

function handleTypingUpdate(payload) {
  const scope = normalizeTypingScope(payload && payload.scope);
  const status = String((payload && payload.status) || "")
    .trim()
    .toLowerCase();
  const targetID = normalizeUserID(payload && payload.targetId);
  const userID = normalizeUserID(payload && payload.user && payload.user.id);
  if (!scope || !targetID || !userID || userID === getCurrentUserID()) return;
  if (status !== TYPING_STATUS_START && status !== TYPING_STATUS_STOP) return;

  const userName = getTypingUserName(userID, payload && payload.user && payload.user.name);

  if (scope === TYPING_SCOPE_DM) {
    if (targetID !== getCurrentUserID()) return;
    const activePeerID = normalizeUserID(state.dmPeerID);
    if (!activePeerID || activePeerID !== userID) return;

    if (status === TYPING_STATUS_START) {
      setRemoteTypingEntry(scope, activePeerID, userID, userName);
    } else {
      removeRemoteTypingEntry(scope, activePeerID, userID, TYPING_CLEAR_DELAY_MS);
    }
    return;
  }

  const activePostID = normalizeUserID(getActivePostIDFromPath() || "");
  if (!activePostID || activePostID !== targetID) return;
  if (status === TYPING_STATUS_START) {
    setRemoteTypingEntry(scope, activePostID, userID, userName);
  } else {
    removeRemoteTypingEntry(scope, activePostID, userID, TYPING_CLEAR_DELAY_MS);
  }
}

function getSortedRemoteTypers(scope, targetID) {
  const collection = getRemoteTypingCollection(scope, targetID, false);
  if (!collection || collection.size === 0) return [];
  return Array.from(collection.values()).sort((a, b) =>
    String(a.name || "").localeCompare(String(b.name || ""), undefined, { sensitivity: "base", numeric: true })
  );
}

function getDMTypingLabel(peerID = state.dmPeerID) {
  const typers = getSortedRemoteTypers(TYPING_SCOPE_DM, peerID);
  if (typers.length === 0) return "";
  return `${typers[0].name} is typing`;
}

function getPostTypingLabel(postID = getActivePostIDFromPath()) {
  const typers = getSortedRemoteTypers(TYPING_SCOPE_POST, postID);
  if (typers.length === 0) return "";
  if (typers.length === 1) {
    return `${typers[0].name} is typing`;
  }
  const othersCount = typers.length - 1;
  return `${typers[0].name} and ${othersCount} other${othersCount === 1 ? "" : "s"} are typing`;
}

function renderTypingIndicator(label) {
  const text = String(label || "").trim();
  if (!text) return "";
  return `
    <div class="typing-indicator" role="status" aria-live="polite">
      <span class="typing-indicator-text">${escapeHTML(text)}</span>
      <span class="typing-dots" aria-hidden="true">
        <span></span><span></span><span></span>
      </span>
    </div>
  `;
}

function syncDMTypingIndicator() {
  const slot = document.getElementById("dm-typing-indicator");
  if (!slot) return;
  slot.innerHTML = renderTypingIndicator(getDMTypingLabel(state.dmPeerID));
}

function syncPostTypingIndicator(postID = getActivePostIDFromPath()) {
  const slot = document.getElementById("post-typing-indicator");
  if (!slot) return;
  slot.innerHTML = renderTypingIndicator(getPostTypingLabel(postID));
}

function handleRealtimeDisconnected() {
  stopAllLocalTyping(false);
  clearAllRemoteTypingState();
}

function clearDMState() {
  state.dmPeerID = "";
  state.dmPeers = [];
  state.dmPeersLoaded = false;
  state.dmMessages = [];
  state.dmUnreadByPeer = {};
  state.dmLoading = false;
  state.dmLoadingOlder = false;
  state.dmHasMore = false;
  state.dmOlderCursor = null;
  state.dmOlderLoadAt = 0;
  state.dmDraftAttachment = null;
  state.dmAttachmentUploading = false;
  state.dmReturnPath = "";
  syncNotificationButton();
  syncDMView();
  syncDMPeersPanel();
}

function clearDMConversationState(preserveUnread = true) {
  state.dmPeerID = "";
  state.dmMessages = [];
  state.dmLoading = false;
  state.dmLoadingOlder = false;
  state.dmHasMore = false;
  state.dmOlderCursor = null;
  state.dmOlderLoadAt = 0;
  state.dmDraftAttachment = null;
  state.dmAttachmentUploading = false;
  state.dmReturnPath = "";
  if (!preserveUnread) {
    state.dmUnreadByPeer = {};
    persistDMUnreadState();
  }
  syncNotificationButton();
  syncDMView();
  syncDMPeersPanel();
}

function clearPresenceState() {
  state.users = [];
  state.usersLoaded = false;
  state.onlineUserIDs = new Set();
  syncPresencePanel();
}

function clearAuthenticatedState() {
  stopAllLocalTyping(false);
  clearAllRemoteTypingState();
  typingRuntime.routeDMTargetID = "";
  typingRuntime.routePostID = "";
  typingRuntime.postSubscriptionID = "";
  state.user = null;
  state.filters.mine = false;
  state.filters.liked = false;
  state.postDraftAttachment = null;
  state.postAttachmentUploading = false;
  state.editingCommentID = "";
  state.editingCommentDraft = "";
  resetCenterState();
  clearDMState();
  clearPresenceState();
  closeRealtimeSocket();
}

function closeRealtimeSocket() {
  if (!realtimeSocket) return;
  const socket = realtimeSocket;
  realtimeSocket = null;
  handleRealtimeDisconnected();
  if (socket.readyState === WebSocket.CLOSED || socket.readyState === WebSocket.CLOSING) return;
  socket.close();
}

function ensureRealtimeSocket() {
  if (!state.user) {
    closeRealtimeSocket();
    return;
  }
  if (typeof WebSocket === "undefined") return;
  if (realtimeSocket && (realtimeSocket.readyState === WebSocket.OPEN || realtimeSocket.readyState === WebSocket.CONNECTING)) {
    return;
  }

  const socket = new WebSocket((location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/ws");
  realtimeSocket = socket;

  socket.onopen = () => {
    if (realtimeSocket !== socket) return;
    debugWS("ws connected");
    const activePostID = normalizeUserID(getActivePostIDFromPath() || "");
    if (activePostID) {
      syncActivePostTypingSubscription(activePostID, true);
    }
  };

  socket.onmessage = (event) => {
    if (realtimeSocket !== socket) return;
    try {
      const payload = JSON.parse(event.data);
      handleRealtimeMessage(payload);
    } catch (err) {
      debugWSWarn("ws invalid message", err);
    }
  };

  socket.onclose = () => {
    if (realtimeSocket === socket) {
      realtimeSocket = null;
    }
    debugWS("ws closed");
    handleRealtimeDisconnected();
  };
}

function getInitialTheme() {
  try {
    const stored = localStorage.getItem(THEME_KEY);
    if (stored === "light" || stored === "dark") return stored;
  } catch (_) {
    // ignore
  }
  if (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) {
    return "dark";
  }
  return "light";
}

function applyTheme(theme) {
  document.documentElement.setAttribute("data-theme", theme === "dark" ? "dark" : "light");
}

function setTheme(theme) {
  state.theme = theme === "dark" ? "dark" : "light";
  applyTheme(state.theme);
  try {
    localStorage.setItem(THEME_KEY, state.theme);
  } catch (_) {
    // ignore
  }
}

function toggleTheme() {
  setTheme(state.theme === "dark" ? "light" : "dark");
  router();
}

function escapeHTML(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function formatDate(value) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return "";
  return d.toLocaleString();
}

function formatCompactCount(value) {
  const n = Number(value ?? 0);
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) return `${(n / 1000).toFixed(n >= 10_000 ? 0 : 1).replace(/\.0$/, "")}k`;
  if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1).replace(/\.0$/, "")}m`;
  return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, "")}b`;
}

function formatAttachmentSize(value) {
  const size = Number(value ?? 0);
  if (!Number.isFinite(size) || size <= 0) return "0 B";
  if (size < 1024) return `${Math.round(size)} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(size >= 10 * 1024 ? 0 : 1).replace(/\.0$/, "")} KB`;
  return `${(size / (1024 * 1024)).toFixed(size >= 10 * 1024 * 1024 ? 0 : 1).replace(/\.0$/, "")} MB`;
}

function avatarMarkup(name, avatarUrl, size = "md") {
  const safeName = escapeHTML(name || "User");
  const initial = escapeHTML((String(name || "?").trim()[0] || "?").toUpperCase());
  if (avatarUrl) {
    return `<span class="avatar avatar-${size}"><img src="${escapeHTML(avatarUrl)}" alt="${safeName}" /></span>`;
  }
  return `<span class="avatar avatar-${size} avatar-fallback" aria-label="${safeName}">${initial}</span>`;
}

const INLINE_SVG_ICONS = {
  sun: `
    <svg viewBox="0 0 64 64" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-preserve-color="true" data-icon-name="sun"><g id="SVGRepo_bgCarrier" stroke-width="0"></g><g id="SVGRepo_tracerCarrier" stroke-linecap="round" stroke-linejoin="round"></g><g id="SVGRepo_iconCarrier"> <path d="m9.86 27.93a28 28 0 0 1 -4.5-6.77c-1.9-4.05-1.92-6.3-1.5-6.46s6.52.11 9.3.13 4 0 4-.16-.41-4.6-.76-7-.69-4-.49-4.06a27.67 27.67 0 0 1 8.49 2c5 1.83 6 3.61 6 3.61a38 38 0 0 1 9.24-5.22c4.85-1.7 6.76-1.46 6.93-1.17s-2.28 11.39-1.86 11.51a22 22 0 0 0 6.59-1.09c2.73-.89 7-3 7.45-3s.82 3.52-1.06 7.85-2.33 4.92-2.16 5a28.33 28.33 0 0 0 4.06-1.36c1.41-.59 2.15-.68 2.32-.18a15.4 15.4 0 0 1 -2.57 10.52c-3.21 4.26-4.62 4.48-4.74 4.65s0 1.07 3.09 1.71 4.65.59 4.61 1.26a11.58 11.58 0 0 1 -5.3 7.38c-4.09 2.44-4.34 2.44-4.25 2.77s.14 7 0 7.51-1.53 1.39-3.73 1a22.33 22.33 0 0 1 -5.75-1.91 7.89 7.89 0 0 1 -1.21-1s-3.68 1.94-4.55 2.28a7.38 7.38 0 0 1 -1.61.43s0 2.82-.11 3a.92.92 0 0 1 -.87.21 4.88 4.88 0 0 0 -1.57 0 2.09 2.09 0 0 1 -1 0 9.27 9.27 0 0 0 -1.58 0c-.46 0-1.58 0-1.66-.28s-.23-2.7-.23-2.7a12.66 12.66 0 0 1 -4.54-1.21 8.49 8.49 0 0 1 -2.34-1.86 9.38 9.38 0 0 1 -4.84 2.53c-2.65.31-4.07 0-4-.22s2.11-6.82 1.61-6.78-9.17.41-9.88.41-1.5-.81-1.21-1.44 4-6.76 5.41-8.18 2.19-2.3 2.19-2.3a22.44 22.44 0 0 0 -5.54-1.53c-2.74-.31-4.16-1.22-4-1.55a23.31 23.31 0 0 1 4.36-5.35c1.98-1.63 3.76-2.98 3.76-2.98z" fill="#1d1d1b"></path> <path d="m5.86 16.34c.3-.3 13.5.06 13.54-.19s-1.88-11.15-1.5-11.15a46 46 0 0 1 9 3.71c2.63 1.68 2.76 2.3 3.47 2.26s4-3.19 8.43-4.88 5.92-1.8 5.92-1.63-1.44 7.31-1.72 8.64-.44 2.74-.15 2.82a28 28 0 0 0 8.26-1c3.93-1.11 6.74-2.66 6.79-2.37a55.16 55.16 0 0 1 -3.63 9.08c-1.19 1.83-1.67 3.41-1.42 3.54s1.66-.31 4.35-1.12 3-.94 3-.81a13.49 13.49 0 0 1 -2.2 7.64 33 33 0 0 1 -5.56 5.85 4.18 4.18 0 0 0 2 2.6c1.17.53 5.7 1.2 5.66 1.45a12.59 12.59 0 0 1 -5.31 6c-3.51 1.81-3.93 2-4 2.32s.18 7.26-.11 7.3a25.34 25.34 0 0 1 -5-.91c-1.41-.49-2-.94-2-1.06a21.37 21.37 0 0 0 2.83-4.51 12 12 0 0 0 .81-3s4.57.22 5-3-1.89-3.81-2.55-3.92a6.49 6.49 0 0 0 -1.33-.16s1.16-10.3-2.33-16-7.21-8.71-14.76-8.4-11.59 4.29-13.44 12-1.36 12.14-1.36 12.14-3.69.15-3.58 4.72 4.93 3.65 4.93 3.65a7 7 0 0 0 1.1 3.91c1.22 1.86 1.77 2.56 1.77 2.56s-4.88 1.86-4.92 1.58 1.25-4.54 1.08-5-1-1.36-1.3-1.36-9.71.41-9.8 0 2.09-3.76 4.27-6.51 3.25-3.81 3.21-4.18-1.51-2.11-4.72-2.58-4.48-.42-4.4-.54a32.65 32.65 0 0 1 5.35-5.44 7.72 7.72 0 0 0 2.52-2.18 35.44 35.44 0 0 1 -4.45-6.21c-1.77-3.33-1.87-5.53-1.75-5.66z" fill="#ffd500"></path> <path d="m20.53 12a42.8 42.8 0 0 1 -.83-4.52 21.08 21.08 0 0 1 3.67 1.38c0 .17 0 .88-.12.88a16.26 16.26 0 0 0 -2.16-.44c0 .12.34 2 .3 2.07s-.78.74-.86.63z" fill="#1d1d1b"></path> <path d="m36 9.5a15.63 15.63 0 0 1 3.26-2.14c1.2-.43 1-.63 1.2-.43s.47.87.34 1a19.73 19.73 0 0 0 -3 1.31c-.95.63-1 1-1.23.92s-.45-.49-.57-.66z" fill="#1d1d1b"></path> <path d="m16.48 41.42a26.38 26.38 0 0 1 .7 4.85c-.12 0-2.36-.27-2.38-1.89a3.74 3.74 0 0 1 1.68-2.96z" fill="#e6e4da"></path> <path d="m48.49 41.46c.13-.14 2.37 0 2 1.93s-2.79 2.61-2.84 2.35a24.06 24.06 0 0 1 .65-2.33c.28-1.17-.06-1.7.19-1.95z" fill="#e6e4da"></path> <path d="m31.2 17.22c4 0 8.84.22 12.63 6.83s3.37 11.85 2.34 17.76-2.67 9.44-3.69 10.57a11.44 11.44 0 0 1 -1.28 1.3 15.58 15.58 0 0 0 -2.46-1.6c-.21.08-.5.38-.41.54s2 1.52 1.92 1.65-1.16.88-1.28.75a27 27 0 0 0 -2.47-1.89c-.16 0-.66.38-.57.55s2.37 1.64 2.13 1.81-1.57.76-1.74.63-1.93-1.93-2.09-1.93-.62.25-.5.46 1.43 1.77 1.43 1.77a30.9 30.9 0 0 1 -8.14-.1c-3.7-.68-6.31-4.76-7.5-9.11s-1.19-13.61.1-18.73 3.56-11.2 11.58-11.26z" fill="#e6e4da"></path> <path d="m30.23 58.37c.12-.12 1.25.08 2.37 0s1.66-.06 1.66.06.14 1.83 0 1.87a2.94 2.94 0 0 1 -1.53.06 2.87 2.87 0 0 0 -1.5-.32c-.41.08-.66.38-.79.21a6.4 6.4 0 0 1 -.21-1.88z" fill="#e6e4da"></path> <path d="m22.94 23.31c.33-.12 3-1.73 9.41-1.7s10.06 1.75 10.15 2.25 0 2.45-.35 2.45-2.34-1.39-9.69-1.31-9.46 1.83-9.87 1.55-.85-2.82.35-3.24z" fill="#1d1d1b"></path> <path d="m23.36 24c.25-.52 3.27-1.69 9.33-1.41s8.9 1.3 8.9 1.64.21.87 0 .87a28.74 28.74 0 0 0 -9.18-1.1 43.37 43.37 0 0 0 -8.87 1.27c-.34-.1-.26-1.14-.18-1.27z" fill="#ffd500"></path> <path d="m35.14 28c2.79-1.14 8-.31 7.81 5.38s-7.13 6.49-9.48 3.31-1.23-7.52 1.67-8.69z" fill="#1d1d1b"></path> <path d="m35.58 28.89c2.28-.94 6.54-.26 6.41 4.41s-5.86 5.33-7.79 2.7-1-6.14 1.38-7.11z" fill="#ffffff"></path> <path d="m23.74 27.85c2.57-1.05 7.35-.29 7.2 4.95s-6.57 6-8.74 3.06-1.13-6.92 1.54-8.01z" fill="#1d1d1b"></path> <path d="m24.15 28.68c2.1-.86 6-.24 5.9 4.07s-5.39 4.91-7.17 2.51-.88-5.68 1.27-6.58z" fill="#ffffff"></path> <g fill="#1d1d1b"> <path d="m25.51 31.85a1.07 1.07 0 1 1 -.32 1.7 1.12 1.12 0 0 1 .32-1.7z"></path> <path d="m37.27 32.29a1.07 1.07 0 1 1 -.33 1.7 1.12 1.12 0 0 1 .33-1.7z"></path> <path d="m21.92 37.74a5.37 5.37 0 0 0 3 1.29 9 9 0 0 0 3.08-.31c.2.05.21.65.2.81a6.57 6.57 0 0 1 -3.76.39 5.1 5.1 0 0 1 -3-1.44c-.09-.17.36-.74.48-.74z"></path> <path d="m36.72 39.58a5.4 5.4 0 0 0 3.27 0 8.55 8.55 0 0 0 2.69-1.47c.21 0 .45.52.5.67a6.54 6.54 0 0 1 -3.32 1.82 5.13 5.13 0 0 1 -3.28-.19c-.17-.08.02-.79.14-.83z"></path> <path d="m29.78 39.79c0-.13.73-.3.78-.1s-.13 6.38 1.57 6.34 1.14-6.36 1.3-6.45a2.14 2.14 0 0 1 .79 0s1 7.59-2 7.55-2.43-7.26-2.44-7.34z"></path> <path d="m22.15 49.3a5.38 5.38 0 0 1 .19-1.74c.25-.55 1-1.17 1.16-1.13s.5.45.37.62-.86.58-.9 1.08 0 1.37-.11 1.42a1.18 1.18 0 0 1 -.71-.25z"></path> <path d="m39.07 46.51s.53-.63.7-.63a2.7 2.7 0 0 1 1.5 1.49c.38 1 .39 1.94.1 2s-.79.13-.79 0 .41-1.24 0-1.91a3.37 3.37 0 0 0 -1.51-.95z"></path> <path d="m24.38 48.58c.21.06 2.22 1.43 6.91 1.27a28 28 0 0 0 7.79-1.55 1.9 1.9 0 0 1 .17.83c0 .16-3.35 1.85-8.12 1.76s-6.94-1.23-7-1.4.13-.95.25-.91z"></path> </g></g></svg>
  `,
  moon: `
    <svg viewBox="0 0 64.00 64.00" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-preserve-color="true" data-icon-name="moon" fill="#000000">
      <g id="SVGRepo_bgCarrier" stroke-width="0"></g>
      <g id="SVGRepo_tracerCarrier" stroke-linecap="round" stroke-linejoin="round"></g>
      <g id="SVGRepo_iconCarrier">
        <path d="m10.35 35.62s-.75.14-1-1.48-.58-8.61-.3-8.87a10 10 0 0 1 2.07-.63s0-7.54 2.67-11.86 5.4-6.09 10.65-7.56a29.38 29.38 0 0 1 15.18.3c4.41 1.18 8.7 4.62 9.89 8.8a49.27 49.27 0 0 1 1.49 9.55 9.53 9.53 0 0 1 2.52.9c.17.33.43 9.12 0 9.38a1.6 1.6 0 0 1 -.87.23s7.86 18 7.59 19-12.93 6.36-30.1 6.23-26.05-5.41-26.36-6.11 6.57-17.88 6.57-17.88z" fill="#1d1d1b"></path>
        <path d="m13.24 27.92s-.6-7.65 1.3-11.91a19.6 19.6 0 0 1 7.75-8.23 20.47 20.47 0 0 1 6.51-1.45c.05.16.67 10.23.66 11.61a27 27 0 0 1 -.57 4s-4-2.44-8.16-1.38a10.47 10.47 0 0 0 -7.49 7.36z" fill="#6a6a6a"></path>
        <path d="m30.11 22.18s.53-3.69.54-4.89-.8-10.82-.6-10.95a4.11 4.11 0 0 1 1.88 0c.07.16 1.07 11.66 1.28 13.22a9.33 9.33 0 0 1 .27 2.44 2.85 2.85 0 0 1 -3.37.18z" fill="#6a6a6a"></path>
        <path d="m34.8 21.44a76.48 76.48 0 0 1 -1.4-9.35c-.17-3.62-.51-5.65-.06-5.83s6.66.17 10.42 3.45 4.06 5.61 3.81 5.66-2.52-1-2.64-.77-.15.8.06.92 2.69 1.1 2.82 1.26a4.18 4.18 0 0 1 .28.95 16.94 16.94 0 0 0 -3-1.13c-.12.17-.23.92 0 1s3 1.3 3 1.3l.11.79s-2.83-1.18-3-1.14-.36.8-.15.92 3.16 1.26 3.25 1.43.41 1.36.41 1.36l-1.71-.54s.31.87.48 1 1.3.42 1.31.76a31.35 31.35 0 0 1 .2 3.33c-.12 0-2.2-6.45-5.21-7.09s-8.98 1.72-8.98 1.72z" fill="#6a6a6a"></path>
        <path d="m22.44 22.28c2.21 0 7.19 2.45 9.76 1.84s6.89-2.79 9.51-2.81 4.07 2.48 6.36 7.84 7.14 17.34 7 17.34-8.1-17.89-9.66-20.23-2.66-3-5.86-2.77-6.81 1-6.85 1.41 0 7.17.49 7.75 3.06.71 6.18.51 5-.33 5.77-.64a10.14 10.14 0 0 0 1.69-1l7.79 16.48a30.15 30.15 0 0 0 -2.94-2.56c-.21.05-.31.8-.19.88s3.67 3.33 4 3.7a12.43 12.43 0 0 1 1.61 2.63c-.12.17-.45.55-.66.47s-5-5.71-5.39-5.83-.45.6-.45.6 5.18 5.5 5.06 5.63a8.62 8.62 0 0 1 -1.15.57s-4.34-5-4.5-4.94-.69.6-.4.81a45.71 45.71 0 0 1 4.07 4.36 7 7 0 0 1 -1.2.4 46.49 46.49 0 0 0 -3.58-3.72c-.25 0-.61.39-.49.55s3.34 3.29 3.22 3.46a4 4 0 0 1 -1.16.37s-2.47-2.59-2.65-2.59-.74.31-.53.6 2.39 2.19 2.27 2.28a4.86 4.86 0 0 1 -1 .19c-.13 0-2-2-2.22-2s-.78.19-.65.48 1.83 1.79 1.63 1.92-5.17 1.66-15.76 1.51-14.84-1.85-14.84-1.85a46 46 0 0 0 -4.22-3.69c-.37 0-.78.6-.61.72a29.31 29.31 0 0 1 2.86 2.6c-.16 0-3.27-.92-4.32-1.1a7.6 7.6 0 0 1 -2.26-.61c0-.13 6-14.45 6.27-14.41s4.29 10.07 4.29 10.07a2.77 2.77 0 0 0 1.2 2.89 3.9 3.9 0 0 0 3.49-.25l15.51.08c.12 0 2.65 1.27 3.69-.68s.44-2.67.44-2.67 4-11.27 3.76-11.52a5.63 5.63 0 0 0 -1.76-.35s-2.75 10.32-3 10.33-1.26-.3-1.68-.34a3.18 3.18 0 0 0 -.7 0l-5.48-7.62s1.1-6-3.28-6-3.14 6-3.14 6-1-.06-2 1.51-3.39 6.29-3.39 6.29a3.77 3.77 0 0 0 -1.45.2c-.66.27-1.07.49-1.07.49s-3.54-8.29-3.46-8.46.82-2.15 1.2-2.16 2.93.39 3.93.41 1.16-.11 1.2-.28.27-.8.1-.8-5.11-.57-5.48-.57-1.07 2.14-1.36 2.23-1.32.25-1.2 0 2.16-5.13 2.29-5.1 7.53 1.07 9.73.64 2.76-1.11 3.07-2.16a31.45 31.45 0 0 0 .25-6.59 19.1 19.1 0 0 0 -6.2-1.31c-2.79-.09-4.67-.13-5.87 1.82s-10.79 27.71-10.96 27.76a3.64 3.64 0 0 1 -.92-.29s4.91-14 6.78-19.26 3.47-11.56 9.47-11.46z" fill="#6a6a6a"></path>
        <path d="m28.76 40.3a14.69 14.69 0 0 0 2.25 0l2.29-.09s-.39 9.67 0 9.75.67.27.66-.06-.19-7.75.06-7.79.75 1.52.75 1.52-.14 6.42 0 6.46.88.27.84.06 0-4.75 0-4.75l1.43 2.26-.79.06s-.08 2 .17 2a4.75 4.75 0 0 0 .71 0c.12 0 .11 1.16.11 1.16s-7.26-.19-9.8-.13-2.74.11-2.74.11a6.75 6.75 0 0 0 -.55-2l-.62-1.28s4.86-7.15 5.23-7.28z" fill="#6a6a6a"></path>
        <g fill="#1d1d1b">
          <path d="m30.57 41.13c.28-.1.79-.06.84.1s.41 8.41.17 8.63-.88.14-.88 0-.25-8.69-.13-8.73z"></path>
          <path d="m28.74 41.26c.21-.09.7-.19.79 0s.55 8.61.38 8.66-.87.18-.87 0-.3-8.66-.3-8.66z"></path>
          <path d="m27 44.8c.06-.14.87-.4.87-.23a52.12 52.12 0 0 1 .21 5.25 3 3 0 0 1 -.79.06s-.38-4.88-.29-5.08z"></path>
          <path d="m25.25 46.8c0-.17.57-.64.65-.51a14 14 0 0 1 .26 3.45 4 4 0 0 1 -.79.14s-.16-2.88-.12-3.08z"></path>
          <path d="m41.23 35.62c.19-.1 2.79-.16 4.58-.37a11 11 0 0 1 1.78-.25c.21 0 .48.87.36 1a17.41 17.41 0 0 1 -4.32.69c-1.46 0-2.29 0-2.29 0s-.48-.9-.11-1.07z"></path>
          <path d="m42 38.39c.34-.09 1.41 1.51 1.41 1.51s-.19.79-.4.63a9.86 9.86 0 0 1 -1.5-1.63c-.08-.21.29-.46.49-.51z"></path>
          <path d="m40.41 39.68c.29-.1 2.3 1.78 2.22 2s-.15.63-.28.59a19.84 19.84 0 0 1 -2.43-2.11c-.04-.22.37-.44.49-.48z"></path>
          <path d="m39.41 41.16s2.55 2 2.47 2.32a2.36 2.36 0 0 1 -.36.67s-2.93-2.38-2.81-2.6.53-.26.7-.39z"></path>
          <path d="m14.45 48.4c.2.07 2.81 2.31 2.82 2.72s-.11.63-.24.63a27 27 0 0 1 -2.93-2.38c-.05-.17.14-1.05.35-.97z"></path>
          <path d="m13.67 50.5s3.83 3.16 3.83 3.37-.11.67-.23.67a35.1 35.1 0 0 1 -4.08-3.07c-.04-.25.48-.97.48-.97z"></path>
          <path d="m16.27 17c-.17-.24.44-2.3 1.57-3.91s2.19-2.52 2.65-2.4.77.61.6.82a7.05 7.05 0 0 0 -2.72 2.86c-.79 1.85-.55 3-.8 3.06a1.86 1.86 0 0 1 -1.3-.43z"></path>
          <path d="m44.85 13.23a2.79 2.79 0 0 1 1.6.54c.09.25.27.83-.11.76a10.8 10.8 0 0 1 -1.43-.53c-.17-.06-.31-.72-.06-.77z"></path>
        </g>
      </g>
    </svg>
  `,
  bell: `
    <svg viewBox="2.8 1.2 18.8 21.8" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="bell">
      <path d="M12.0196 2.91016C8.7096 2.91016 6.0196 5.60016 6.0196 8.91016V11.8002C6.0196 12.4102 5.7596 13.3402 5.4496 13.8602L4.2996 15.7702C3.5896 16.9502 4.0796 18.2602 5.3796 18.7002C9.6896 20.1402 14.3396 20.1402 18.6496 18.7002C19.8596 18.3002 20.3896 16.8702 19.7296 15.7702L18.5796 13.8602C18.2796 13.3402 18.0196 12.4102 18.0196 11.8002V8.91016C18.0196 5.61016 15.3196 2.91016 12.0196 2.91016Z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-miterlimit="10" stroke-linecap="round"></path>
      <path d="M13.8699 3.19994C13.5599 3.10994 13.2399 3.03994 12.9099 2.99994C11.9499 2.87994 11.0299 2.94994 10.1699 3.19994C10.4599 2.45994 11.1799 1.93994 12.0199 1.93994C12.8599 1.93994 13.5799 2.45994 13.8699 3.19994Z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-miterlimit="10" stroke-linecap="round" stroke-linejoin="round"></path>
      <path d="M15.0195 19.0601C15.0195 20.7101 13.6695 22.0601 12.0195 22.0601C11.1995 22.0601 10.4395 21.7201 9.89953 21.1801C9.35953 20.6401 9.01953 19.8801 9.01953 19.0601" opacity="0.4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-miterlimit="10"></path>
    </svg>
  `,
  search: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="search">
      <path d="M20 20L15.8033 15.8033C15.8033 15.8033 14 18 10.5 18C6.35786 18 3 14.6421 3 10.5C3 6.35786 6.35786 3 10.5 3C14.6421 3 18 6.35786 18 10.5C18 11.0137 17.9484 11.5153 17.85 12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"></path>
    </svg>
  `,
  send: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="send">
      <path d="M10 14L13 21L20 4L3 11L6.5 12.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"></path>
    </svg>
  `,
  paperclip: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="paperclip">
      <path d="m8.5 12.5 6.4-6.4a3.2 3.2 0 1 1 4.5 4.5l-8.1 8.1a5.2 5.2 0 1 1-7.4-7.4l8.6-8.6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"></path>
    </svg>
  `,
  logout: `
    <svg viewBox="1.2 1.2 21.6 21.6" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="logout">
      <path d="M21.25 12C21.25 17.1086 17.1086 21.25 12 21.25C6.89137 21.25 2.75 17.1086 2.75 12C2.75 6.89137 6.89137 2.75 12 2.75C12.4142 2.75 12.75 2.41421 12.75 2C12.75 1.58579 12.4142 1.25 12 1.25C6.06294 1.25 1.25 6.06294 1.25 12C1.25 17.9371 6.06294 22.75 12 22.75C17.9371 22.75 22.75 17.9371 22.75 12C22.75 11.5858 22.4142 11.25 22 11.25C21.5858 11.25 21.25 11.5858 21.25 12Z" fill="currentColor"></path>
      <path d="M12.4697 10.4697C12.1768 10.7626 12.1768 11.2374 12.4697 11.5303C12.7626 11.8232 13.2374 11.8232 13.5303 11.5303L21.25 3.81066V7.34375C21.25 7.75796 21.5858 8.09375 22 8.09375C22.4142 8.09375 22.75 7.75796 22.75 7.34375V2C22.75 1.58579 22.4142 1.25 22 1.25H16.6562C16.242 1.25 15.9062 1.58579 15.9062 2C15.9062 2.41421 16.242 2.75 16.6562 2.75H20.1893L12.4697 10.4697Z" fill="currentColor"></path>
    </svg>
  `,
  home: `
    <svg viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
      <path d="M981.4 502.3c-9.1 0-18.3-2.9-26-8.9L539 171.7c-15.3-11.8-36.7-11.8-52 0L70.7 493.4c-18.6 14.4-45.4 10.9-59.7-7.7-14.4-18.6-11-45.4 7.7-59.7L435 104.3c46-35.5 110.2-35.5 156.1 0L1007.5 426c18.6 14.4 22 41.1 7.7 59.7-8.5 10.9-21.1 16.6-33.8 16.6z" fill="currentColor"></path>
      <path d="M810.4 981.3H215.7c-70.8 0-128.4-57.6-128.4-128.4V534.2c0-23.5 19.1-42.6 42.6-42.6s42.6 19.1 42.6 42.6v318.7c0 23.8 19.4 43.2 43.2 43.2h594.8c23.8 0 43.2-19.4 43.2-43.2V534.2c0-23.5 19.1-42.6 42.6-42.6s42.6 19.1 42.6 42.6v318.7c-0.1 70.8-57.7 128.4-128.5 128.4z" fill="currentColor"></path>
    </svg>
  `,
  post: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
      <path d="M19.44,8.22C17.53,10.41,14,10,14,10s-.39-4,1.53-6.18a3.49,3.49,0,0,1,.56-.53L18,4l.47-1.82A8.19,8.19,0,0,1,21,2S21.36,6,19.44,8.22ZM14,10l-2,2" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"></path>
      <path d="M12,3H4A1,1,0,0,0,3,4V20a1,1,0,0,0,1,1H20a1,1,0,0,0,1-1V12" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"></path>
    </svg>
  `,
  createdPosts: `
    <svg viewBox="0 0 64 64" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-preserve-color="true" data-icon-name="created-posts">
      <g transform="translate(6, 4) scale(2.8)">
        <path d="M 4,0 V 18 H 17 V 5 4 L 13,0 h -1 z m 1,1 h 7 v 3 1 h 4 V 17 H 5 Z M 2,2 v 17 1 H 3 15 V 19 H 3 V 2 Z" fill="currentColor"></path>
        <path d="M 6,7 v 1 h 9 V 7 Z m 0,2 v 1 h 9 V 9 Z m 0,2 v 1 h 9 v -1 z m 0,2 v 1 h 9 v -1 z m 0,2 v 1 h 9 v -1 z" fill="currentColor"></path>
      </g>
      <g transform="translate(18, 6) scale(1.1)">
        <circle cx="12" cy="12" r="10" fill="#ffffff" stroke="currentColor" stroke-width="1.8"></circle>
        <circle cx="12" cy="9" r="3.5" stroke="currentColor" stroke-width="1.8" fill="none"></circle>
        <path d="M17.9691 20C17.81 17.1085 16.9247 15 11.9999 15C7.07521 15 6.18991 17.1085 6.03076 20" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" fill="none"></path>
      </g>
    </svg>
  `,
  like: `
    <svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
      <path d="M42.5,7.6A12,12,0,0,0,34,4c-3.9,0-8,3.1-10,6.5C22,7.1,17.9,4,14,4A12,12,0,0,0,5.5,7.6,11.9,11.9,0,0,0,2,16.1,11.1,11.1,0,0,0,2.2,18H6.3A6.8,6.8,0,0,1,6,16.1a7.7,7.7,0,0,1,2.4-5.6A7.6,7.6,0,0,1,14,8.1c2.2,0,5.1,2,6.5,4.4L23.1,17a1.1,1.1,0,0,0,1.8,0l2.6-4.5c1.4-2.4,4.3-4.4,6.5-4.4a7.6,7.6,0,0,1,5.6,2.4A7.7,7.7,0,0,1,42,16.1c0,1.8-.9,3.7-2.7,5.9H25.9L24,25.1l-5.1-8.6a1.1,1.1,0,0,0-1.8,0L13.9,22H7a2,2,0,0,0-2,2.3A2.1,2.1,0,0,0,7.1,26h9L18,22.9l5.1,8.6a1.1,1.1,0,0,0,1.8,0L28.1,26h7.6L24,38.5,16,30H10.5L23.3,43.7a1,1,0,0,0,1.4,0C27.7,40.5,39,28.5,41,26.2s5-6,5-10.1A11.9,11.9,0,0,0,42.5,7.6Z" fill="currentColor"></path>
    </svg>
  `,
  dislike: `
    <svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
      <path d="M21.125 4 L19.46875 4.625 C17.605469 5.328125 16.394531 6.054688 15.59375 6.8125 C14.792969 7.570313 14.414063 8.402344 14.21875 9.03125 C14.023438 9.660156 13.945313 10.027344 13.8125 10.21875 C13.695313 10.386719 13.492188 10.589844 12.84375 10.78125 C12.832031 10.773438 12.753906 10.722656 12.75 10.71875 L12.71875 10.71875 C12.21875 10.390625 11.648438 10.1875 11.09375 10.09375 C9.429688 9.8125 7.6875 10.539063 6.71875 12.09375 L6.6875 12.125 C6.09375 13.132813 5.769531 14.410156 5.9375 15.75 C5.988281 16.15625 6.089844 16.570313 6.25 16.96875 C4.785156 17.054688 3.410156 17.878906 2.625 19.21875 L2.59375 19.21875 C1.375 21.371094 2.117188 24.148438 4.28125 25.375 L4.3125 25.375 L4.3125 25.40625 C4.425781 25.460938 5.757813 26.164063 7.75 26.78125 C9.742188 27.398438 12.53125 28 16 28 C19.46875 28 22.253906 27.421875 24.25 26.8125 C26.222656 26.207031 27.332031 25.613281 27.71875 25.375 C27.722656 25.371094 27.746094 25.347656 27.75 25.34375 C29.882813 24.105469 30.601563 21.378906 29.375 19.28125 L29.40625 19.28125 C28.832031 18.265625 27.933594 17.574219 26.90625 17.25 C26.90625 17.246094 26.875 17.253906 26.875 17.25 C26.558594 16.109375 25.808594 15.132813 24.78125 14.5625 C24.917969 14.125 25 13.617188 25 13 C25 11.839844 24.585938 10.863281 24.03125 10.15625 C23.476563 9.449219 22.808594 8.976563 22.25 8.53125 C21.691406 8.085938 21.242188 7.675781 21 7.28125 C20.757813 6.886719 20.636719 6.511719 20.78125 5.75 Z M18.875 7.1875 C18.96875 7.605469 19.109375 8.011719 19.3125 8.34375 C19.785156 9.113281 20.417969 9.601563 21 10.0625 C21.582031 10.523438 22.125 10.964844 22.46875 11.40625 C22.8125 11.847656 23 12.269531 23 13 C23 13.480469 22.945313 13.773438 22.875 14 C20.148438 13.601563 17.761719 12.914063 16 12.25 C15.648438 12.117188 15.339844 11.976563 15.03125 11.84375 C15.199219 11.695313 15.347656 11.519531 15.46875 11.34375 C15.917969 10.691406 15.980469 10.058594 16.125 9.59375 C16.269531 9.128906 16.433594 8.757813 16.96875 8.25 C17.289063 7.945313 18.179688 7.550781 18.875 7.1875 Z M10.3125 11.96875 C10.753906 11.964844 11.207031 12.101563 11.625 12.375 C12.175781 12.757813 13.355469 13.359375 15.3125 14.09375 C17.289063 14.835938 19.933594 15.625 23.03125 16.03125 L23.0625 16.03125 C23.757813 16.117188 24.332031 16.527344 24.6875 17.125 C24.332031 17.191406 23.964844 17.285156 23.625 17.4375 C23.574219 17.460938 23.519531 17.445313 23.46875 17.46875 C23.429688 17.480469 23.421875 17.492188 23.40625 17.5 C23.375 17.515625 23.34375 17.515625 23.3125 17.53125 C23.285156 17.542969 23.253906 17.578125 23.21875 17.59375 C23.132813 17.632813 23.007813 17.683594 22.84375 17.75 C22.519531 17.886719 22.035156 18.0625 21.40625 18.25 C20.148438 18.621094 18.320313 19 16 19 C14.277344 19 12.804688 18.804688 11.65625 18.5625 C11.296875 18.476563 10.949219 18.382813 10.625 18.28125 C9.972656 18.074219 9.367188 17.777344 9.375 17.78125 C8.40625 17.128906 8.011719 16.34375 7.90625 15.5 C7.800781 14.65625 8.035156 13.757813 8.40625 13.125 C8.875 12.371094 9.578125 11.976563 10.3125 11.96875 Z M6.28125 18.9375 C6.757813 18.902344 7.257813 19.019531 7.71875 19.28125 C7.726563 19.285156 7.742188 19.308594 7.75 19.3125 C7.804688 19.34375 7.875 19.375 7.9375 19.40625 C8.0625 19.472656 8.207031 19.539063 8.40625 19.625 C8.804688 19.796875 9.355469 19.984375 10.0625 20.1875 C10.3125 20.257813 10.617188 20.335938 10.90625 20.40625 C11.011719 20.433594 11.109375 20.472656 11.21875 20.5 C11.230469 20.503906 11.238281 20.496094 11.25 20.5 C11.648438 20.601563 12.042969 20.691406 12.4375 20.75 L12.4375 20.71875 C13.480469 20.890625 14.664063 21 16 21 C18.535156 21 20.558594 20.574219 21.96875 20.15625 C22.675781 19.945313 23.242188 19.722656 23.625 19.5625 C23.765625 19.503906 23.84375 19.449219 23.9375 19.40625 L24.03125 19.40625 L24.0625 19.375 L24.25 19.28125 C25.492188 18.617188 26.984375 19.011719 27.6875 20.25 L27.6875 20.28125 C28.371094 21.449219 27.976563 22.960938 26.75 23.65625 L26.71875 23.65625 L26.71875 23.6875 C26.640625 23.734375 25.515625 24.347656 23.6875 24.90625 C21.859375 25.464844 19.25 26 16 26 C12.75 26 10.144531 25.472656 8.3125 24.90625 C6.503906 24.34375 5.597656 23.832031 5.25 23.65625 C5.234375 23.648438 5.230469 23.632813 5.21875 23.625 C4.023438 22.917969 3.652344 21.441406 4.34375 20.21875 C4.691406 19.636719 5.210938 19.203125 5.8125 19.03125 C5.964844 18.988281 6.121094 18.949219 6.28125 18.9375 Z" fill="currentColor"></path>
    </svg>
  `,
  comment: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
      <path d="M21.5,11V21a.489.489,0,0,1-.31.46.433.433,0,0,1-.19.04.508.508,0,0,1-.36-.15L18.83,19.5H10A3.5,3.5,0,0,1,6.5,16H14a5,5,0,0,0,5-5V7.65A3.507,3.507,0,0,1,21.5,11Z" fill="currentColor"></path>
      <path d="M17.5,11V6A3.5,3.5,0,0,0,14,2.5H6A3.5,3.5,0,0,0,2.5,6V16a.489.489,0,0,0,.31.46A.433.433,0,0,0,3,16.5a.508.508,0,0,0,.36-.15L5.17,14.5H14A3.5,3.5,0,0,0,17.5,11Z" fill="none" stroke="currentColor" stroke-width="0.5"></path>
    </svg>
  `,
  filter: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
      <g id="SVGRepo_bgCarrier" stroke-width="0"></g>
      <g id="SVGRepo_tracerCarrier" stroke-linecap="round" stroke-linejoin="round"></g>
      <g id="SVGRepo_iconCarrier">
        <path d="M9 4h13v1H9V4zm0 17h13v-1H9v1zm0-8h13v-1H9v1zm-5.44 9.17L7 18.74 6.26 18l-2.71 2.7-1.12-1-.74.74 1.86 1.73zm0-16L7 2.74 6.26 2 3.55 4.7l-1.12-1-.74.74 1.86 1.73zm0 8L7 10.74 6.26 10l-2.71 2.7-1.12-1-.74.74 1.86 1.73z" fill="currentColor"></path>
      </g>
    </svg>
  `,
  menu: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="menu">
      <path d="M4 7h16M4 12h16M4 17h16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"></path>
    </svg>
  `,
  eye: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="eye">
      <path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6-10-6-10-6Z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"></path>
      <circle cx="12" cy="12" r="2.7" fill="none" stroke="currentColor" stroke-width="1.8"></circle>
    </svg>
  `,
  share: `
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="share">
      <path d="M15 8.5 8.5 11.6m6.5 3.9-6.5-3.1" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"></path>
      <circle cx="18" cy="6.8" r="2.2" fill="none" stroke="currentColor" stroke-width="1.8"></circle>
      <circle cx="6" cy="12" r="2.2" fill="none" stroke="currentColor" stroke-width="1.8"></circle>
      <circle cx="18" cy="17.2" r="2.2" fill="none" stroke="currentColor" stroke-width="1.8"></circle>
    </svg>
  `,
  back: `
    <svg viewBox="0 0 512.001 512.001" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" data-icon-name="back">
      <path d="M355.477,72.795h-81.423c-10.221,0-18.507,8.286-18.507,18.507s8.286,18.507,18.507,18.507h81.423c65.898,0,119.51,53.612,119.51,119.511s-53.611,119.511-119.51,119.511H131.049v-28.585c0-19.011-20.634-30.957-37.126-21.434l-81.565,47.092c-16.465,9.506-16.492,33.348,0,42.869l81.565,47.091c16.464,9.506,37.126-2.391,37.126-21.434v-28.584h224.429c86.308,0,156.523-70.217,156.523-156.524S441.785,72.795,355.477,72.795z" fill="currentColor"></path>
      <path d="M131.049,109.809h60.633c10.221,0,18.507-8.286,18.507-18.507s-8.286-18.507-18.507-18.507h-60.633c-10.221,0-18.507,8.286-18.507,18.507S120.829,109.809,131.049,109.809z" fill="currentColor"></path>
    </svg>
  `,
};

function icon(name) {
  if (INLINE_SVG_ICONS[name]) {
    return `<span class="icon">${INLINE_SVG_ICONS[name]}</span>`;
  }

  const map = {
    logo: "F",
    search: "?",
    sun: "O",
    moon: "C",
    bell: "!",
    home: "H",
    post: "P",
    liked: "L",
    back: "<",
    plus: "+",
    send: "^",
    paperclip: "@",
    open: ">",
    like: "+",
    dislike: "-",
    comment: "#",
    filter: "=",
    menu: "=",
    eye: "o",
    share: ">",
  };
  return `<span class="icon">${map[name] || "*"}</span>`;
}

function getAuthorUsername(entity) {
  const author = entity && entity.author;
  if (author && typeof author === "object") {
    const username = String(author.username || "").trim();
    if (username) return username.replace(/^@+/, "");
  }
  if (typeof author === "string") {
    const username = author.trim();
    if (username) return username.replace(/^@+/, "");
  }
  if (entity && entity.user_id != null) return `user${entity.user_id}`;
  return "user";
}

function getAuthorDisplay(entity) {
  const author = entity && entity.author;
  if (author && typeof author === "object") {
    return getDisplayNameOrUsername(author);
  }
  if (entity && entity.user_id != null) {
    return getUserDisplayName(entity.user_id);
  }
  const username = getAuthorUsername(entity);
  return username || "user";
}

function handleSessionEndedUX(message) {
  const text = String(message || "Session ended. Please sign in again.").trim();
  clearAuthenticatedState();

  if (!state.authSessionNoticeOpen) {
    state.authSessionNoticeOpen = true;
    try {
      alert(text);
    } finally {
      state.authSessionNoticeOpen = false;
    }
  }
}

async function apiFetch(url, options = {}) {
  const opts = { ...options };
  const suppressSessionEndedUX = Boolean(opts.suppressSessionEndedUX);
  delete opts.suppressSessionEndedUX;
  opts.headers = opts.headers || {};
  if (!(opts.body instanceof FormData) && !opts.headers["Content-Type"]) {
    opts.headers["Content-Type"] = "application/json";
  }

  const res = await fetch(url, opts);
  if (res.status === 204) return null;

  let data = null;
  try {
    data = await res.json();
  } catch (_) {
    data = null;
  }

  if (!res.ok) {
    const apiError = data && typeof data.error === "string" ? data.error : "";
    const apiMessage = data && typeof data.message === "string" ? data.message : "";
    const err = new Error(apiMessage || apiError || "request failed");
    err.status = res.status;
    err.code = apiError;
    err.apiMessage = apiMessage;

    const isSessionEndedUnauthorized = res.status === 401 && apiError === "unauthorized" && apiMessage;
    if (isSessionEndedUnauthorized && !suppressSessionEndedUX) {
      err.handled = true;
      handleSessionEndedUX(apiMessage);
    }

    throw err;
  }
  return data;
}

function navigate(path) {
  history.pushState(null, "", path);
  router();
}

function getActivePostIDFromPath(pathname = location.pathname) {
  const match = String(pathname || "").match(/^\/post\/([^/?#]+)/);
  return match ? match[1] : null;
}

function categoryTags(categories) {
  if (!Array.isArray(categories) || categories.length === 0) return "";
  return categories.map((c) => `<span class="tag-pill">${escapeHTML(c.name)}</span>`).join("");
}

function renderHeader() {
  const user = state.user;
  const pathname = String(location.pathname || "/");
  const isLoginRoute = pathname === "/login";
  const activePostID = getActivePostIDFromPath();
  const isPostSearch = Boolean(activePostID);
  const totalUnread = getTotalUnreadCount();
  const headerSearchValue = isPostSearch ? String(state.commentSearchByPost[String(activePostID)] || "") : String(state.filters.q || "");
  const headerSearchPlaceholder = isPostSearch ? "Search comments or author..." : "Search posts, content or author...";
  return `
    <header class="app-header">
      <a class="brand header-brand" data-link data-action="open-home-feed" href="/">
        <span class="brand-badge">${icon("logo")}</span>
        <span class="brand-text">Forum</span>
      </a>
      <form id="global-feed-search-form" class="search-box header-search" role="search" aria-label="${isPostSearch ? "Search comments" : "Search posts"}">
        ${icon("search")}
        <input type="search" name="q" maxlength="100" placeholder="${escapeHTML(headerSearchPlaceholder)}" value="${escapeHTML(headerSearchValue)}" />
      </form>
      <div class="header-right">
        <nav class="top-nav">
          <a class="top-link is-active" data-link href="/">Feed</a>
          <a class="top-link is-muted" href="#" data-action="under-construction">Popular</a>
          <a class="top-link is-muted" href="#" data-action="under-construction">Recent</a>
        </nav>
        <button class="icon-btn icon-btn-plain theme-toggle-btn" type="button" data-action="toggle-theme" aria-label="Toggle theme">
          ${state.theme === "dark" ? icon("sun") : icon("moon")}
        </button>
        <button class="icon-btn icon-btn-plain notification-btn${totalUnread > 0 ? " has-notifications" : ""}" type="button" data-action="open-notifications" aria-label="${escapeHTML(getNotificationsLabel(totalUnread))}">
          ${icon("bell")}
          ${totalUnread > 0 ? '<span class="notif-dot" aria-hidden="true"></span>' : ""}
        </button>
        ${
          user
            ? `
              <button
                class="user-chip"
                type="button"
                data-action="open-profile"
                data-username="${escapeHTML(user.username)}"
                aria-label="Open your profile"
              >
                ${avatarMarkup(getDisplayNameOrUsername(user), user.avatarUrl, "sm")}
                <span>${escapeHTML(getDisplayNameOrUsername(user))}</span>
              </button>
              <button class="icon-btn icon-btn-plain logout-btn" type="button" data-action="logout" aria-label="Logout">${icon("logout")}</button>
            `
            : `
              <a class="btn ${isLoginRoute ? "btn-primary" : "btn-ghost"} btn-compact" data-link href="/login">Login</a>
              <a class="btn ${isLoginRoute ? "btn-ghost" : "btn-primary"} btn-compact" data-link href="/register">Register</a>
            `
        }
      </div>
    </header>
  `;
}

function renderSidebar(mode) {
  const categoriesList = (state.categories || [])
    .map(
      (cat) => `
        <button
          class="side-filter-btn ${state.filters.cat.has(String(cat.id)) ? "is-active" : ""}"
          type="button"
          data-cat-toggle="${cat.id}"
          aria-pressed="${state.filters.cat.has(String(cat.id)) ? "true" : "false"}"
        >${escapeHTML(cat.name)}</button>
      `
    )
    .join("");

  return `
    <aside class="app-sidebar">
      <div class="sidebar-block">
        <div class="sidebar-title">Navigation</div>
        <a class="side-link ${mode === "feed" && !state.filters.mine && !state.filters.liked ? "is-active" : ""}" data-link data-action="open-home-feed" href="/">${icon("home")}<span>Home</span></a>
        <a class="side-link ${mode === "center" ? "is-active" : ""}" data-link href="/center">${icon("bell")}<span>Center</span></a>
        <a class="side-link ${mode === "feed" && state.filters.mine ? "is-active" : ""}" data-link data-action="open-created-posts" href="/">${icon("createdPosts")}<span>Created Posts</span></a>
        <a class="side-link ${mode === "feed" && state.filters.liked ? "is-active" : ""}" data-link data-action="open-liked-posts" href="/">${icon("like")}<span>Liked Posts</span></a>
      </div>

      ${
        mode === "feed"
          ? `
            <div class="sidebar-block">
              <div class="sidebar-title">Filters</div>
              <div class="side-check-list">
                ${categoriesList || '<div class="side-note">No categories</div>'}
              </div>
            </div>
          `
          : mode === "post"
            ? ""
            : `
              <div class="sidebar-block">
                <div class="sidebar-title">Categories</div>
                <div class="side-tag-list">
                  ${(state.categories || []).map((cat) => `<span class="side-tag">${escapeHTML(cat.name)}</span>`).join("") || '<span class="side-tag">No categories</span>'}
                </div>
              </div>
            `
      }

      ${renderPresencePanel()}
    </aside>
  `;
}

function renderLayout({ mode = "feed", content, hideHeading = false, title = "", subtitle = "" }) {
  return `
    <div class="ui-shell">
      ${renderHeader()}
      <div class="ui-body">
        ${renderSidebar(mode)}
        <main class="app-main">
          ${
            hideHeading
              ? ""
              : `
                <section class="page-head">
                  <h1>${escapeHTML(title)}</h1>
                  ${subtitle ? `<p>${escapeHTML(subtitle)}</p>` : ""}
                </section>
              `
          }
          ${content}
        </main>
      </div>
    </div>
  `;
}

function renderNotice(msg) {
  return `<div class="notice-box">${escapeHTML(msg)}</div>`;
}

function closeAppModal() {
  const modal = document.getElementById("app-modal-root");
  if (modal) modal.remove();
}

function showFormModal({ title = "Dialog", description = "", fields = [], submitLabel = "Submit", cancelLabel = "Cancel" } = {}) {
  closeAppModal();
  return new Promise((resolve) => {
    const modal = document.createElement("div");
    modal.id = "app-modal-root";
    modal.className = "modal-overlay";
    modal.innerHTML = `
      <div class="modal-card" role="dialog" aria-modal="true" aria-labelledby="app-modal-title">
        <div class="modal-head">
          <h2 id="app-modal-title">${escapeHTML(title)}</h2>
          <button class="icon-btn icon-btn-plain" type="button" data-modal-close aria-label="Close">x</button>
        </div>
        ${description ? `<p class="modal-copy">${escapeHTML(description)}</p>` : ""}
        <form class="form-stack" data-modal-form>
          ${fields
            .map((field) => {
              const type = String(field.type || "text").trim();
              const name = String(field.name || "").trim();
              const label = String(field.label || name).trim();
              const value = field.value == null ? "" : String(field.value);
              const required = field.required ? "required" : "";
              const maxLength = Number.isFinite(field.maxLength) && field.maxLength > 0 ? `maxlength="${field.maxLength}"` : "";
              if (type === "select") {
                return `
                  <label class="field">
                    <span>${escapeHTML(label)}</span>
                    <select name="${escapeHTML(name)}" ${required}>
                      ${(Array.isArray(field.options) ? field.options : [])
                        .map((option) => {
                          const optionValue = String(option && option.value ? option.value : "").trim();
                          const optionLabel = String(option && option.label ? option.label : optionValue).trim();
                          return `<option value="${escapeHTML(optionValue)}" ${optionValue === value ? "selected" : ""}>${escapeHTML(optionLabel)}</option>`;
                        })
                        .join("")}
                    </select>
                  </label>
                `;
              }
              if (type === "textarea") {
                return `
                  <label class="field">
                    <span>${escapeHTML(label)}</span>
                    <textarea name="${escapeHTML(name)}" ${required} ${maxLength}>${escapeHTML(value)}</textarea>
                  </label>
                `;
              }
              if (type === "checkbox-group") {
                const selected = Array.isArray(field.value) ? field.value.map((entry) => String(entry).trim()) : [];
                return `
                  <fieldset class="field modal-checkbox-group">
                    <span>${escapeHTML(label)}</span>
                    <div class="category-grid">
                      ${(Array.isArray(field.options) ? field.options : [])
                        .map((option) => {
                          const optionValue = String(option && option.value ? option.value : "").trim();
                          const optionLabel = String(option && option.label ? option.label : optionValue).trim();
                          return `
                            <label class="check-chip">
                              <input type="checkbox" name="${escapeHTML(name)}" value="${escapeHTML(optionValue)}" ${selected.includes(optionValue) ? "checked" : ""} />
                              <span>${escapeHTML(optionLabel)}</span>
                            </label>
                          `;
                        })
                        .join("")}
                    </div>
                  </fieldset>
                `;
              }
              return `
                <label class="field">
                  <span>${escapeHTML(label)}</span>
                  <input type="${escapeHTML(type)}" name="${escapeHTML(name)}" value="${escapeHTML(value)}" ${required} ${maxLength} />
                </label>
              `;
            })
            .join("")}
          <div class="form-actions">
            <button class="btn btn-primary" type="submit">${escapeHTML(submitLabel)}</button>
            <button class="btn btn-ghost" type="button" data-modal-cancel>${escapeHTML(cancelLabel)}</button>
          </div>
        </form>
      </div>
    `;

    const finish = (value) => {
      closeAppModal();
      resolve(value);
    };

    modal.addEventListener("click", (event) => {
      if (event.target === modal) finish(null);
    });
    modal.querySelectorAll("[data-modal-close], [data-modal-cancel]").forEach((button) => {
      button.addEventListener("click", () => finish(null));
    });
    const form = modal.querySelector("[data-modal-form]");
    if (form) {
      form.addEventListener("submit", (event) => {
        event.preventDefault();
        const data = new FormData(form);
        const values = {};
        fields.forEach((field) => {
          if (String(field.type || "").trim() === "checkbox-group") {
            values[field.name] = data.getAll(field.name).map((entry) => String(entry || "").trim()).filter(Boolean);
            return;
          }
          values[field.name] = String(data.get(field.name) || "").trim();
        });
        finish(values);
      });
    }

    document.body.appendChild(modal);
    const firstInput = modal.querySelector("input, textarea, select, button");
    if (firstInput instanceof HTMLElement) {
      requestAnimationFrame(() => firstInput.focus());
    }
  });
}

async function showConfirmModal({ title = "Confirm", description = "", confirmLabel = "Confirm", cancelLabel = "Cancel" } = {}) {
  return showFormModal({ title, description, fields: [], submitLabel: confirmLabel, cancelLabel });
}

function moderationReasonOptions() {
  return MODERATION_REASONS.map((reason) => ({ value: reason, label: reason.replace(/_/g, " ") }));
}

async function showReasonNoteModal({
  title = "Submit",
  description = "",
  submitLabel = "Submit",
  noteLabel = "Note",
  defaultReason = MODERATION_REASONS[0],
  defaultNote = "",
  reasonLabel = "Reason",
} = {}) {
  const values = await showFormModal({
    title,
    description,
    submitLabel,
    fields: [
      {
        type: "select",
        name: "reason",
        label: reasonLabel,
        required: true,
        value: defaultReason,
        options: moderationReasonOptions(),
      },
      {
        type: "textarea",
        name: "note",
        label: noteLabel,
        required: true,
        maxLength: 2000,
        value: defaultNote,
      },
    ],
  });
  if (!values) return null;
  return {
    reason: String(values.reason || "").trim(),
    note: String(values.note || "").trim(),
  };
}

async function showNoteModal({
  title = "Submit",
  description = "",
  submitLabel = "Submit",
  noteLabel = "Note",
  defaultNote = "",
  required = true,
} = {}) {
  const values = await showFormModal({
    title,
    description,
    submitLabel,
    fields: [
      {
        type: "textarea",
        name: "note",
        label: noteLabel,
        required,
        maxLength: 2000,
        value: defaultNote,
      },
    ],
  });
  if (!values) return null;
  return {
    note: String(values.note || "").trim(),
  };
}

function syncNotificationButton() {
  const button = document.querySelector("[data-action='open-notifications']");
  if (!button) return;

  const totalUnread = getTotalUnreadCount();
  button.classList.toggle("has-notifications", totalUnread > 0);
  button.setAttribute("aria-label", getNotificationsLabel(totalUnread));

  const existingDot = button.querySelector(".notif-dot");
  if (totalUnread > 0) {
    if (!existingDot) {
      button.insertAdjacentHTML("beforeend", '<span class="notif-dot" aria-hidden="true"></span>');
    }
    return;
  }

  if (existingDot) {
    existingDot.remove();
  }
}

function normalizeDMPeer(peer) {
  if (!peer || typeof peer !== "object") return null;
  const id = normalizeUserID(peer.id);
  const username = normalizeUsername(peer.username);
  const displayName = String(peer.displayName || peer.display_name || "").trim();
  const lastMessageAt = Number(peer.lastMessageAt ?? peer.last_message_at ?? 0);
  const lastMessageID = normalizeUserID(peer.lastMessageId ?? peer.last_message_id ?? "");
  const lastMessageFromUserID = normalizeUserID(peer.lastMessageFromUserId ?? peer.last_message_from_user_id ?? "");
  const lastMessagePreview = String(peer.lastMessagePreview ?? peer.last_message_preview ?? "").trim();
  const lastMessageHasAttachment = Boolean(peer.lastMessageHasAttachment ?? peer.last_message_has_attachment);
  const unreadCount = Math.max(0, Math.trunc(Number(peer.unreadCount ?? peer.unread_count ?? 0) || 0));
  if (!id || !username) return null;
  return {
    id,
    username,
    displayName,
    lastMessageAt: Number.isFinite(lastMessageAt) && lastMessageAt > 0 ? lastMessageAt : 0,
    lastMessageID,
    lastMessageFromUserID,
    lastMessagePreview,
    lastMessageHasAttachment,
    unreadCount,
  };
}

function getDMPeerLabel(peer) {
  return getDisplayNameOrUsername(peer);
}

function sortDMPeers(peers) {
  return [...(Array.isArray(peers) ? peers : [])].sort((a, b) => {
    const lastA = Number(a && a.lastMessageAt ? a.lastMessageAt : 0);
    const lastB = Number(b && b.lastMessageAt ? b.lastMessageAt : 0);
    if (lastA !== lastB) return lastB - lastA;

    const labelA = getDMPeerLabel(a).toLocaleLowerCase();
    const labelB = getDMPeerLabel(b).toLocaleLowerCase();
    if (labelA !== labelB) return labelA.localeCompare(labelB, undefined, { sensitivity: "base" });

    return normalizeUserID(a && a.id).localeCompare(normalizeUserID(b && b.id), undefined, { numeric: true, sensitivity: "base" });
  });
}

function renderDMPeersList(peers, emptyLabel) {
  if (!Array.isArray(peers) || peers.length === 0) {
    return `<div class="dm-peers-empty">${escapeHTML(emptyLabel)}</div>`;
  }

  return peers
    .map((peer) => {
      const id = normalizeUserID(peer && peer.id);
      const username = normalizeUsername(peer && peer.username);
      const label = getDMPeerLabel(peer);
      const isOnline = state.onlineUserIDs.has(id);
      const hasUnread = Number(state.dmUnreadByPeer[id] || 0) > 0;
      const lastMessageAt = Number(peer && peer.lastMessageAt ? peer.lastMessageAt : 0);
      const activityLabel = lastMessageAt > 0 ? formatDate(new Date(lastMessageAt * 1000).toISOString()) : "No messages yet";
      if (!id || !username) return "";
      return `
        <button
          class="dm-peer-item ${id === state.dmPeerID ? "is-active" : ""}"
          type="button"
          data-action="dm-open"
          data-dm-open="${escapeHTML(id)}"
        >
          <div class="dm-peer-main">
            <div class="dm-peer-head">
              <div class="dm-peer-title">
                <span class="dm-peer-status-dot ${isOnline ? "is-online" : "is-offline"}" aria-hidden="true"></span>
                <strong>${escapeHTML(label)}</strong>
                ${hasUnread ? '<span class="dm-peer-unread-dot" aria-label="Unread messages"></span>' : ""}
              </div>
            </div>
            <div class="dm-peer-subhead">
              <span>@${escapeHTML(username)}</span>
              <span>${escapeHTML(activityLabel)}</span>
            </div>
          </div>
        </button>
      `;
    })
    .join("");
}

function renderDMPeersContent() {
  if (!state.user) return "";

  const sortedPeers = sortDMPeers(state.dmPeers);

  return `
    <div class="section-row">
      <h2>Conversations</h2>
      <p>${sortedPeers.length} total</p>
    </div>
    <div class="dm-peers-scroll">
      <div class="dm-peers-list">
        ${renderDMPeersList(sortedPeers, "No conversations yet")}
      </div>
    </div>
  `;
}

function renderDMViewContent() {
  if (!state.user) return "";

  if (!state.dmPeerID) {
    return `
      <div class="section-row">
        <h2>Direct Messages</h2>
        <p>Select a conversation to open the thread.</p>
      </div>
    `;
  }

  const peerName = getUserDisplayName(state.dmPeerID);
  const messages = Array.isArray(state.dmMessages) ? state.dmMessages : [];

  return `
    <div class="section-row">
      <h2>Direct Messages</h2>
      <div class="form-actions">
        <span class="side-note">${escapeHTML(peerName)}</span>
        <button class="btn btn-ghost btn-compact" type="button" data-action="dm-close">Close</button>
      </div>
    </div>
    ${
      state.dmLoading
        ? `<div class="side-note">Loading conversation...</div>`
        : `
          <div id="dm-thread" class="dm-thread">
            ${state.dmLoadingOlder ? `<div class="dm-thread-status">Loading older messages...</div>` : ""}
            ${messages.length ? messages.map(renderDMMessage).join("") : `<div class="dm-thread-empty">${renderEmpty("No messages yet", "Start the conversation.")}</div>`}
          </div>
        `
    }
    <div id="dm-typing-indicator" class="typing-indicator-slot">
      ${renderTypingIndicator(getDMTypingLabel(state.dmPeerID))}
    </div>
    <form id="dm-form" class="form-stack">
      <label class="field">
        <span>Message</span>
        <textarea name="body" rows="3" ${state.dmLoading ? "disabled" : ""}></textarea>
      </label>
      <input id="dm-attachment-input" type="file" accept="image/jpeg,image/png,image/gif" hidden ${state.dmLoading || state.dmAttachmentUploading ? "disabled" : ""} />
      <div id="dm-attachment-preview">
        ${renderAttachmentPreview(state.dmDraftAttachment, "dm")}
      </div>
      <div class="form-actions">
        <button class="btn btn-ghost btn-compact attachment-picker-btn" type="button" data-action="dm-pick-attachment" ${state.dmLoading || state.dmAttachmentUploading ? "disabled" : ""}>${icon("paperclip")} ${state.dmAttachmentUploading ? "Uploading..." : "Attach image"}</button>
        <button class="btn btn-primary" type="submit" ${state.dmLoading ? "disabled" : ""}>${icon("send")} Send</button>
      </div>
      <div id="dm-error"></div>
    </form>
  `;
}

function renderDMMessage(message) {
  const isOutgoing = normalizeUserID(message?.from?.id) === getCurrentUserID();
  const fromName = String(message?.from?.name || getUserDisplayName(message?.from?.id)).trim() || "user";
  const directionClass = isOutgoing ? "dm-msg--outgoing" : "dm-msg--incoming";
  const attachmentMarkup = renderImageAttachment(message && message.attachment, "dm-message-image", "Attached image");
  const body = String(message && message.body ? message.body : "").trim();
  return `
    <div class="dm-msg ${directionClass}">
      <div class="dm-msg-body">
        <div class="dm-meta">${escapeHTML(fromName)} · ${escapeHTML(formatDate(message.createdAt))}</div>
        ${attachmentMarkup ? `<div class="dm-attachment">${attachmentMarkup}</div>` : ""}
        ${body ? `<div class="dm-bubble">${escapeHTML(body)}</div>` : ""}
      </div>
    </div>
  `;
}

function normalizeDMMessage(message) {
  if (!message || typeof message !== "object") return null;
  const id = normalizeUserID(message.id);
  const fromID = normalizeUserID(message.from && message.from.id);
  const toID = normalizeUserID(message.to && message.to.id);
  const body = String(message.body || "").trim();
  const createdAt = message.createdAt || message.created_at || "";
  const attachment = normalizeAttachment(message.attachment);
  if (!id || !fromID || !toID || (!body && !attachment) || !createdAt) return null;

  return {
    id,
    from: {
      id: fromID,
      name: String((message.from && message.from.name) || "").trim(),
    },
    to: {
      id: toID,
    },
    body,
    attachment,
    createdAt,
  };
}

function getDMMessageTimestampSeconds(message) {
  const created = new Date(message && message.createdAt);
  if (!Number.isFinite(created.getTime())) return 0;
  return Math.floor(created.getTime() / 1000);
}

function getDMConversationCursor(messages = state.dmMessages) {
  if (!Array.isArray(messages) || messages.length === 0) return null;
  const oldestMessage = messages[0];
  const ts = getDMMessageTimestampSeconds(oldestMessage);
  const id = Number.parseInt(normalizeUserID(oldestMessage && oldestMessage.id), 10);
  if (!ts || !Number.isFinite(id) || id <= 0) return null;
  return { ts, id };
}

function isDMThreadNearBottom(thread, threshold = 40) {
  if (!thread) return true;
  return thread.scrollHeight - thread.clientHeight - thread.scrollTop <= threshold;
}

function bindDMThreadScroll(thread = document.getElementById("dm-thread")) {
  if (!thread) return;
  thread.addEventListener(
    "scroll",
    () => {
      if (thread.scrollTop > 40) return;
      if (state.dmLoading || state.dmLoadingOlder || !state.dmHasMore) return;
      const now = Date.now();
      if (now - Number(state.dmOlderLoadAt || 0) < DM_HISTORY_THROTTLE_MS) return;
      state.dmOlderLoadAt = now;
      void loadOlderDMConversation().catch((err) => {
        debugWSWarn("dm history load failed", err);
      });
    },
    { passive: true }
  );
}

function syncDMView(options = {}) {
  const panel = document.getElementById("dm-view");
  if (!panel) return;
  const previousThread = panel.querySelector("#dm-thread");
  const previousScrollTop = Number.isFinite(options.prevScrollTop) ? options.prevScrollTop : previousThread ? previousThread.scrollTop : 0;
  const previousScrollHeight = Number.isFinite(options.prevScrollHeight) ? options.prevScrollHeight : previousThread ? previousThread.scrollHeight : 0;
  panel.innerHTML = renderDMViewContent();
  const thread = panel.querySelector("#dm-thread");
  if (!thread) return;

  if (options.scrollMode === "bottom") {
    thread.scrollTop = thread.scrollHeight;
  } else if (options.scrollMode === "prepend") {
    thread.scrollTop = Math.max(0, thread.scrollHeight - previousScrollHeight + previousScrollTop);
  } else if (options.scrollMode === "preserve") {
    thread.scrollTop = Math.max(0, previousScrollTop);
  }

  bindDMThreadScroll(thread);
  syncDMTypingIndicator();
}

function syncDMComposerAttachmentPreview() {
  const preview = document.getElementById("dm-attachment-preview");
  if (!preview) return;
  preview.innerHTML = renderAttachmentPreview(state.dmDraftAttachment, "dm");
}

function syncDMPeersPanel() {
  const panel = document.getElementById("dm-peers-panel");
  if (!panel) return;
  panel.innerHTML = renderDMPeersContent();
}

function syncPostAttachmentPreview() {
  const preview = document.getElementById("post-attachment-preview");
  if (!preview) return;
  preview.innerHTML = renderAttachmentPreview(state.postDraftAttachment, "post");
}

function syncPostAttachmentControls() {
  const picker = document.querySelector("[data-action='post-pick-attachment']");
  if (picker) {
    picker.disabled = !state.user || state.postAttachmentUploading;
    picker.innerHTML = `${icon("paperclip")} ${state.postAttachmentUploading ? "Uploading..." : "Attach image"}`;
  }
  const input = document.getElementById("post-attachment-input");
  if (input) {
    input.disabled = !state.user || state.postAttachmentUploading;
  }
}

function validateAttachmentFile(file) {
  if (!file) return "";
  const size = Number(file.size || 0);
  if (Number.isFinite(size) && size > MAX_ATTACHMENT_BYTES) {
    return ATTACHMENT_TOO_BIG_MESSAGE;
  }
  return "";
}

async function readUploadError(response) {
  let text = "";
  try {
    text = await response.text();
  } catch (_) {
    text = "";
  }

  const body = String(text || "").trim();
  if (!body) {
    return { code: "", message: "" };
  }

  try {
    const payload = JSON.parse(body);
    return {
      code: typeof payload.error === "string" ? payload.error.trim() : "",
      message: typeof payload.message === "string" ? payload.message.trim() : "",
      text: body,
    };
  } catch (_) {
    return { code: "", message: body, text: body };
  }
}

async function uploadImageAttachment(file) {
  const validationMessage = validateAttachmentFile(file);
  if (validationMessage) {
    const err = new Error(validationMessage);
    err.status = 413;
    throw err;
  }

  const payload = new FormData();
  payload.append("file", file);

  let response;
  try {
    response = await fetch("/api/attachments", {
      method: "POST",
      body: payload,
    });
  } catch (_) {
    throw new Error("Failed to upload image.");
  }

  if (!response.ok) {
    const errorPayload = await readUploadError(response);
    const apiError = String(errorPayload.code || "").trim();
    const apiMessage = String(errorPayload.message || "").trim();
    const message = apiMessage || apiError || "Failed to upload image.";
    const err = new Error(message);
    err.status = response.status;
    err.code = apiError;
    err.apiMessage = apiMessage;

    const isSessionEndedUnauthorized = response.status === 401 && apiError === "unauthorized" && apiMessage;
    if (isSessionEndedUnauthorized) {
      err.handled = true;
      handleSessionEndedUX(apiMessage);
    }

    throw err;
  }

  return normalizeAttachment(await response.json());
}

function renderPresencePanel() {
  return `
    <div id="presence-panel" class="sidebar-block">
      ${renderPresencePanelContent()}
    </div>
  `;
}

function renderPresencePanelContent() {
  if (!state.user) {
    return `
      <div class="sidebar-title">Presence</div>
      <div class="side-note">Login to see who is online.</div>
    `;
  }

  const onlineUsers = [];
  const offlineUsers = [];

  (state.users || []).forEach((user) => {
    const id = normalizeUserID(user && user.id);
    if (!id) return;
    if (state.onlineUserIDs.has(id)) {
      onlineUsers.push(user);
      return;
    }
    offlineUsers.push(user);
  });

  return `
    <div class="sidebar-title">Presence</div>
    <div class="side-note">${onlineUsers.length} online / ${offlineUsers.length} offline</div>
    <div class="sidebar-title">Online</div>
    <div class="side-tag-list">
      ${renderPresenceUsers(onlineUsers, "Nobody online")}
    </div>
    <div class="sidebar-title">Offline</div>
    <div class="side-tag-list">
      ${renderPresenceUsers(offlineUsers, "Nobody offline")}
    </div>
  `;
}

function renderPresenceUsers(users, emptyLabel) {
  if (!Array.isArray(users) || users.length === 0) {
    return `<span class="side-tag">${escapeHTML(emptyLabel)}</span>`;
  }

  return users
    .map((user) => {
      const id = normalizeUserID(user && user.id);
      const username = normalizeUsername(user && user.username);
      const name = getDisplayNameOrUsername(user) || `user-${id}`;
      if (!id) return "";
      return `
        <div class="presence-user-row">
          <button
            class="side-filter-btn presence-user-name"
            type="button"
            data-action="open-profile"
            data-username="${escapeHTML(username)}"
          >${escapeHTML(name)}${id === getCurrentUserID() ? " (you)" : ""}</button>
          ${
            id === getCurrentUserID()
              ? ""
              : `
                <button
                  class="side-filter-btn presence-user-dm ${state.dmPeerID === id ? "is-active" : ""}"
                  type="button"
                  data-action="dm-open"
                  data-dm-open="${escapeHTML(id)}"
                >DM</button>
              `
          }
        </div>
      `;
    })
    .join("");
}

function syncPresencePanel() {
  const panel = document.getElementById("presence-panel");
  if (!panel) return;
  panel.innerHTML = renderPresencePanelContent();
}

function renderEmpty(title, text, href, cta) {
  return `
    <div class="surface empty-box">
      <div class="empty-symbol">${icon("comment")}</div>
      <h3>${escapeHTML(title)}</h3>
      <p>${escapeHTML(text)}</p>
      ${href ? `<a class="btn btn-primary" data-link href="${escapeHTML(href)}">${escapeHTML(cta || "Open")}</a>` : ""}
    </div>
  `;
}

function isCurrentUserOwner(entity) {
  const ownerID = normalizeUserID(entity && (entity.user_id ?? entity.userId));
  return Boolean(ownerID) && ownerID === getCurrentUserID();
}

function isDeletedComment(comment) {
  return Boolean(comment && (comment.deleted_at || comment.deletedAt));
}

function isDeletedPost(post) {
  return Boolean(post && (post.deleted_at || post.deletedAt));
}

function isUnderReviewPost(post) {
  return Boolean(post && (post.under_review || post.underReview));
}

function isProtectedPost(post) {
  return Boolean(post && (post.delete_protected || post.deleteProtected));
}

function renderContentFlags(flags) {
  const items = Array.isArray(flags) ? flags.filter(Boolean) : [];
  if (!items.length) return "";
  return `
    <div class="content-flag-row">
      ${items.map((flag) => `<span class="content-flag">${escapeHTML(flag)}</span>`).join("")}
    </div>
  `;
}

function renderPostFlags(post) {
  const flags = [];
  if (isUnderReviewPost(post)) flags.push("under review");
  if (isDeletedPost(post)) flags.push("[deleted]");
  if (isProtectedPost(post)) flags.push("protected");
  if (post && post.approved_at) flags.push("approved");
  return renderContentFlags(flags);
}

function renderCommentFlags(comment) {
  const flags = [];
  if (isDeletedComment(comment)) flags.push("[deleted]");
  return renderContentFlags(flags);
}

function renderPostModerationActions(post, { compact = false } = {}) {
  if (!post) return "";
  const role = getCurrentUserRole();
  const buttonClass = compact ? "btn btn-ghost btn-compact" : "action-pill action-pill-muted";
  const actions = [];
  if (state.user && !isDeletedPost(post)) {
    actions.push(`<button class="${buttonClass}" type="button" data-action="open-report-modal" data-target-type="post" data-target-id="${escapeHTML(String(post.id))}">Report</button>`);
  }
  if (isCurrentUserOwner(post) && isDeletedPost(post)) {
    actions.push(`<button class="${buttonClass}" type="button" data-action="open-appeal-modal" data-target-type="post" data-target-id="${escapeHTML(String(post.id))}">Appeal</button>`);
  }
  if (state.user && isStaffRole(role)) {
    if (isUnderReviewPost(post)) {
      actions.push(`<button class="${buttonClass}" type="button" data-action="queue-approve-post" data-post-id="${escapeHTML(String(post.id))}">Approve</button>`);
    }
    if (!isDeletedPost(post)) {
      actions.push(`<button class="${buttonClass}" type="button" data-action="open-soft-delete-modal" data-target-type="post" data-target-id="${escapeHTML(String(post.id))}">Soft delete</button>`);
    } else {
      actions.push(`<button class="${buttonClass}" type="button" data-action="restore-content" data-target-type="post" data-target-id="${escapeHTML(String(post.id))}">Restore</button>`);
    }
    if ((role === "admin" || role === "owner") && !isDeletedPost(post)) {
      actions.push(`<button class="${buttonClass}" type="button" data-action="edit-post-categories" data-post-id="${escapeHTML(String(post.id))}">Change categories</button>`);
    }
    if (role === "admin" || role === "owner") {
      actions.push(`<button class="${buttonClass}" type="button" data-action="open-hard-delete-modal" data-target-type="post" data-target-id="${escapeHTML(String(post.id))}">Hard delete</button>`);
    }
    if (role === "owner" && !isDeletedPost(post)) {
      actions.push(`<button class="${buttonClass}" type="button" data-action="toggle-delete-protection" data-post-id="${escapeHTML(String(post.id))}" data-next-protected="${isProtectedPost(post) ? "0" : "1"}">${isProtectedPost(post) ? "Remove protection" : "Protect from delete"}</button>`);
    }
  }
  if (!actions.length) return "";
  return `<div class="action-row action-row-secondary">${actions.join("")}</div>`;
}

function renderCommentModerationActions(comment, { compact = false } = {}) {
  if (!comment) return "";
  const role = getCurrentUserRole();
  const buttonClass = compact ? "btn btn-ghost btn-compact" : "action-pill action-pill-muted";
  const actions = [];
  if (state.user && !isDeletedComment(comment)) {
    actions.push(`<button class="${buttonClass}" type="button" data-action="open-report-modal" data-target-type="comment" data-target-id="${escapeHTML(String(comment.id))}">Report</button>`);
  }
  if (isCurrentUserOwner(comment) && isDeletedComment(comment)) {
    actions.push(`<button class="${buttonClass}" type="button" data-action="open-appeal-modal" data-target-type="comment" data-target-id="${escapeHTML(String(comment.id))}">Appeal</button>`);
  }
  if (state.user && isStaffRole(role)) {
    if (!isDeletedComment(comment)) {
      actions.push(`<button class="${buttonClass}" type="button" data-action="open-soft-delete-modal" data-target-type="comment" data-target-id="${escapeHTML(String(comment.id))}">Soft delete</button>`);
    } else {
      actions.push(`<button class="${buttonClass}" type="button" data-action="restore-content" data-target-type="comment" data-target-id="${escapeHTML(String(comment.id))}">Restore</button>`);
    }
    if (role === "admin" || role === "owner") {
      actions.push(`<button class="${buttonClass}" type="button" data-action="open-hard-delete-modal" data-target-type="comment" data-target-id="${escapeHTML(String(comment.id))}">Hard delete</button>`);
    }
  }
  if (!actions.length) return "";
  return `<div class="action-row action-row-secondary">${actions.join("")}</div>`;
}

function renderProfileRoleActions(profile, isSelf) {
  if (!profile) return "";
  const viewerRole = getCurrentUserRole();
  const targetActions = !isSelf ? getAvailableRoleActions(profile.role, profile.id) : [];
  const selfActions = [];
  if (isSelf) {
    if (normalizeRole(profile.role) === "user") {
      selfActions.push(`<button class="btn btn-ghost btn-compact" type="button" data-action="request-role" data-requested-role="moderator">Request moderator</button>`);
    } else if (normalizeRole(profile.role) === "moderator") {
      selfActions.push(`<button class="btn btn-ghost btn-compact" type="button" data-action="request-role" data-requested-role="admin">Request admin</button>`);
    }
  }
  const directActions = targetActions.map((action) => `
    <button class="btn btn-ghost btn-compact" type="button" data-action="change-user-role" data-user-id="${escapeHTML(String(profile.id))}" data-next-role="${escapeHTML(action.role)}">${escapeHTML(action.label)}</button>
  `);
  const buttons = [...selfActions, ...directActions];
  if (!buttons.length || (!isSelf && !isAdminRole(viewerRole))) return "";
  return `<div class="profile-actions">${buttons.join("")}</div>`;
}

function getActiveCommentReply(postID) {
  const pending = state.pendingCommentReply;
  if (!pending || !postID) return null;
  return String(pending.postID) === String(postID) ? pending : null;
}

function clearPendingCommentReply() {
  state.pendingCommentReply = null;
}

function canEditComment(comment) {
  if (isDeletedComment(comment)) return false;
  if (!isCurrentUserOwner(comment)) return false;
  const createdAt = Date.parse(comment && (comment.created_at || comment.createdAt || ""));
  if (!Number.isFinite(createdAt)) return false;
  return Date.now() - createdAt <= 30 * 60 * 1000;
}

function renderPostCard(post) {
  const authorUsername = getAuthorUsername(post);
  const author = getAuthorDisplay(post);
  const categoriesMarkup = categoryTags(post.categories);
  const viewsCount = post.views_count ?? post.views ?? 0;
  const attachmentMarkup = renderImageAttachment(post && post.attachment, "post-card-image", post && post.title ? `${post.title} attachment` : "Post attachment");
  const isOwner = isCurrentUserOwner(post);
  return `
    <article class="surface post-card">
      <div class="post-card-main">
        <h3 class="post-card-title"><a data-link href="/post/${post.id}">${escapeHTML(post.title)}</a></h3>
        <div class="author-line">
          ${avatarMarkup(author, post.avatarUrl, "sm")}
          <div class="author-meta">
            <div class="author-meta-row">
              <a class="author-name author-link" data-link href="${escapeHTML(getProfilePath(authorUsername))}">${escapeHTML(author)}</a>
              ${renderStaffBadges(getUserBadges(post))}
              ${
                categoriesMarkup
                  ? `
                    <span class="meta-sep" aria-hidden="true">&bull;</span>
                    <div class="tag-row tag-row-inline">${categoriesMarkup}</div>
                  `
                  : ""
              }
            </div>
            <div class="meta-line">${escapeHTML(formatDate(post.created_at))}</div>
          </div>
        </div>
        ${renderPostFlags(post)}
        <p class="post-card-body">${escapeHTML(post.body)}</p>
        ${attachmentMarkup ? `<div class="post-card-media">${attachmentMarkup}</div>` : ""}
        <div class="action-row" data-post="${post.id}">
          <button class="action-pill" type="button" data-action="like" aria-label="Like">${icon("like")} ${post.likes || 0}</button>
          <button class="action-pill" type="button" data-action="dislike" aria-label="Dislike">${icon("dislike")} ${post.dislikes || 0}</button>
          <a class="action-pill" data-link href="/post/${post.id}" aria-label="Comments">${icon("comment")} ${post.comments_count ?? (Array.isArray(post.comments) ? post.comments.length : 0)}</a>
          ${isOwner ? `<a class="action-pill" data-link href="/post/${post.id}?edit=1">Edit</a>` : ""}
          ${isOwner ? `<button class="action-pill" type="button" data-action="delete-post" data-post-id="${escapeHTML(String(post.id))}">Delete</button>` : ""}
        </div>
        ${renderPostModerationActions(post)}
      </div>
      <div class="post-card-side">
        <button class="post-side-btn post-side-menu" type="button" data-action="under-construction" aria-label="More actions">
          ${icon("menu")}
        </button>
        <div class="post-card-side-bottom">
          <button class="post-side-pill" type="button" data-action="under-construction" aria-label="Viewed">
            ${icon("eye")}
            <span>${escapeHTML(formatCompactCount(viewsCount))}</span>
          </button>
          <button class="post-side-pill post-side-pill-icon" type="button" data-action="under-construction" aria-label="Share">
            ${icon("share")}
          </button>
        </div>
      </div>
    </article>
  `;
}

function renderImageAttachment(attachment, className, alt) {
  const media = normalizeAttachment(attachment);
  if (!media) return "";
  const classes = ["attachment-image"];
  if (className) classes.push(className);
  return `<img class="${classes.join(" ")}" src="${escapeHTML(media.url)}" alt="${escapeHTML(alt || "Attached image")}" loading="lazy" />`;
}

function renderAttachmentPreview(attachment, variant) {
  const media = normalizeAttachment(attachment);
  if (!media) return "";
  const removeAction = variant === "post" ? "post-remove-attachment" : "dm-remove-attachment";
  return `
    <div class="attachment-preview-card">
      ${renderImageAttachment(media, "attachment-preview-image", "Selected image")}
      <div class="attachment-preview-meta">
        <strong>${escapeHTML(media.mime)}</strong>
        <span>${escapeHTML(formatAttachmentSize(media.size))}</span>
      </div>
      <button class="btn btn-ghost btn-compact" type="button" data-action="${removeAction}">Remove</button>
    </div>
  `;
}

function bindHeaderActions() {
  document.querySelectorAll("[data-action='toggle-theme']").forEach((el) => {
    el.addEventListener("click", toggleTheme);
  });
  syncNotificationButton();
  if (state.user) {
    void loadDMPeers();
  }

  const logoutBtn = document.querySelector("[data-action='logout']");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", async () => {
      try {
        await apiFetch("/api/logout", { method: "POST" });
      } catch (_) {
        // ignore
      }
      clearAuthenticatedState();
      navigate("/login");
    });
  }

  const globalSearchForm = document.getElementById("global-feed-search-form");
  if (globalSearchForm) {
    const searchInput = globalSearchForm.querySelector("input[name='q']");
    const syncSearchClear = () => {
      if (!(searchInput instanceof HTMLInputElement)) return;
      const nextQ = searchInput.value.trim();
      if (nextQ !== "") return;
      const activePostID = getActivePostIDFromPath();
      if (activePostID) {
        const key = String(activePostID);
        if (!state.commentSearchByPost[key]) return;
        state.commentSearchByPost[key] = "";
        router();
        return;
      }
      if (!state.filters.q) return;
      state.filters.q = "";
      if (location.pathname === "/") router();
    };

    globalSearchForm.addEventListener("submit", (e) => {
      e.preventDefault();
      const data = new FormData(globalSearchForm);
      const q = String(data.get("q") || "").trim();
      const activePostID = getActivePostIDFromPath();
      if (activePostID) {
        state.commentSearchByPost[String(activePostID)] = q;
        router();
        return;
      }
      state.filters.q = q;
      if (location.pathname !== "/") {
        navigate("/");
        return;
      }
      router();
    });

    if (searchInput) {
      searchInput.addEventListener("input", syncSearchClear);
      searchInput.addEventListener("search", syncSearchClear);
    }
  }
}

function bindFeedFilters() {
  document.querySelectorAll("[data-cat-toggle]").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      const id = e.currentTarget.getAttribute("data-cat-toggle");
      if (state.filters.cat.has(id)) state.filters.cat.delete(id);
      else state.filters.cat.add(id);
      router();
    });
  });

  document.querySelectorAll("[data-filter-toggle]").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      const key = e.currentTarget.getAttribute("data-filter-toggle");
      if (key === "mine") {
        state.filters.mine = !state.filters.mine;
        if (state.filters.mine) state.filters.liked = false;
      } else if (key === "liked") {
        state.filters.liked = !state.filters.liked;
        if (state.filters.liked) state.filters.mine = false;
      }
      router();
    });
  });
}

function bindPostReactions() {
  document.querySelectorAll("[data-post]").forEach((bar) => {
    bar.addEventListener("click", async (e) => {
      const btn = e.target.closest("[data-action]");
      if (!btn) return;
      const action = btn.getAttribute("data-action");
      if (action !== "like" && action !== "dislike") return;
      const postID = bar.getAttribute("data-post");
      try {
        await apiFetch(`/api/posts/${postID}/react`, {
          method: "POST",
          body: JSON.stringify({ value: action === "like" ? 1 : -1 }),
        });
        router();
      } catch (err) {
        if (err && err.handled) return;
        if (err && err.status === 401 && err.code === "unauthorized") {
          alert("Please login or register to use this function");
          return;
        }
        alert(err.message);
      }
    });
  });
}

function bindCommentReactions() {
  document.querySelectorAll("[data-comment]").forEach((bar) => {
    bar.addEventListener("click", async (e) => {
      const btn = e.target.closest("[data-action]");
      if (!btn) return;
      const action = btn.getAttribute("data-action");
      if (action !== "like" && action !== "dislike") return;
      const commentID = bar.getAttribute("data-comment");
      try {
        await apiFetch(`/api/comments/${commentID}/react`, {
          method: "POST",
          body: JSON.stringify({ value: action === "like" ? 1 : -1 }),
        });
        router();
      } catch (err) {
        if (err && err.handled) return;
        if (err && err.status === 401 && err.code === "unauthorized") {
          alert("Please login or register to use this function");
          return;
        }
        alert(err.message);
      }
    });
  });
}

function bindCommentReplyActions() {
  document.querySelectorAll("[data-action='reply-comment']").forEach((btn) => {
    btn.addEventListener("click", () => {
      if (!state.user) {
        alert("Login to reply.");
        return;
      }
      const id = btn.getAttribute("data-reply-comment-id");
      const author = btn.getAttribute("data-reply-author") || "comment";
      if (!id) return;
      const activePostID = getActivePostIDFromPath();
      if (!activePostID) return;
      state.pendingCommentReply = {
        postID: String(activePostID),
        commentID: String(id),
        author: String(author || "comment"),
        draft: "",
      };
      router();
    });
  });

  document.querySelectorAll("[data-action='cancel-comment-reply']").forEach((button) => {
    button.addEventListener("click", () => {
      clearPendingCommentReply();
      stopLocalTyping(TYPING_SCOPE_POST, { send: true });
      router();
    });
  });
}

function bindCommentComposeForms(postID) {
  document.querySelectorAll("[data-comment-compose-form]").forEach((form) => {
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      if (!state.user) return;
      const errorBox = form.querySelector(".comment-compose-error");
      if (errorBox) errorBox.innerHTML = "";
      const data = new FormData(form);
      const parentRaw = String(data.get("parent_id") || "").trim();
      const parentID = parentRaw ? Number(parentRaw) : null;
      try {
        await apiFetch(`/api/posts/${postID}/comments`, {
          method: "POST",
          body: JSON.stringify({
            body: data.get("body"),
            ...(Number.isFinite(parentID) && parentID > 0 ? { parent_id: parentID } : {}),
          }),
        });
        stopLocalTyping(TYPING_SCOPE_POST, { send: true, targetID: String(postID) });
        clearPendingCommentReply();
        router();
      } catch (err) {
        if (err && err.handled) return;
        if (errorBox) errorBox.innerHTML = renderNotice(err.message || "Failed to add comment.");
      }
    });
  });

  const replyTextarea = document.querySelector("[data-inline-reply-form] textarea[name='body']");
  if (replyTextarea instanceof HTMLTextAreaElement) {
    const value = replyTextarea.value || "";
    requestAnimationFrame(() => {
      replyTextarea.focus();
      replyTextarea.setSelectionRange(value.length, value.length);
    });
  }
}

function bindCommentOwnerActions() {
  document.querySelectorAll("[data-action='edit-comment']").forEach((button) => {
    button.addEventListener("click", () => {
      const commentID = button.getAttribute("data-edit-comment-id");
      if (!commentID) return;
      const commentCard = button.closest(".comment-card");
      const text = commentCard ? commentCard.querySelector(".comment-text") : null;
      state.editingCommentID = commentID;
      state.editingCommentDraft = text ? text.textContent || "" : "";
      router();
    });
  });

  document.querySelectorAll("[data-action='cancel-edit-comment']").forEach((button) => {
    button.addEventListener("click", () => {
      state.editingCommentID = "";
      state.editingCommentDraft = "";
      router();
    });
  });

  document.querySelectorAll("[data-comment-edit-form]").forEach((form) => {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const commentID = form.getAttribute("data-comment-edit-form");
      if (!commentID) return;
      const data = new FormData(form);
      try {
        await apiFetch(`/api/comments/${commentID}`, {
          method: "PUT",
          body: JSON.stringify({ body: data.get("body") }),
        });
        state.editingCommentID = "";
        state.editingCommentDraft = "";
        router();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to update comment.");
      }
    });
  });

  document.querySelectorAll("[data-action='delete-comment']").forEach((button) => {
    button.addEventListener("click", async () => {
      const commentID = button.getAttribute("data-delete-comment-id");
      if (!commentID) return;
      if (!confirm("Delete this comment?")) return;
      try {
        await apiFetch(`/api/comments/${commentID}`, { method: "DELETE" });
        if (normalizeUserID(state.editingCommentID) === normalizeUserID(commentID)) {
          state.editingCommentID = "";
          state.editingCommentDraft = "";
        }
        router();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to delete comment.");
      }
    });
  });
}

function bindCommentComposerAutosize() {
  document.querySelectorAll("[data-comment-compose-form] textarea[name='body']").forEach((textarea) => {
    if (!(textarea instanceof HTMLTextAreaElement)) return;

    const collapsedHeight = 96;
    const expandedBaseHeight = 140;

    const syncHeight = (baseHeight) => {
      textarea.style.height = "0px";
      const target = Math.max(baseHeight, textarea.scrollHeight);
      textarea.style.height = `${target}px`;
    };

    const expand = () => {
      textarea.classList.add("is-expanded");
      syncHeight(expandedBaseHeight);
    };

    const collapseIfEmpty = () => {
      if (textarea.value.trim()) return;
      textarea.classList.remove("is-expanded");
      syncHeight(collapsedHeight);
    };

    textarea.style.height = `${collapsedHeight}px`;
    textarea.style.overflowY = "hidden";
    syncHeight(textarea.value.trim() ? expandedBaseHeight : collapsedHeight);

    textarea.addEventListener("focus", expand);
    textarea.addEventListener("input", expand);
    textarea.addEventListener("blur", collapseIfEmpty);
  });
}

async function ensureUser(force = false) {
  if (state.user && !force) {
    if (!state.usersLoaded) {
      await ensureUsersLoaded();
    }
    if (!state.center.summaryLoaded) {
      await ensureNotificationSummary();
    }
    ensureRealtimeSocket();
    return;
  }

  const previousUserID = normalizeUserID(state.user && state.user.id);
  try {
    state.user = normalizePersonRecord(await apiFetch("/api/me"));
  } catch (_) {
    clearAuthenticatedState();
    return;
  }

  const currentUserID = normalizeUserID(state.user && state.user.id);
  if (force || previousUserID !== currentUserID) {
    state.dmUnreadByPeer = loadPersistedDMUnreadState(currentUserID);
    resetCenterState();
    syncNotificationButton();
  }
  if (force || !state.usersLoaded || previousUserID !== currentUserID) {
    await ensureUsersLoaded(force || previousUserID !== currentUserID);
  }
  await ensureNotificationSummary(force || previousUserID !== currentUserID);
  if (force) closeRealtimeSocket();
  ensureRealtimeSocket();
}

async function ensureAuthProviders(force = false) {
  if (state.authProvidersLoaded && !force) {
    return;
  }

  try {
    const providers = await apiFetch("/api/auth/providers", { suppressSessionEndedUX: true });
    state.authProviders = Array.isArray(providers)
      ? providers
          .filter((provider) => provider && provider.enabled)
          .map((provider) => ({
            name: String(provider.name || "").trim().toLowerCase(),
            label: String(provider.label || "").trim(),
            enabled: Boolean(provider.enabled),
          }))
      : [];
  } catch (_) {
    state.authProviders = [];
  } finally {
    state.authProvidersLoaded = true;
  }
}

async function ensureNotificationSummary(force = false) {
  if (!state.user) {
    resetCenterState();
    syncNotificationButton();
    return;
  }
  if (state.center.summaryLoaded && !force) {
    syncNotificationButton();
    return;
  }

  try {
    const summary = await apiFetch("/api/center/summary");
    setNotificationSummary(summary);
  } catch (_) {
    setNotificationSummary({ total: 0, dm: 0, myContent: 0, subscriptions: 0, deleted: 0, reports: 0, appeals: 0, management: 0 });
  }
}

function normalizeNotificationItem(notification) {
  if (!notification || typeof notification !== "object") return null;
  const id = normalizeUserID(notification.id);
  const bucket = String(notification.bucket || "").trim();
  const type = String(notification.type || "").trim();
  const text = String(notification.text || "").trim();
  const context = String(notification.context || "").trim();
  const linkPath = String(notification.linkPath || notification.link_path || "").trim();
  const createdAt = String(notification.createdAt || notification.created_at || "").trim();
  if (!id || !bucket || !type || !text || !createdAt) return null;
  return {
    id,
    bucket,
    type,
    text,
    context,
    linkPath,
    entityType: String(notification.entityType || notification.entity_type || "").trim(),
    entityID: normalizeUserID(notification.entityId || notification.entity_id),
    postTitle: String(notification.postTitle || notification.post_title || "").trim(),
    postPreview: String(notification.postPreview || notification.post_preview || "").trim(),
    commentPreview: String(notification.commentPreview || notification.comment_preview || "").trim(),
    canAppeal: Boolean(notification.canAppeal ?? notification.can_appeal ?? false),
    entityAvailable: Boolean(notification.entityAvailable ?? notification.entity_available ?? true),
    isRead: Boolean(notification.isRead ?? notification.is_read ?? false),
    createdAt,
  };
}

async function ensureUsersLoaded(force = false) {
  if (!state.user) {
    clearPresenceState();
    return;
  }
  if (state.usersLoaded && !force) {
    syncPresencePanel();
    return;
  }

  try {
    const users = (await apiFetch("/api/users")) || [];
    state.users = Array.isArray(users)
      ? users
          .map((user) => {
            const normalized = normalizePersonRecord(user);
            if (!normalized) return null;
            return {
              ...normalized,
              name: String(user && user.name ? user.name : normalized.displayName || normalized.username).trim(),
            };
          })
          .filter((user) => user.id)
      : [];
    state.usersLoaded = true;
  } catch (_) {
    clearPresenceState();
    return;
  }

  syncPresencePanel();
}

async function loadDMPeers(force = false) {
  if (!state.user) {
    state.dmPeers = [];
    state.dmPeersLoaded = false;
    syncDMPeersPanel();
    return;
  }
  if (state.dmPeersLoaded && !force) {
    syncDMPeersPanel();
    return;
  }

  try {
    const peers = (await apiFetch("/api/dm/peers")) || [];
    const normalizedPeers = Array.isArray(peers) ? peers.map(normalizeDMPeer).filter(Boolean) : [];
    state.dmPeers = sortDMPeers(normalizedPeers);
    syncDMUnreadCache(
      normalizedPeers.reduce((acc, peer) => {
        acc[peer.id] = peer.unreadCount;
        return acc;
      }, {})
    );
    state.dmPeersLoaded = true;
  } catch (_) {
    state.dmPeers = [];
    state.dmPeersLoaded = false;
  }

  syncDMPeersPanel();
}

async function loadCenterActivity(force = false) {
  if (!state.user) {
    state.center.activityLoaded = false;
    state.center.activity = {
      posts: [],
      postsHasMore: false,
      reactions: [],
      reactionsHasMore: false,
      comments: [],
      commentsHasMore: false,
    };
    return;
  }
  if (state.center.activityLoaded && !force) return;

  const activity = await apiFetch("/api/center/activity?limit=20");
  state.center.activity = {
    posts: Array.isArray(activity && activity.posts) ? activity.posts : [],
    postsHasMore: Boolean(activity && activity.postsHasMore),
    reactions: Array.isArray(activity && activity.reactions) ? activity.reactions : [],
    reactionsHasMore: Boolean(activity && activity.reactionsHasMore),
    comments: Array.isArray(activity && activity.comments) ? activity.comments : [],
    commentsHasMore: Boolean(activity && activity.commentsHasMore),
  };
  state.center.activityLoaded = true;
}

function getNotificationBucketState(bucket) {
  const normalizedBucket = String(bucket || "").trim();
  if (!state.center.notifications[normalizedBucket]) {
    state.center.notifications[normalizedBucket] = {
      loaded: false,
      loading: false,
      items: [],
      hasMore: false,
    };
  }
  return state.center.notifications[normalizedBucket];
}

function centerNotificationAPIBucket(bucket) {
  switch (String(bucket || "").trim()) {
    case "all":
      return "";
    case "deleted":
      return "deleted";
    case "reports":
      return "reports";
    case "appeals":
      return "appeals";
    default:
      return "";
  }
}

async function loadCenterNotifications(bucket, { force = false, append = false } = {}) {
  const normalizedBucket = String(bucket || "").trim();
  const bucketState = getNotificationBucketState(normalizedBucket);
  if (!state.user) {
    bucketState.loaded = false;
    bucketState.items = [];
    bucketState.hasMore = false;
    bucketState.loading = false;
    return;
  }
  if (bucketState.loading) return;
  if (bucketState.loaded && !force && !append) return;
  if (append && !bucketState.hasMore) return;

  bucketState.loading = true;
  const offset = append ? bucketState.items.length : 0;
  try {
    const apiBucket = centerNotificationAPIBucket(normalizedBucket);
    const qs = new URLSearchParams({
      limit: "20",
      offset: String(offset),
    });
    if (apiBucket) qs.set("bucket", apiBucket);
    const response = await apiFetch(`/api/center/notifications?${qs.toString()}`);
    const items = Array.isArray(response && response.items) ? response.items.map(normalizeNotificationItem).filter(Boolean) : [];
    bucketState.items = append ? [...bucketState.items, ...items] : items;
    bucketState.hasMore = Boolean(response && response.hasMore);
    bucketState.loaded = true;
    if (response && response.summary) {
      setNotificationSummary(response.summary);
    }
  } finally {
    bucketState.loading = false;
  }
}

async function loadMyReports(force = false) {
  if (!state.user) {
    state.center.myReportsLoaded = false;
    state.center.myReports = [];
    return;
  }
  if (state.center.myReportsLoading) return;
  if (state.center.myReportsLoaded && !force) return;
  state.center.myReportsLoading = true;
  try {
    const items = await apiFetch("/api/moderation/reports?mine=1");
    state.center.myReports = Array.isArray(items) ? items : [];
    state.center.myReportsLoaded = true;
  } finally {
    state.center.myReportsLoading = false;
  }
}

async function loadMyAppeals(force = false) {
  if (!state.user) {
    state.center.appealsLoaded = false;
    state.center.appeals = [];
    state.center.appealInboxLoaded = false;
    state.center.appealInbox = [];
    return;
  }
  if (state.center.appealsLoading) return;
  if (state.center.appealsLoaded && !force) return;
  state.center.appealsLoading = true;
  try {
    const items = await apiFetch("/api/moderation/appeals?mine=1");
    state.center.appeals = Array.isArray(items) ? items : [];
    state.center.appealsLoaded = true;
  } finally {
    state.center.appealsLoading = false;
  }
}

async function loadAppealInbox(force = false) {
  if (!state.user || !isAdminRole(getCurrentUserRole())) {
    state.center.appealInboxLoaded = false;
    state.center.appealInbox = [];
    return;
  }
  if (state.center.appealInboxLoading) return;
  if (state.center.appealInboxLoaded && !force) return;
  state.center.appealInboxLoading = true;
  try {
    const items = await apiFetch("/api/moderation/appeals?status=pending");
    state.center.appealInbox = Array.isArray(items) ? items : [];
    state.center.appealInboxLoaded = true;
  } finally {
    state.center.appealInboxLoading = false;
  }
}

async function loadModerationQueue(force = false) {
  if (!state.user || !isStaffRole(getCurrentUserRole())) {
    state.center.moderation.queueLoaded = false;
    state.center.moderation.queue = [];
    return;
  }
  if (state.center.moderation.queueLoading) return;
  if (state.center.moderation.queueLoaded && !force) return;
  state.center.moderation.queueLoading = true;
  try {
    const items = await apiFetch("/api/moderation/queue");
    state.center.moderation.queue = Array.isArray(items) ? items : [];
    state.center.moderation.queueLoaded = true;
  } finally {
    state.center.moderation.queueLoading = false;
  }
}

async function loadModerationReports(force = false) {
  if (!state.user || !isStaffRole(getCurrentUserRole())) {
    state.center.moderation.reportsLoaded = false;
    state.center.moderation.reports = [];
    return;
  }
  if (state.center.moderation.reportsLoading) return;
  if (state.center.moderation.reportsLoaded && !force) return;
  state.center.moderation.reportsLoading = true;
  try {
    const items = await apiFetch("/api/moderation/reports?status=pending");
    state.center.moderation.reports = Array.isArray(items) ? items : [];
    state.center.moderation.reportsLoaded = true;
  } finally {
    state.center.moderation.reportsLoading = false;
  }
}

async function loadModerationHistory(force = false) {
  if (!state.user || !isStaffRole(getCurrentUserRole())) {
    state.center.moderation.historyLoaded = false;
    state.center.moderation.history = [];
    return;
  }
  if (state.center.moderation.historyLoading) return;
  if (state.center.moderation.historyLoaded && !force) return;
  state.center.moderation.historyLoading = true;
  try {
    const items = await apiFetch("/api/moderation/history");
    state.center.moderation.history = Array.isArray(items) ? items : [];
    state.center.moderation.historyLoaded = true;
  } finally {
    state.center.moderation.historyLoading = false;
  }
}

async function loadManagementRequests(force = false) {
  if (!state.user || !isAdminRole(getCurrentUserRole())) {
    state.center.management.requestsLoaded = false;
    state.center.management.requests = [];
    return;
  }
  if (state.center.management.requestsLoading) return;
  if (state.center.management.requestsLoaded && !force) return;
  state.center.management.requestsLoading = true;
  try {
    const items = await apiFetch("/api/moderation/requests");
    state.center.management.requests = Array.isArray(items) ? items : [];
    state.center.management.requestsLoaded = true;
  } finally {
    state.center.management.requestsLoading = false;
  }
}

async function loadManagementCategories(force = false) {
  if (!state.user || !isAdminRole(getCurrentUserRole())) {
    state.center.management.categoriesLoaded = false;
    state.center.management.categories = [];
    return;
  }
  if (state.center.management.categoriesLoading) return;
  if (state.center.management.categoriesLoaded && !force) return;
  state.center.management.categoriesLoading = true;
  try {
    const items = await apiFetch("/api/moderation/categories");
    state.center.management.categories = Array.isArray(items) ? items : [];
    state.center.management.categoriesLoaded = true;
    state.categories = state.center.management.categories;
  } finally {
    state.center.management.categoriesLoading = false;
  }
}

async function loadManagementJournal(force = false) {
  if (!state.user || !isAdminRole(getCurrentUserRole())) {
    state.center.management.journalLoaded = false;
    state.center.management.journal = [];
    return;
  }
  if (state.center.management.journalLoading) return;
  if (state.center.management.journalLoaded && !force) return;
  state.center.management.journalLoading = true;
  try {
    const items = await apiFetch("/api/moderation/history");
    state.center.management.journal = Array.isArray(items) ? items : [];
    state.center.management.journalLoaded = true;
  } finally {
    state.center.management.journalLoading = false;
  }
}

function upsertNotificationItem(item, { prepend = false } = {}) {
  const normalizedItem = normalizeNotificationItem(item);
  if (!normalizedItem) return;

  const bucketState = getNotificationBucketState(normalizedItem.bucket);
  const existingIndex = bucketState.items.findIndex((entry) => normalizeUserID(entry && entry.id) === normalizedItem.id);
  if (existingIndex >= 0) {
    bucketState.items[existingIndex] = normalizedItem;
  } else {
    bucketState.items = prepend ? [normalizedItem, ...bucketState.items] : [...bucketState.items, normalizedItem];
  }

  const allState = getNotificationBucketState("all");
  const allIndex = allState.items.findIndex((entry) => normalizeUserID(entry && entry.id) === normalizedItem.id);
  if (allIndex >= 0) {
    allState.items[allIndex] = normalizedItem;
  } else {
    allState.items = prepend ? [normalizedItem, ...allState.items] : [...allState.items, normalizedItem];
  }
}

function replaceNotificationItem(item) {
  const normalizedItem = normalizeNotificationItem(item);
  if (!normalizedItem) return;
  const bucketState = getNotificationBucketState(normalizedItem.bucket);
  const existingIndex = bucketState.items.findIndex((entry) => normalizeUserID(entry && entry.id) === normalizedItem.id);
  if (existingIndex >= 0) {
    bucketState.items[existingIndex] = normalizedItem;
  }
  const allState = getNotificationBucketState("all");
  const allIndex = allState.items.findIndex((entry) => normalizeUserID(entry && entry.id) === normalizedItem.id);
  if (allIndex >= 0) {
    allState.items[allIndex] = normalizedItem;
  }
}

function removeNotificationItem(notificationID) {
  const id = normalizeUserID(notificationID);
  if (!id) return;
  CENTER_NOTIFICATION_BUCKETS.forEach((bucket) => {
    const bucketState = getNotificationBucketState(bucket);
    bucketState.items = bucketState.items.filter((item) => normalizeUserID(item && item.id) !== id);
  });
}

function isMessageForPeer(message, peerID) {
  const me = getCurrentUserID();
  const peer = normalizeUserID(peerID);
  if (!me || !peer || !message) return false;

  const fromID = normalizeUserID(message.from && message.from.id);
  const toID = normalizeUserID(message.to && message.to.id);
  return (fromID === me && toID === peer) || (fromID === peer && toID === me);
}

function appendDMMessage(message) {
  const nextMessage = normalizeDMMessage(message);
  if (!nextMessage) return;
  if (state.dmMessages.some((entry) => normalizeUserID(entry && entry.id) === nextMessage.id)) {
    return;
  }
  state.dmMessages = [...state.dmMessages, nextMessage];
}

function getLatestDMMessageID(messages = state.dmMessages) {
  if (!Array.isArray(messages) || messages.length === 0) return 0;
  const latestMessage = messages[messages.length - 1];
  const id = Number.parseInt(normalizeUserID(latestMessage && latestMessage.id), 10);
  return Number.isFinite(id) && id >= 0 ? id : 0;
}

async function markDMConversationRead(peerID, lastReadMessageID) {
  const peer = normalizeUserID(peerID);
  const lastReadID = Number.parseInt(String(lastReadMessageID ?? "").trim(), 10);
  if (!state.user || !peer || peer === getCurrentUserID() || !Number.isFinite(lastReadID) || lastReadID < 0) {
    return;
  }

  setDMPeerUnreadCount(peer, 0);
  if (lastReadID === 0) {
    return;
  }

  await apiFetch(`/api/dm/${peer}/read`, {
    method: "POST",
    body: JSON.stringify({ lastReadMessageId: lastReadID }),
  });
  void ensureNotificationSummary(true);
}

async function loadDMConversation(peerID, limit = DM_HISTORY_PAGE_SIZE) {
  const peer = normalizeUserID(peerID);
  if (!state.user || !peer || peer === getCurrentUserID()) return;

  state.dmLoading = true;
  state.dmLoadingOlder = false;
  state.dmHasMore = false;
  state.dmOlderCursor = null;
  state.dmOlderLoadAt = 0;
  syncDMView();

  try {
    const messages = (await apiFetch(`/api/dm/${peer}?limit=${limit}`)) || [];
    state.dmMessages = Array.isArray(messages) ? messages.map(normalizeDMMessage).filter(Boolean) : [];
    state.dmHasMore = state.dmMessages.length >= limit;
    state.dmOlderCursor = getDMConversationCursor(state.dmMessages);
  } finally {
    state.dmLoading = false;
    syncDMView({ scrollMode: "bottom" });
  }
}

async function loadOlderDMConversation(limit = DM_HISTORY_PAGE_SIZE) {
  const peer = normalizeUserID(state.dmPeerID);
  const cursor = state.dmOlderCursor || getDMConversationCursor(state.dmMessages);
  if (!state.user || !peer || peer === getCurrentUserID() || !cursor) return;
  if (state.dmLoading || state.dmLoadingOlder || !state.dmHasMore) return;

  const thread = document.getElementById("dm-thread");
  const prevScrollTop = thread ? thread.scrollTop : 0;
  const prevScrollHeight = thread ? thread.scrollHeight : 0;

  state.dmLoadingOlder = true;
  syncDMView({ scrollMode: "preserve" });

  try {
    const params = new URLSearchParams({
      limit: String(limit),
      beforeTs: String(cursor.ts),
      beforeID: String(cursor.id),
    });
    const messages = (await apiFetch(`/api/dm/${peer}?${params.toString()}`)) || [];
    if (normalizeUserID(state.dmPeerID) !== peer) {
      state.dmLoadingOlder = false;
      return;
    }

    const olderMessages = Array.isArray(messages) ? messages.map(normalizeDMMessage).filter(Boolean) : [];
    const knownIDs = new Set((state.dmMessages || []).map((message) => normalizeUserID(message && message.id)));
    const uniqueOlderMessages = olderMessages.filter((message) => !knownIDs.has(normalizeUserID(message && message.id)));

    if (uniqueOlderMessages.length > 0) {
      state.dmMessages = [...uniqueOlderMessages, ...state.dmMessages];
    }
    state.dmHasMore = olderMessages.length >= limit;
    state.dmOlderCursor = getDMConversationCursor(state.dmMessages);
    state.dmLoadingOlder = false;
    syncDMView({ scrollMode: "prepend", prevScrollTop, prevScrollHeight });
  } catch (err) {
    state.dmLoadingOlder = false;
    syncDMView({ scrollMode: "preserve", prevScrollTop, prevScrollHeight });
    throw err;
  }
}

function openDM(peerID) {
  const peer = normalizeUserID(peerID);
  if (!state.user || !peer || peer === getCurrentUserID()) return;

  setDMPeerUnreadCount(peer, 0);
  if (!isDMRoute()) {
    state.dmReturnPath = `${location.pathname || "/"}${location.search || ""}`;
  }
  syncPresencePanel();
  syncDMPeersPanel();
  navigate(`/dm/${peer}`);
}

function openNotifications() {
  if (!state.user) {
    alert("Login to open your center.");
    return;
  }
  navigate(getCenterTabPath("notifications", getPreferredNotificationBucket()));
}

function closeDMConversation() {
  const target = state.dmReturnPath || "/dm";
  clearDMConversationState(true);
  syncPresencePanel();
  navigate(target);
}

async function togglePostSubscription(postID, subscribed) {
  const id = normalizeUserID(postID);
  if (!id) return;
  if (subscribed) {
    await apiFetch(`/api/posts/${id}/subscription`, { method: "DELETE" });
    return;
  }
  await apiFetch(`/api/posts/${id}/subscription`, { method: "POST", body: "{}" });
}

async function toggleAuthorFollow(username, following) {
  const normalizedUsername = normalizeUsername(username);
  if (!normalizedUsername) return;
  if (following) {
    await apiFetch(`/api/u/${encodeURIComponent(normalizedUsername)}/follow`, { method: "DELETE" });
    return;
  }
  await apiFetch(`/api/u/${encodeURIComponent(normalizedUsername)}/follow`, { method: "POST", body: "{}" });
}

async function deletePostByID(postID) {
  const id = normalizeUserID(postID);
  if (!id) return;
  await apiFetch(`/api/posts/${id}`, { method: "DELETE" });
}

async function loadPublicProfile(username) {
  return apiFetch(`/api/u/${encodeURIComponent(normalizeUsername(username))}`);
}

function sendDMMessage(body, attachment) {
  const peerID = normalizeUserID(state.dmPeerID);
  if (!peerID) {
    throw new Error("Select a user first");
  }
  if (!realtimeSocket || realtimeSocket.readyState !== WebSocket.OPEN) {
    throw new Error("Realtime connection is not ready");
  }

  realtimeSocket.send(
    JSON.stringify({
      type: "pm:send",
      to: { id: peerID },
      body: String(body || ""),
      ...(getAttachmentNumericID(attachment) ? { attachmentId: normalizeUserID(attachment && attachment.id) } : {}),
    })
  );
}

function refreshCenterRouteIfOpen() {
  if (location.pathname !== "/center") return;
  if (!state.user) return;
  app.innerHTML = renderLayout({
    mode: "center",
    hideHeading: true,
    content: renderCenterContent(getCenterTab(), getCenterSubtab()),
  });
  bindHeaderActions();
  bindCenterActions();
}

function handleRealtimeNotificationNew(payload) {
  if (payload && payload.notification) {
    upsertNotificationItem(payload.notification, { prepend: true });
  }
  if (payload && payload.summary) {
    setNotificationSummary(payload.summary);
  }
  refreshCenterRouteIfOpen();
}

function handleRealtimeNotificationUpdate(payload) {
  if (payload && payload.notification) {
    replaceNotificationItem(payload.notification);
  }
  if (payload && payload.summary) {
    setNotificationSummary(payload.summary);
  }
  refreshCenterRouteIfOpen();
}

function handleRealtimeNotificationSummary(payload) {
  if (payload && payload.summary) {
    setNotificationSummary(payload.summary);
  }
  refreshCenterRouteIfOpen();
}

function handleRealtimeMessage(payload) {
  if (!payload || typeof payload !== "object") return;

  if (payload.type === "hello") {
    return;
  }

  if (payload.type === "presence:init") {
    const nextOnline = new Set();
    const users = Array.isArray(payload.users) ? payload.users : [];
    users.forEach((user) => {
      const id = normalizeUserID(user && user.id);
      if (id) nextOnline.add(id);
    });
    state.onlineUserIDs = nextOnline;
    syncPresencePanel();
    syncDMPeersPanel();
    return;
  }

  if (payload.type === "presence:update") {
    const userID = normalizeUserID(payload.user && payload.user.id);
    if (!userID) return;

    if (payload.status === "online") {
      state.onlineUserIDs.add(userID);
    } else if (payload.status === "offline") {
      state.onlineUserIDs.delete(userID);
    } else {
      return;
    }

    syncPresencePanel();
    syncDMPeersPanel();
    return;
  }

  if (payload.type === "typing:update") {
    handleTypingUpdate(payload);
    return;
  }

  if (payload.type === "notification:new") {
    handleRealtimeNotificationNew(payload);
    return;
  }

  if (payload.type === "notification:update") {
    handleRealtimeNotificationUpdate(payload);
    return;
  }

  if (payload.type === "notification:summary") {
    handleRealtimeNotificationSummary(payload);
    return;
  }

  if (payload.type === "pm:new") {
    const message = normalizeDMMessage(payload.message);
    if (!message) return;
    updateDMPeerActivity(message);

    if (isMessageForPeer(message, state.dmPeerID)) {
      const thread = document.getElementById("dm-thread");
      const shouldStickBottom = isDMThreadNearBottom(thread);
      const fromUserID = normalizeUserID(message.from && message.from.id);
      appendDMMessage(message);
      if (normalizeUserID(message.from && message.from.id) === getCurrentUserID()) {
        state.dmDraftAttachment = null;
      } else if (fromUserID) {
        removeRemoteTypingEntry(TYPING_SCOPE_DM, fromUserID, fromUserID, 0);
      }
      void markDMConversationRead(state.dmPeerID, message.id).catch((err) => {
        debugWSWarn("dm read sync failed", err);
      });
      syncDMView({ scrollMode: shouldStickBottom ? "bottom" : "preserve" });
      syncPresencePanel();
      syncDMPeersPanel();
      return;
    }

    if (normalizeUserID(message.to && message.to.id) === getCurrentUserID()) {
      const peerID = normalizeUserID(message.from && message.from.id);
      if (peerID && peerID !== getCurrentUserID()) {
        setDMPeerUnreadCount(peerID, Number(state.dmUnreadByPeer[peerID] || 0) + 1);
        syncPresencePanel();
        syncDMPeersPanel();
      }
    } else {
      debugWS("ws message", payload);
    }
    return;
  }

  debugWS("ws message", payload);
}

function updateDMPeerActivity(message) {
  const me = getCurrentUserID();
  if (!me || !message) return;

  const fromID = normalizeUserID(message.from && message.from.id);
  const toID = normalizeUserID(message.to && message.to.id);
  const peerID = fromID === me ? toID : toID === me ? fromID : "";
  if (!peerID || peerID === me) return;

  const created = new Date(message.createdAt);
  const nextLastMessageAt = Number.isFinite(created.getTime()) ? Math.floor(created.getTime() / 1000) : 0;
  if (nextLastMessageAt <= 0) return;

  let changed = false;
  state.dmPeers = sortDMPeers(
    (state.dmPeers || []).map((peer) => {
      if (normalizeUserID(peer && peer.id) !== peerID) return peer;
      changed = true;
      return {
        ...peer,
        lastMessageAt: nextLastMessageAt,
        lastMessageID: normalizeUserID(message.id),
        lastMessageFromUserID: fromID,
        lastMessagePreview: String(message.body || "").trim(),
        lastMessageHasAttachment: Boolean(message.attachment),
      };
    })
  );

  if (changed) {
    syncDMPeersPanel();
    return;
  }

  if (state.user && state.dmPeersLoaded) {
    void loadDMPeers(true).then(() => refreshCenterRouteIfOpen());
  }
}

async function ensureCategories() {
  if (state.categories.length > 0) return;
  state.categories = (await apiFetch("/api/categories")) || [];
}

async function loadPosts() {
  const params = new URLSearchParams();
  state.filters.cat.forEach((id) => params.append("cat", id));
  if (state.filters.mine) params.set("mine", "1");
  if (state.filters.liked) params.set("liked", "1");
  const q = String(state.filters.q || "").trim();
  if (q) params.set("q", q);
  const qs = params.toString();
  return apiFetch(qs ? `/api/posts?${qs}` : "/api/posts");
}

function getCenterTab() {
  const tab = String(getSearchParam("tab") || "").trim().toLowerCase();
  if (tab === "moderation" && isStaffRole(getCurrentUserRole())) return "moderation";
  if (tab === "management" && isAdminRole(getCurrentUserRole())) return "management";
  if (tab === "activity") return "activity";
  return "notifications";
}

function centerSubtabsForTab(tab) {
  switch (tab) {
    case "notifications":
      return CENTER_NOTIFICATION_BUCKETS;
    case "moderation":
      return isStaffRole(getCurrentUserRole()) ? CENTER_MODERATION_TABS : [];
    case "management":
      return isAdminRole(getCurrentUserRole()) ? CENTER_MANAGEMENT_TABS : [];
    default:
      return [];
  }
}

function getCenterSubtab(tab = getCenterTab()) {
  const subtab = String(getSearchParam("subtab") || "").trim().toLowerCase();
  const subtabs = centerSubtabsForTab(tab);
  if (subtabs.includes(subtab)) return subtab;
  return subtabs[0] || "";
}

function getCenterTabPath(tab, subtab = getCenterSubtab(tab)) {
  const params = new URLSearchParams();
  params.set("tab", tab || "notifications");
  if (subtab) params.set("subtab", subtab);
  return `/center?${params.toString()}`;
}

function getPreferredNotificationBucket() {
  if (getNotificationBucketUnreadCount("deleted") > 0) return "deleted";
  if (getNotificationBucketUnreadCount("reports") > 0) return "reports";
  if (getNotificationBucketUnreadCount("appeals") > 0) return "appeals";
  return "all";
}

function renderCenterTabs(activeTab) {
  const tabs = [{ key: "notifications", label: "Notifications" }];
  if (isStaffRole(getCurrentUserRole())) tabs.push({ key: "moderation", label: "Moderation" });
  if (isAdminRole(getCurrentUserRole())) tabs.push({ key: "management", label: "Management" });
  tabs.push({ key: "activity", label: "Activity" });

  return `
    <div class="center-tabs" role="tablist" aria-label="Center tabs">
      ${tabs
        .map(
          (tab) => `
            <a class="center-tab ${activeTab === tab.key ? "is-active" : ""}" data-link href="${escapeHTML(getCenterTabPath(tab.key))}" role="tab" aria-selected="${activeTab === tab.key ? "true" : "false"}">${escapeHTML(tab.label)}</a>
          `
        )
        .join("")}
    </div>
  `;
}

function getNotificationBucketLabel(bucket) {
  switch (bucket) {
    case "all":
      return "All";
    case "deleted":
      return "Deleted";
    case "reports":
      return "My reports";
    case "appeals":
      return "Appeals";
    default:
      return "Notifications";
  }
}

function getCenterSubtabLabel(tab, subtab) {
  if (tab === "notifications") return getNotificationBucketLabel(subtab);
  switch (subtab) {
    case "queue":
      return "Under review";
    case "reports":
      return "Reports";
    case "history":
      return "History";
    case "requests":
      return "Requests";
    case "roles":
      return "Roles";
    case "categories":
      return "Categories";
    case "journal":
      return "Journal";
    default:
      return subtab || tab;
  }
}

function renderCenterSubtabs(activeTab, activeSubtab) {
  const subtabs = centerSubtabsForTab(activeTab);
  if (!subtabs.length) return "";
  return `
    <div class="center-subtabs" role="tablist" aria-label="Center subtabs">
      ${subtabs.map((subtab) => {
        const unread = activeTab === "notifications" ? getNotificationBucketUnreadCount(subtab) : 0;
        return `
          <a
            class="center-subtab ${activeSubtab === subtab ? "is-active" : ""}"
            data-link
            href="${escapeHTML(getCenterTabPath(activeTab, subtab))}"
            role="tab"
            aria-selected="${activeSubtab === subtab ? "true" : "false"}"
          >
            <span>${escapeHTML(getCenterSubtabLabel(activeTab, subtab))}</span>
            ${unread > 0 ? `<span class="center-badge">${escapeHTML(String(unread))}</span>` : ""}
          </a>
        `;
      }).join("")}
    </div>
  `;
}

function renderCenterNotificationItem(item) {
  if (!item) return "";
  const deletedPreviewMarkup = item.type === "content_deleted" ? renderDeletedNotificationPreview(item) : "";
  const meta = item.context || (item.entityAvailable ? "" : "content is no longer available");
  const metaMarkup = deletedPreviewMarkup || (meta
    ? item.linkPath && item.entityAvailable
      ? `<a class="center-item-body center-item-context-link" data-link href="${escapeHTML(item.linkPath)}">${escapeHTML(meta)}</a>`
      : `<div class="center-item-body">${escapeHTML(meta)}</div>`
    : "");
  return `
    <article class="center-item center-notification-item${item.isRead ? " is-read" : " is-unread"}">
      <div class="center-item-main">
        <div class="center-item-head">
          <strong>${escapeHTML(item.text)}</strong>
          <span class="center-item-meta">${escapeHTML(formatDate(item.createdAt))}</span>
        </div>
        ${metaMarkup}
        <div class="center-item-actions">
          ${item.isRead ? `<span class="center-status-label">Seen</span>` : `<button class="btn btn-ghost btn-compact" type="button" data-action="notification-read" data-notification-id="${escapeHTML(item.id)}">Mark as read</button>`}
          ${item.type === "content_deleted" && item.canAppeal && item.entityType && item.entityID ? `<button class="btn btn-primary btn-compact" type="button" data-action="open-appeal-modal" data-target-type="${escapeHTML(item.entityType)}" data-target-id="${escapeHTML(item.entityID)}">Appeal</button>` : ""}
          <button class="btn btn-ghost btn-compact" type="button" data-action="notification-delete" data-notification-id="${escapeHTML(item.id)}">Delete notification</button>
        </div>
      </div>
    </article>
  `;
}

function renderDeletedNotificationPreview(item) {
  if (!item || item.type !== "content_deleted") return "";
  const postTitle = truncateInline(item.postTitle, 80);
  const postPreview = truncateInline(item.postPreview, 160);
  const commentPreview = truncateInline(item.commentPreview, 160);
  const bodyPreview = commentPreview || postPreview;
  if (!postTitle && !bodyPreview) return "";

  let titleMarkup = "";
  if (postTitle) {
    titleMarkup = item.linkPath && item.entityAvailable
      ? `<a class="center-item-link" data-link href="${escapeHTML(item.linkPath)}">${escapeHTML(postTitle)}</a>`
      : `<strong>${escapeHTML(postTitle)}</strong>`;
  } else if (item.entityType === "comment") {
    titleMarkup = item.linkPath && item.entityAvailable
      ? `<a class="center-item-link" data-link href="${escapeHTML(item.linkPath)}">Comment</a>`
      : "<strong>Comment</strong>";
  } else {
    titleMarkup = item.linkPath && item.entityAvailable
      ? `<a class="center-item-link" data-link href="${escapeHTML(item.linkPath)}">Post</a>`
      : "<strong>Post</strong>";
  }

  return `
    <div class="center-target-preview">
      ${titleMarkup}
      ${bodyPreview ? `<div class="center-item-body">${escapeHTML(bodyPreview)}</div>` : ""}
    </div>
  `;
}

function moderationTargetLink(target) {
  if (!target || typeof target !== "object") return "";
  const postID = normalizeUserID(target.postId ?? target.post_id ?? "");
  const commentID = normalizeUserID(target.commentId ?? target.comment_id ?? "");
  if (postID && commentID) return `/post/${postID}#comment-${commentID}`;
  if (postID) return `/post/${postID}`;
  if (target.targetType === "post" && target.targetId) return `/post/${target.targetId}`;
  return "";
  const attachmentLabel = peer.lastMessageHasAttachment ? (body ? " • attachment" : "Attachment") : "";
  if (!body) return attachmentLabel;
  const prefix = normalizeUserID(peer.lastMessageFromUserID) === getCurrentUserID() ? "You: " : "";
  return `${prefix}${body}${attachmentLabel}`;
}

function renderModerationTargetPreview(target, { titleLimit = 80, bodyLimit = 160 } = {}) {
  if (!target || typeof target !== "object") return `<div class="center-item-body">No target snapshot.</div>`;
  const title = truncateInline(target.postTitle, titleLimit);
  const body = truncateInline(target.commentBody || target.postBody, bodyLimit);
  const linkPath = moderationTargetLink(target);
  return `
    <div class="center-target-preview">
      ${title ? (linkPath ? `<a class="center-item-link" data-link href="${escapeHTML(linkPath)}">${escapeHTML(title)}</a>` : `<strong>${escapeHTML(title)}</strong>`) : ""}
      ${body ? `<div class="center-item-body">${escapeHTML(body)}</div>` : ""}
    </div>
  `;
}

function renderCenterStatusPill(value) {
  const label = String(value || "").trim() || "pending";
  return `<span class="center-status-label center-status-pill">${escapeHTML(label.replace(/_/g, " "))}</span>`;
}

function renderHistoryDetails(item) {
  const previewNote = truncateInline(item.note, 120);
  const previewTitle = truncateInline(item.postTitleSnapshot, 80);
  const previewPostBody = truncateInline(item.postBodySnapshot, 160);
  const previewCommentBody = truncateInline(item.commentBodySnapshot, 160);
  if (![item.note, item.postTitleSnapshot, item.postBodySnapshot, item.commentBodySnapshot].some(Boolean)) return "";
  return `
    <details class="center-inline-details">
      <summary>Expand details</summary>
      ${item.postTitleSnapshot ? `<div><strong>Post title:</strong> ${escapeHTML(previewTitle)}</div>` : ""}
      ${item.postBodySnapshot ? `<div><strong>Post body:</strong> ${escapeHTML(previewPostBody)}</div>` : ""}
      ${item.commentBodySnapshot ? `<div><strong>Comment body:</strong> ${escapeHTML(previewCommentBody)}</div>` : ""}
      ${item.note ? `<div><strong>Note:</strong> ${escapeHTML(previewNote)}</div>` : ""}
    </details>
  `;
}

function renderReportCard(report, { mine = false, staff = false } = {}) {
  if (!report) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>${escapeHTML(String(report.reason || "report").replace(/_/g, " "))}</strong>
        <span class="center-item-meta">${escapeHTML(formatDate(report.createdAt))}</span>
      </div>
      <div class="center-inline-meta">
        ${renderCenterStatusPill(report.status)}
        <span>Target: ${escapeHTML(report.target && report.target.targetType ? report.target.targetType : "content")}</span>
        ${mine ? "" : `<span>Reporter: ${escapeHTML(getDisplayNameOrUsername(report.reporter || {}))}</span>`}
      </div>
      ${renderModerationTargetPreview(report.target)}
      ${report.note ? `<div class="center-item-body">${escapeHTML(truncateInline(report.note, 160))}</div>` : ""}
      <div class="center-item-actions">
        ${report.target && moderationTargetLink(report.target) ? `<a class="btn btn-ghost btn-compact" data-link href="${escapeHTML(moderationTargetLink(report.target))}">Open content</a>` : ""}
        ${staff && String(report.status) === "pending" ? `<button class="btn btn-primary btn-compact" type="button" data-action="staff-report-action" data-report-id="${escapeHTML(String(report.id))}" data-report-mode="action">Action taken</button>` : ""}
        ${staff && String(report.status) === "pending" ? `<button class="btn btn-ghost btn-compact" type="button" data-action="staff-report-action" data-report-id="${escapeHTML(String(report.id))}" data-report-mode="dismiss">Dismiss</button>` : ""}
      </div>
    </article>
  `;
}

function renderAppealCard(appeal, { reviewer = false } = {}) {
  if (!appeal) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>Appeal to ${escapeHTML(humanizeRole(appeal.targetRole))}</strong>
        <span class="center-item-meta">${escapeHTML(formatDate(appeal.createdAt))}</span>
      </div>
      <div class="center-inline-meta">
        ${renderCenterStatusPill(appeal.status)}
        <span>Requester: ${escapeHTML(getDisplayNameOrUsername(appeal.requester || {}))}</span>
      </div>
      ${renderModerationTargetPreview(appeal.target)}
      ${appeal.note ? `<div class="center-item-body">${escapeHTML(truncateInline(appeal.note, 160))}</div>` : ""}
      <div class="center-item-actions">
        ${appeal.target && moderationTargetLink(appeal.target) ? `<a class="btn btn-ghost btn-compact" data-link href="${escapeHTML(moderationTargetLink(appeal.target))}">Open content</a>` : ""}
        ${reviewer && String(appeal.status) === "pending" ? `<button class="btn btn-primary btn-compact" type="button" data-action="staff-appeal-action" data-appeal-id="${escapeHTML(String(appeal.id))}" data-appeal-mode="reverse">Reverse decision</button>` : ""}
        ${reviewer && String(appeal.status) === "pending" ? `<button class="btn btn-ghost btn-compact" type="button" data-action="staff-appeal-action" data-appeal-id="${escapeHTML(String(appeal.id))}" data-appeal-mode="uphold">Uphold decision</button>` : ""}
      </div>
    </article>
  `;
}

function renderCenterDMPeerItem(peer) {
  if (!peer) return "";
  const label = getDMPeerLabel(peer);
  const preview = formatCenterDMPeerPreview(peer) || "Open conversation";
  const unreadCount = Math.max(0, Number(peer.unreadCount || 0));
  return `
    <article class="center-item center-notification-item${unreadCount > 0 ? " is-unread" : " is-read"}">
      <div class="center-item-main">
        <div class="center-item-head">
          <a class="center-item-link" data-link href="/dm/${escapeHTML(peer.id)}">${escapeHTML(label)}</a>
          <span class="center-item-meta">${peer.lastMessageAt > 0 ? escapeHTML(formatDate(new Date(peer.lastMessageAt * 1000).toISOString())) : ""}</span>
        </div>
        <a class="center-item-body center-item-context-link" data-link href="/dm/${escapeHTML(peer.id)}">${escapeHTML(preview)}</a>
        <div class="center-item-actions">
          ${
            unreadCount > 0
              ? `
                <span class="center-status-label">${escapeHTML(`${unreadCount} unread`)}</span>
                ${peer.lastMessageID ? `<button class="btn btn-ghost btn-compact" type="button" data-action="dm-center-read" data-dm-peer-id="${escapeHTML(peer.id)}" data-dm-last-message-id="${escapeHTML(peer.lastMessageID)}">Mark as read</button>` : ""}
              `
              : `<span class="center-status-label" aria-label="Status: seen">Status: seen</span>`
          }
        </div>
      </div>
    </article>
  `;
}

function moderationTargetLink(target) {
  if (!target || typeof target !== "object") return "";
  const postID = normalizeUserID(target.postId ?? target.post_id ?? "");
  const commentID = normalizeUserID(target.commentId ?? target.comment_id ?? "");
  if (postID && commentID) return `/post/${postID}#comment-${commentID}`;
  if (postID) return `/post/${postID}`;
  if (target.targetType === "post" && target.targetId) return `/post/${target.targetId}`;
  return "";
}

function formatCenterDMPeerPreview(peer) {
  const body = String(peer && peer.lastMessageBody ? peer.lastMessageBody : "").trim();
  const attachmentLabel = peer && peer.lastMessageHasAttachment ? (body ? " • attachment" : "Attachment") : "";
  if (!body) return attachmentLabel;
  const prefix = normalizeUserID(peer.lastMessageFromUserID) === getCurrentUserID() ? "You: " : "";
  return `${prefix}${body}${attachmentLabel}`;
}

function renderHistoryDetails(item) {
  const previewNote = truncateInline(item.note, 120);
  const previewTitle = truncateInline(item.postTitleSnapshot, 80);
  const previewPostBody = truncateInline(item.postBodySnapshot, 160);
  const previewCommentBody = truncateInline(item.commentBodySnapshot, 160);
  if (![item.note, item.postTitleSnapshot, item.postBodySnapshot, item.commentBodySnapshot].some(Boolean)) return "";
  return `
    <details class="center-inline-details">
      <summary>${escapeHTML([previewTitle, previewPostBody, previewCommentBody, previewNote].filter(Boolean)[0] || "Expand details")}</summary>
      ${item.postTitleSnapshot ? `<div><strong>Post title:</strong> ${escapeHTML(item.postTitleSnapshot)}</div>` : ""}
      ${item.postBodySnapshot ? `<div><strong>Post body:</strong> ${escapeHTML(item.postBodySnapshot)}</div>` : ""}
      ${item.commentBodySnapshot ? `<div><strong>Comment body:</strong> ${escapeHTML(item.commentBodySnapshot)}</div>` : ""}
      ${item.note ? `<div><strong>Note:</strong> ${escapeHTML(item.note)}</div>` : ""}
    </details>
  `;
}

function renderReportCard(report, { mine = false, staff = false } = {}) {
  if (!report) return "";
  const targetLink = moderationTargetLink(report.target);
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>${escapeHTML(String(report.reason || "report").replace(/_/g, " "))}</strong>
        <span class="center-item-meta">${escapeHTML(formatDate(report.createdAt))}</span>
      </div>
      <div class="center-inline-meta">
        ${renderCenterStatusPill(report.status)}
        <span>Target: ${escapeHTML(report.target && report.target.targetType ? report.target.targetType : "content")}</span>
        ${mine ? "" : `<span>Reporter: ${escapeHTML(getDisplayNameOrUsername(report.reporter || {}))}</span>`}
      </div>
      ${renderModerationTargetPreview(report.target)}
      ${report.note ? `<div class="center-item-body">${escapeHTML(truncateInline(report.note, 160))}</div>` : ""}
      ${report.decisionNote ? `<div class="center-item-body">Decision: ${escapeHTML(truncateInline(report.decisionNote, 160))}</div>` : ""}
      <div class="center-item-actions">
        ${targetLink ? `<a class="btn btn-ghost btn-compact" data-link href="${escapeHTML(targetLink)}">Open content</a>` : ""}
        ${staff && String(report.status) === "pending" ? `<button class="btn btn-primary btn-compact" type="button" data-action="staff-report-action" data-report-id="${escapeHTML(String(report.id))}" data-report-mode="action">Action taken</button>` : ""}
        ${staff && String(report.status) === "pending" ? `<button class="btn btn-ghost btn-compact" type="button" data-action="staff-report-action" data-report-id="${escapeHTML(String(report.id))}" data-report-mode="dismiss">Dismiss</button>` : ""}
      </div>
    </article>
  `;
}

function renderAppealCard(appeal, { reviewer = false } = {}) {
  if (!appeal) return "";
  const targetLink = moderationTargetLink(appeal.target);
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>Appeal to ${escapeHTML(humanizeRole(appeal.targetRole))}</strong>
        <span class="center-item-meta">${escapeHTML(formatDate(appeal.createdAt))}</span>
      </div>
      <div class="center-inline-meta">
        ${renderCenterStatusPill(appeal.status)}
        <span>Requester: ${escapeHTML(getDisplayNameOrUsername(appeal.requester || {}))}</span>
        ${reviewer ? "" : `<span>Review level: ${escapeHTML(humanizeRole(appeal.targetRole))}</span>`}
      </div>
      ${renderModerationTargetPreview(appeal.target)}
      ${appeal.note ? `<div class="center-item-body">${escapeHTML(truncateInline(appeal.note, 160))}</div>` : ""}
      ${appeal.decisionNote ? `<div class="center-item-body">Decision: ${escapeHTML(truncateInline(appeal.decisionNote, 160))}</div>` : ""}
      <div class="center-item-actions">
        ${targetLink ? `<a class="btn btn-ghost btn-compact" data-link href="${escapeHTML(targetLink)}">Open content</a>` : ""}
        ${reviewer && String(appeal.status) === "pending" ? `<button class="btn btn-primary btn-compact" type="button" data-action="staff-appeal-action" data-appeal-id="${escapeHTML(String(appeal.id))}" data-appeal-mode="reverse">Reverse decision</button>` : ""}
        ${reviewer && String(appeal.status) === "pending" ? `<button class="btn btn-ghost btn-compact" type="button" data-action="staff-appeal-action" data-appeal-id="${escapeHTML(String(appeal.id))}" data-appeal-mode="uphold">Uphold decision</button>` : ""}
      </div>
    </article>
  `;
}

function renderCenterNotificationPanel(activeBucket) {
  if (activeBucket === "reports") {
    const items = Array.isArray(state.center.myReports) ? state.center.myReports : [];
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>My reports</h2>
            <p>Your submitted reports and their outcomes.</p>
          </div>
        </div>
        <div class="center-list">
          ${items.length ? items.map((item) => renderReportCard(item, { mine: true })).join("") : renderEmpty("No reports", "You have not filed any reports yet.")}
        </div>
      </section>
    `;
  }
  if (activeBucket === "appeals") {
    const items = Array.isArray(state.center.appeals) ? state.center.appeals : [];
    const reviewItems = Array.isArray(state.center.appealInbox) ? state.center.appealInbox : [];
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>Appeals</h2>
            <p>Your appeals on moderation decisions.</p>
          </div>
        </div>
        ${
          isAdminRole(getCurrentUserRole())
            ? `
              <div class="section-row center-panel-head">
                <div>
                  <h3>Incoming appeals</h3>
                  <p>Pending decisions routed to your staff level.</p>
                </div>
              </div>
              <div class="center-list">
                ${reviewItems.length ? reviewItems.map((item) => renderAppealCard(item, { reviewer: true })).join("") : renderEmpty("No incoming appeals", "There are no pending appeals routed to your level.")}
              </div>
            `
            : ""
        }
        <div class="section-row center-panel-head">
          <div>
            <h3>My appeals</h3>
            <p>Appeals you have already submitted.</p>
          </div>
        </div>
        <div class="center-list">
          ${items.length ? items.map((item) => renderAppealCard(item)).join("") : renderEmpty("No appeals", "You have not filed any appeals yet.")}
        </div>
      </section>
    `;
  }

  const bucketState = getNotificationBucketState(activeBucket);
  const items = Array.isArray(bucketState.items) ? bucketState.items : [];
  const unread = getNotificationBucketUnreadCount(activeBucket);
  return `
    <section class="surface center-panel">
      <div class="section-row center-panel-head">
        <div>
          <h2>${escapeHTML(getNotificationBucketLabel(activeBucket))}</h2>
          <p>${unread > 0 ? `${unread} unread` : "All caught up"}</p>
        </div>
        <div class="center-panel-actions">
          ${unread > 0 ? `<button class="btn btn-ghost btn-compact" type="button" data-action="notifications-read-all" data-notification-bucket="${escapeHTML(activeBucket)}">Mark all as read</button>` : ""}
        </div>
      </div>
      <div class="center-list">
        ${items.length ? items.map(renderCenterNotificationItem).join("") : renderEmpty("No notifications", "Nothing here yet.")}
      </div>
      ${bucketState.hasMore ? `<div class="center-panel-footer"><button class="btn btn-ghost" type="button" data-action="notifications-load-more" data-notification-bucket="${escapeHTML(activeBucket)}">Load more</button></div>` : ""}
    </section>
  `;
}

function renderActivityPostItem(post) {
  if (!post) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <a class="center-item-link" data-link href="/post/${post.id}">${escapeHTML(post.title || "Untitled post")}</a>
        <span class="center-item-meta">${escapeHTML(formatDate(post.created_at))}</span>
      </div>
      <div class="center-item-body">${escapeHTML(String(post.body || "").trim())}</div>
    </article>
  `;
}

function renderActivityReactionItem(reaction) {
  if (!reaction) return "";
  const actionLabel = Number(reaction.value) > 0 ? "liked" : "disliked";
  const targetLabel = reaction.targetType === "comment" ? "comment" : "post";
  const context = reaction.targetType === "comment" ? reaction.commentPreview || reaction.postTitle : reaction.postTitle || reaction.postPreview;
  return `
    <article class="center-item">
      <div class="center-item-head">
        ${reaction.linkPath ? `<a class="center-item-link" data-link href="${escapeHTML(reaction.linkPath)}">You ${escapeHTML(actionLabel)} a ${escapeHTML(targetLabel)}</a>` : `<strong>You ${escapeHTML(actionLabel)} a ${escapeHTML(targetLabel)}</strong>`}
        <span class="center-item-meta">${escapeHTML(formatDate(reaction.createdAt))}</span>
      </div>
      ${context ? `<div class="center-item-body">${escapeHTML(context)}</div>` : ""}
    </article>
  `;
}

function renderActivityCommentItem(item) {
  if (!item || !item.comment) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        ${item.linkPath ? `<a class="center-item-link" data-link href="${escapeHTML(item.linkPath)}">${escapeHTML(item.postTitle || "Untitled post")}</a>` : `<strong>${escapeHTML(item.postTitle || "Untitled post")}</strong>`}
        <span class="center-item-meta">${escapeHTML(formatDate(item.comment.created_at || item.comment.createdAt))}</span>
      </div>
      <div class="center-item-body">${escapeHTML(String(item.comment.body || "").trim())}</div>
    </article>
  `;
}

function renderActivitySection(title, subtitle, items, renderer, hasMore) {
  return `
    <section class="surface center-panel">
      <div class="section-row center-panel-head">
        <div>
          <h2>${escapeHTML(title)}</h2>
          <p>${escapeHTML(subtitle)}</p>
        </div>
      </div>
      <div class="center-list">
        ${Array.isArray(items) && items.length ? items.map(renderer).join("") : renderEmpty(`No ${title.toLowerCase()} yet`, "Nothing to show right now.")}
      </div>
      ${hasMore ? `<div class="center-panel-footer"><span class="side-note">Showing the latest 20 items.</span></div>` : ""}
    </section>
  `;
}

function renderQueuePostItem(post) {
  if (!post) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <a class="center-item-link" data-link href="/post/${escapeHTML(String(post.id))}">${escapeHTML(post.title || "[deleted]")}</a>
        <span class="center-item-meta">${escapeHTML(formatDate(post.created_at || post.createdAt))}</span>
      </div>
      <div class="center-inline-meta">
        ${renderCenterStatusPill(post.under_review ? "under_review" : "visible")}
        <span>Author: ${escapeHTML(getAuthorDisplay(post))}</span>
        ${renderStaffBadges(getUserBadges(post))}
      </div>
      ${renderPostFlags(post)}
      <div class="center-item-body">${escapeHTML(truncateInline(post.body, 160) || "")}</div>
      <div class="center-item-actions">
        <a class="btn btn-ghost btn-compact" data-link href="/post/${escapeHTML(String(post.id))}">Open</a>
        <button class="btn btn-primary btn-compact" type="button" data-action="queue-approve-post" data-post-id="${escapeHTML(String(post.id))}">Approve</button>
      </div>
    </article>
  `;
}

function renderRoleRequestItem(item) {
  if (!item) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>${escapeHTML(getDisplayNameOrUsername(item.applicant || {}))}</strong>
        <span class="center-item-meta">${escapeHTML(formatDate(item.createdAt))}</span>
      </div>
      <div class="center-inline-meta">
        ${renderCenterStatusPill(item.status)}
        <span>Requested: ${escapeHTML(humanizeRole(item.requestedRole))}</span>
      </div>
      ${item.note ? `<div class="center-item-body">${escapeHTML(truncateInline(item.note, 160))}</div>` : ""}
      <div class="center-item-actions">
        <a class="btn btn-ghost btn-compact" data-link href="${escapeHTML(getProfilePath(item.applicant && item.applicant.username))}">Open profile</a>
        ${String(item.status) === "pending" ? `<button class="btn btn-primary btn-compact" type="button" data-action="review-role-request" data-request-id="${escapeHTML(String(item.id))}" data-request-approve="1">Approve</button>` : ""}
        ${String(item.status) === "pending" ? `<button class="btn btn-ghost btn-compact" type="button" data-action="review-role-request" data-request-id="${escapeHTML(String(item.id))}" data-request-approve="0">Reject</button>` : ""}
      </div>
    </article>
  `;
}

function getAvailableRoleActions(targetRole, targetID) {
  const viewerRole = getCurrentUserRole();
  const target = normalizeRole(targetRole);
  if (normalizeUserID(targetID) === getCurrentUserID()) return [];
  if (viewerRole === "owner") {
    if (target === "user") return [{ role: "moderator", label: "Promote to moderator" }, { role: "admin", label: "Promote to admin" }];
    if (target === "moderator") return [{ role: "admin", label: "Promote to admin" }, { role: "user", label: "Demote" }];
    if (target === "admin") return [{ role: "moderator", label: "Demote" }];
  }
  if (viewerRole === "admin") {
    if (target === "user") return [{ role: "moderator", label: "Promote to moderator" }];
    if (target === "moderator") return [{ role: "user", label: "Demote moderator" }];
  }
  return [];
}

function renderRoleUserItem(user) {
  if (!user) return "";
  const actions = getAvailableRoleActions(user.role, user.id);
  return `
    <article class="center-item">
      <div class="center-item-head">
        <a class="center-item-link" data-link href="${escapeHTML(getProfilePath(user.username))}">${escapeHTML(user.name || user.username)}</a>
        <span class="center-item-meta">@${escapeHTML(user.username)}</span>
      </div>
      <div class="center-inline-meta">
        <span>${escapeHTML(humanizeRole(user.role))}</span>
        ${renderStaffBadges(user.badges)}
      </div>
      <div class="center-item-actions">
        ${actions
          .map(
            (action) => `
              <button class="btn btn-ghost btn-compact" type="button" data-action="change-user-role" data-user-id="${escapeHTML(String(user.id))}" data-next-role="${escapeHTML(action.role)}">${escapeHTML(action.label)}</button>
            `
          )
          .join("")}
      </div>
    </article>
  `;
}

function renderCategoryItem(category) {
  if (!category) return "";
  const isSystem = Boolean(category.is_system ?? category.isSystem);
  const isOther = String(category.code || "").trim() === "other";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>${escapeHTML(category.name || category.code || "category")}</strong>
        <span class="center-item-meta">${escapeHTML(category.code || "")}</span>
      </div>
      <div class="center-inline-meta">
        <span>${isSystem ? "system" : "custom"}</span>
      </div>
      <div class="center-item-actions">
        ${isOther || isSystem ? `<span class="center-status-label">Protected</span>` : `<button class="btn btn-ghost btn-compact" type="button" data-action="delete-category" data-category-id="${escapeHTML(String(category.id))}" data-category-name="${escapeHTML(category.name || category.code)}">Delete category</button>`}
      </div>
    </article>
  `;
}

function renderHistoryItem(item) {
  if (!item) return "";
  return `
    <article class="center-item">
      <div class="center-item-head">
        <strong>${escapeHTML(String(item.actionType || "").replace(/_/g, " "))}</strong>
        <span class="center-item-meta">${escapeHTML(formatDate(item.actedAt))}</span>
      </div>
      <div class="center-inline-meta">
        <span>${escapeHTML(item.targetType || "")} #${escapeHTML(String(item.targetId || ""))}</span>
        ${item.currentStatus ? renderCenterStatusPill(item.currentStatus) : ""}
        ${item.actor && item.actor.username ? `<span>by @${escapeHTML(item.actor.username)}</span>` : ""}
      </div>
      ${renderHistoryDetails(item)}
    </article>
  `;
}

function renderModerationPanel(activeSubtab) {
  if (activeSubtab === "queue") {
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>Under Review</h2>
            <p>Visible posts waiting for staff approval.</p>
          </div>
        </div>
        <div class="center-list">
          ${(state.center.moderation.queue || []).length ? state.center.moderation.queue.map(renderQueuePostItem).join("") : renderEmpty("Queue is clear", "There are no posts waiting for review.")}
        </div>
      </section>
    `;
  }
  if (activeSubtab === "reports") {
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>Reports</h2>
            <p>Active report queue for your staff role.</p>
          </div>
        </div>
        <div class="center-list">
          ${(state.center.moderation.reports || []).length ? state.center.moderation.reports.map((item) => renderReportCard(item, { staff: true })).join("") : renderEmpty("No active reports", "The report queue is empty.")}
        </div>
      </section>
    `;
  }
  return `
    <section class="surface center-panel">
      <div class="section-row center-panel-head">
        <div>
          <h2>History</h2>
          <p>Immutable moderation trail.</p>
        </div>
      </div>
      <div class="center-list">
        ${(state.center.moderation.history || []).length ? state.center.moderation.history.map(renderHistoryItem).join("") : renderEmpty("No history", "No moderation actions recorded yet.")}
      </div>
    </section>
  `;
}

function renderManagementPanel(activeSubtab) {
  if (activeSubtab === "requests") {
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>Requests</h2>
            <p>Role applications routed to your level.</p>
          </div>
        </div>
        <div class="center-list">
          ${(state.center.management.requests || []).length ? state.center.management.requests.map(renderRoleRequestItem).join("") : renderEmpty("No requests", "There are no role requests yet.")}
        </div>
      </section>
    `;
  }
  if (activeSubtab === "roles") {
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>Roles</h2>
            <p>Direct role actions apply immediately.</p>
          </div>
        </div>
        <div class="center-list">
          ${(state.users || []).length ? state.users.map(renderRoleUserItem).join("") : renderEmpty("No users", "No users available.")}
        </div>
      </section>
    `;
  }
  if (activeSubtab === "categories") {
    return `
      <section class="surface center-panel">
        <div class="section-row center-panel-head">
          <div>
            <h2>Categories</h2>
            <p>Create categories and move deleted-category posts into other.</p>
          </div>
          <div class="center-panel-actions">
            <button class="btn btn-primary btn-compact" type="button" data-action="create-category">Create category</button>
          </div>
        </div>
        <div class="center-list">
          ${(state.center.management.categories || []).length ? state.center.management.categories.map(renderCategoryItem).join("") : renderEmpty("No categories", "No categories available.")}
        </div>
      </section>
    `;
  }
  return `
    <section class="surface center-panel">
      <div class="section-row center-panel-head">
        <div>
          <h2>Journal</h2>
          <p>Audit journal for moderation and management actions.</p>
        </div>
        <div class="center-panel-actions">
          ${isOwnerRole(getCurrentUserRole()) ? `<button class="btn btn-ghost btn-compact" type="button" data-action="purge-history">Purge history</button>` : ""}
        </div>
      </div>
      <div class="center-list">
        ${(state.center.management.journal || []).length ? state.center.management.journal.map(renderHistoryItem).join("") : renderEmpty("No journal", "No audit items available.")}
      </div>
    </section>
  `;
}

function renderCenterContent(activeTab, activeSubtab) {
  const activity = state.center.activity || { posts: [], reactions: [], comments: [] };
  return `
    <section class="page-head">
      <div>
        <h1>Center</h1>
        <p>One place for notifications, moderation and management.</p>
      </div>
    </section>
    <section class="center-shell">
      ${renderCenterTabs(activeTab)}
      ${renderCenterSubtabs(activeTab, activeSubtab)}
      ${
        activeTab === "notifications"
          ? renderCenterNotificationPanel(activeSubtab)
          : activeTab === "moderation"
            ? renderModerationPanel(activeSubtab)
            : activeTab === "management"
              ? renderManagementPanel(activeSubtab)
          : `
            <div class="center-activity-grid">
              ${renderActivitySection("My posts", "Posts you created.", activity.posts, renderActivityPostItem, activity.postsHasMore)}
              ${renderActivitySection("My reactions", "Where you liked or disliked content.", activity.reactions, renderActivityReactionItem, activity.reactionsHasMore)}
              ${renderActivitySection("My comments", "Your comments with linked post context.", activity.comments, renderActivityCommentItem, activity.commentsHasMore)}
            </div>
          `
      }
    </section>
  `;
}

async function centerView() {
  await ensureUser(true);
  if (!state.user) {
    return {
      html: renderLayout({
        mode: "center",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Center</h2><p>Login to open your activity and notifications center.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  const activeTab = getCenterTab();
  const activeSubtab = getCenterSubtab(activeTab);

  await ensureNotificationSummary();
  if (activeTab === "notifications") {
    if (activeSubtab === "reports") {
      await loadMyReports(true);
    } else if (activeSubtab === "appeals") {
      await loadMyAppeals(true);
      await loadAppealInbox(true);
    } else {
      await loadCenterNotifications(activeSubtab, { force: true });
    }
  } else if (activeTab === "moderation") {
    if (activeSubtab === "queue") {
      await loadModerationQueue(true);
    } else if (activeSubtab === "reports") {
      await loadModerationReports(true);
    } else {
      await loadModerationHistory(true);
    }
  } else if (activeTab === "management") {
    await ensureUsersLoaded(true);
    if (activeSubtab === "requests") {
      await loadManagementRequests(true);
    } else if (activeSubtab === "categories") {
      await loadManagementCategories(true);
    } else if (activeSubtab === "journal") {
      await loadManagementJournal(true);
    }
  } else {
    await loadCenterActivity(true);
  }

  return {
    html: renderLayout({
      mode: "center",
      hideHeading: true,
      content: renderCenterContent(activeTab, activeSubtab),
    }),
    onMount: () => {
      bindHeaderActions();
      bindCenterActions();
    },
  };
}

function bindCenterActions() {
  document.querySelectorAll("[data-action='dm-center-read']").forEach((button) => {
    button.addEventListener("click", async () => {
      const peerID = button.getAttribute("data-dm-peer-id");
      const lastMessageID = button.getAttribute("data-dm-last-message-id");
      if (!peerID || !lastMessageID) return;
      try {
        await markDMConversationRead(peerID, lastMessageID);
        state.dmPeers = sortDMPeers(
          (state.dmPeers || []).map((peer) =>
            normalizeUserID(peer && peer.id) === normalizeUserID(peerID)
              ? { ...peer, unreadCount: 0 }
              : peer
          )
        );
        refreshCenterRouteIfOpen();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to mark conversation as read.");
      }
    });
  });

  document.querySelectorAll("[data-action='notification-read']").forEach((button) => {
    button.addEventListener("click", async () => {
      const notificationID = button.getAttribute("data-notification-id");
      if (!notificationID) return;
      try {
        const response = await apiFetch(`/api/center/notifications/${encodeURIComponent(notificationID)}/read`, {
          method: "POST",
          body: "{}",
        });
        if (response && response.notification) {
          replaceNotificationItem(response.notification);
        }
        if (response && response.summary) {
          setNotificationSummary(response.summary);
        }
        refreshCenterRouteIfOpen();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to mark notification as read.");
      }
    });
  });

  document.querySelectorAll("[data-action='notification-delete']").forEach((button) => {
    button.addEventListener("click", async () => {
      const notificationID = button.getAttribute("data-notification-id");
      if (!notificationID) return;
      if (!confirm("Delete this notification?")) return;
      try {
        const response = await apiFetch(`/api/center/notifications/${encodeURIComponent(notificationID)}`, {
          method: "DELETE",
        });
        removeNotificationItem(notificationID);
        if (response && response.summary) {
          setNotificationSummary(response.summary);
        }
        refreshCenterRouteIfOpen();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to delete notification.");
      }
    });
  });

  document.querySelectorAll("[data-action='notifications-read-all']").forEach((button) => {
    button.addEventListener("click", async () => {
      const bucket = button.getAttribute("data-notification-bucket") || "";
      try {
        const response = await apiFetch("/api/center/notifications/read-all", {
          method: "POST",
          body: JSON.stringify({ bucket: centerNotificationAPIBucket(bucket) }),
        });
        const bucketState = getNotificationBucketState(bucket);
        bucketState.items = bucketState.items.map((item) => ({ ...item, isRead: true }));
        if (bucket === "all") {
          CENTER_NOTIFICATION_BUCKETS.filter((entry) => entry !== "all").forEach((entry) => {
            const entryState = getNotificationBucketState(entry);
            entryState.items = entryState.items.map((item) => ({ ...item, isRead: true }));
          });
        }
        if (response && response.summary) {
          setNotificationSummary(response.summary);
        }
        refreshCenterRouteIfOpen();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to mark notifications as read.");
      }
    });
  });

  document.querySelectorAll("[data-action='notifications-load-more']").forEach((button) => {
    button.addEventListener("click", async () => {
      const bucket = button.getAttribute("data-notification-bucket") || "";
      try {
        await loadCenterNotifications(bucket, { append: true });
        refreshCenterRouteIfOpen();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to load more notifications.");
      }
    });
  });

  document.querySelectorAll("[data-action='queue-approve-post']").forEach((button) => {
    button.addEventListener("click", async () => {
      const postID = button.getAttribute("data-post-id");
      if (!postID) return;
      try {
        await approveQueuedPost(postID);
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to approve post.");
      }
    });
  });

  document.querySelectorAll("[data-action='review-role-request']").forEach((button) => {
    button.addEventListener("click", async () => {
      const requestID = button.getAttribute("data-request-id");
      const approve = button.getAttribute("data-request-approve") === "1";
      if (!requestID) return;
      try {
        await reviewRoleRequestByID(requestID, approve);
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to review role request.");
      }
    });
  });

  document.querySelectorAll("[data-action='change-user-role']").forEach((button) => {
    button.addEventListener("click", async () => {
      const userID = button.getAttribute("data-user-id");
      const nextRole = button.getAttribute("data-next-role");
      if (!userID || !nextRole) return;
      try {
        await changeUserRoleByID(userID, nextRole);
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to change user role.");
      }
    });
  });

  document.querySelectorAll("[data-action='staff-report-action']").forEach((button) => {
    button.addEventListener("click", async () => {
      const reportID = button.getAttribute("data-report-id");
      const mode = button.getAttribute("data-report-mode");
      if (!reportID || !mode) return;
      try {
        await closeReportByID(reportID, mode === "action");
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to review report.");
      }
    });
  });

  document.querySelectorAll("[data-action='staff-appeal-action']").forEach((button) => {
    button.addEventListener("click", async () => {
      const appealID = button.getAttribute("data-appeal-id");
      const mode = button.getAttribute("data-appeal-mode");
      if (!appealID || !mode) return;
      try {
        await closeAppealByID(appealID, mode === "reverse");
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to review appeal.");
      }
    });
  });

  document.querySelectorAll("[data-action='create-category']").forEach((button) => {
    button.addEventListener("click", async () => {
      try {
        await createCategoryFlow();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to create category.");
      }
    });
  });

  document.querySelectorAll("[data-action='delete-category']").forEach((button) => {
    button.addEventListener("click", async () => {
      const categoryID = button.getAttribute("data-category-id");
      const categoryName = button.getAttribute("data-category-name") || "category";
      if (!categoryID) return;
      try {
        await deleteCategoryFlow(categoryID, categoryName);
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to delete category.");
      }
    });
  });

  document.querySelectorAll("[data-action='purge-history']").forEach((button) => {
    button.addEventListener("click", async () => {
      try {
        await purgeHistoryFlow();
      } catch (err) {
        if (err && err.handled) return;
        alert(err.message || "Failed to purge history.");
      }
    });
  });
}

function findPostCategories(post) {
  if (!post || !Array.isArray(post.categories)) return [];
  return post.categories
    .map((entry) => normalizeUserID(entry && entry.id))
    .filter(Boolean);
}

async function approveQueuedPost(postID) {
  const normalizedPostID = normalizeUserID(postID);
  let post = (state.center.moderation.queue || []).find((entry) => normalizeUserID(entry && entry.id) === normalizedPostID);
  if (!post && location.pathname.startsWith("/post/")) {
    post = await apiFetch(`/api/posts/${encodeURIComponent(normalizedPostID)}`);
  }
  const viewerRole = getCurrentUserRole();
  let categoryIDs = [];
  let note = "";
  if (viewerRole === "admin" || viewerRole === "owner") {
    await ensureCategories();
    const values = await showFormModal({
      title: "Approve post",
      description: "Approving removes the post from the review queue. Admin and owner can reassign categories here.",
      submitLabel: "Approve",
      fields: [
        {
          type: "checkbox-group",
          name: "categories",
          label: "Categories",
          value: post ? findPostCategories(post) : [],
          options: (state.categories || []).map((category) => ({
            value: String(category.id),
            label: category.name,
          })),
        },
        {
          type: "textarea",
          name: "note",
          label: "Note",
          maxLength: 2000,
        },
      ],
    });
    if (!values) return;
    categoryIDs = Array.isArray(values.categories)
      ? values.categories.map((value) => Number.parseInt(value, 10)).filter((value) => Number.isFinite(value) && value > 0)
      : [];
    if (categoryIDs.length === 0) {
      throw new Error("Choose at least one category.");
    }
    note = String(values.note || "").trim();
  } else {
    const values = await showNoteModal({
      title: "Approve post",
      description: "Approving removes the post from the review queue.",
      submitLabel: "Approve",
      noteLabel: "Note",
      required: false,
    });
    if (values == null) return;
    note = String(values.note || "").trim();
  }

  await apiFetch(`/api/moderation/posts/${encodeURIComponent(normalizedPostID)}/approve`, {
    method: "POST",
    body: JSON.stringify({ categories: categoryIDs, note }),
  });
  await ensureCategories();
  await loadModerationQueue(true);
  await loadModerationHistory(true);
  await ensureNotificationSummary(true);
  refreshCenterRouteIfOpen();
}

async function reviewRoleRequestByID(requestID, approve) {
  const confirmed = await showConfirmModal({
    title: approve ? "Approve request" : "Reject request",
    description: approve
      ? "This role change applies immediately."
      : "Rejecting a role request requires a note.",
    confirmLabel: approve ? "Approve" : "Continue",
  });
  if (!confirmed) return;

  const values = await showNoteModal({
    title: approve ? "Approval note" : "Rejection note",
    description: approve ? "Optional note for the applicant." : "Explain why the request was rejected.",
    submitLabel: approve ? "Approve" : "Reject",
    noteLabel: "Note",
    required: !approve,
  });
  if (values == null) return;

  await apiFetch(`/api/moderation/requests/${encodeURIComponent(requestID)}/review`, {
    method: "POST",
    body: JSON.stringify({
      approve: Boolean(approve),
      note: values.note,
    }),
  });
  await ensureUsersLoaded(true);
  await loadManagementRequests(true);
  await loadManagementJournal(true);
  await ensureNotificationSummary(true);
  refreshCenterRouteIfOpen();
}

async function changeUserRoleByID(userID, nextRole) {
  const roleLabel = humanizeRole(nextRole);
  const confirmed = await showConfirmModal({
    title: `${String(nextRole) === "user" ? "Demote" : "Change role"}`,
    description: `This will apply ${roleLabel} immediately.`,
    confirmLabel: "Continue",
  });
  if (!confirmed) return;

  const values = await showNoteModal({
    title: "Role change note",
    description: "Confirmation note for the audit trail.",
    submitLabel: "Apply",
    noteLabel: "Note",
    required: false,
  });
  if (values == null) return;

  await apiFetch(`/api/moderation/users/${encodeURIComponent(userID)}/role`, {
    method: "POST",
    body: JSON.stringify({
      role: nextRole,
      note: values.note,
    }),
  });
  await ensureUsersLoaded(true);
  await ensureUser(true);
  await loadManagementJournal(true);
  await ensureNotificationSummary(true);
  refreshCenterRouteIfOpen();
  if (location.pathname.startsWith("/u/")) {
    router();
  }
}

async function closeReportByID(reportID, actionTaken) {
  const values = await showReasonNoteModal({
    title: actionTaken ? "Close report with action" : "Dismiss report",
    description: actionTaken
      ? "Explain what action was taken."
      : "Explain why this report is being dismissed.",
    submitLabel: actionTaken ? "Close report" : "Dismiss report",
    noteLabel: "Decision note",
    defaultReason: "other",
  });
  if (!values) return;

  await apiFetch(`/api/moderation/reports/${encodeURIComponent(reportID)}/review`, {
    method: "POST",
    body: JSON.stringify({
      actionTaken: Boolean(actionTaken),
      reason: values.reason,
      note: values.note,
    }),
  });
  await loadModerationReports(true);
  await loadModerationHistory(true);
  await loadMyReports(true);
  await ensureNotificationSummary(true);
  refreshCenterRouteIfOpen();
}

async function closeAppealByID(appealID, reverse) {
  const values = await showNoteModal({
    title: reverse ? "Reverse decision" : "Uphold decision",
    description: reverse
      ? "Leave a note explaining the reversal."
      : "Leave a note explaining why the original decision stands.",
    submitLabel: reverse ? "Reverse" : "Uphold",
    noteLabel: "Decision note",
    required: true,
  });
  if (!values) return;

  await apiFetch(`/api/moderation/appeals/${encodeURIComponent(appealID)}/review`, {
    method: "POST",
    body: JSON.stringify({
      reverse: Boolean(reverse),
      note: values.note,
    }),
  });
  await loadAppealInbox(true);
  await loadMyAppeals(true);
  await loadModerationHistory(true);
  await loadManagementJournal(true);
  await ensureNotificationSummary(true);
  refreshCenterRouteIfOpen();
}

async function createCategoryFlow() {
  const values = await showFormModal({
    title: "Create category",
    description: "Categories are managed by admin and owner only.",
    submitLabel: "Create",
    fields: [
      {
        type: "text",
        name: "name",
        label: "Name",
        required: true,
      },
    ],
  });
  if (!values) return;
  await apiFetch("/api/moderation/categories", {
    method: "POST",
    body: JSON.stringify({ name: values.name }),
  });
  await ensureCategories();
  await loadManagementCategories(true);
  await loadManagementJournal(true);
  refreshCenterRouteIfOpen();
}

async function deleteCategoryFlow(categoryID, categoryName) {
  const confirmed = await showConfirmModal({
    title: "Delete category",
    description: `Posts from ${categoryName} will be moved to other.`,
    confirmLabel: "Continue",
  });
  if (!confirmed) return;
  const values = await showNoteModal({
    title: "Delete category",
    description: "Leave an audit note for this bulk move.",
    submitLabel: "Delete category",
    noteLabel: "Note",
    required: true,
  });
  if (values == null) return;
  await apiFetch(`/api/moderation/categories/${encodeURIComponent(categoryID)}/delete`, {
    method: "POST",
    body: JSON.stringify({ note: values.note }),
  });
  await ensureCategories();
  await loadManagementCategories(true);
  await loadManagementJournal(true);
  refreshCenterRouteIfOpen();
}

async function purgeHistoryFlow() {
  const confirmed = await showConfirmModal({
    title: "Purge history",
    description: "This bulk action removes audit records. Continue to choose filters.",
    confirmLabel: "Continue",
  });
  if (!confirmed) return;

  const values = await showFormModal({
    title: "Purge history",
    description: "Leave filters blank to purge all matching records.",
    submitLabel: "Purge",
    fields: [
      { type: "text", name: "actionType", label: "Action type", value: "" },
      { type: "text", name: "targetType", label: "Target type", value: "" },
      { type: "text", name: "status", label: "Status", value: "" },
      { type: "datetime-local", name: "from", label: "From", value: "" },
      { type: "datetime-local", name: "to", label: "To", value: "" },
      { type: "textarea", name: "note", label: "Note", required: true, maxLength: 2000 },
    ],
  });
  if (!values) return;

  const toRFC3339 = (value) => {
    const text = String(value || "").trim();
    if (!text) return "";
    const date = new Date(text);
    return Number.isNaN(date.getTime()) ? "" : date.toISOString();
  };

  await apiFetch("/api/moderation/history/purge", {
    method: "POST",
    body: JSON.stringify({
      actionType: values.actionType,
      targetType: values.targetType,
      status: values.status,
      from: toRFC3339(values.from),
      to: toRFC3339(values.to),
      note: values.note,
    }),
  });
  await loadModerationHistory(true);
  await loadManagementJournal(true);
  refreshCenterRouteIfOpen();
}

async function submitReportFlow(targetType, targetID) {
  const values = await showReasonNoteModal({
    title: "Report content",
    description: "Reports require both a reason and a note.",
    submitLabel: "Send report",
    noteLabel: "Note",
    defaultReason: "other",
  });
  if (!values) return false;

  await apiFetch("/api/moderation/reports", {
    method: "POST",
    body: JSON.stringify({
      targetType,
      targetId: Number.parseInt(String(targetID), 10),
      reason: values.reason,
      note: values.note,
    }),
  });
  await loadMyReports(true);
  await ensureNotificationSummary(true);
  return true;
}

async function submitAppealFlow(targetType, targetID) {
  const values = await showNoteModal({
    title: "Appeal decision",
    description: "Appeals require a note and are routed to the next review level.",
    submitLabel: "Send appeal",
    noteLabel: "Appeal note",
    required: true,
  });
  if (!values) return false;

  await apiFetch("/api/moderation/appeals", {
    method: "POST",
    body: JSON.stringify({
      targetType,
      targetId: Number.parseInt(String(targetID), 10),
      note: values.note,
    }),
  });
  await loadMyAppeals(true);
  await ensureNotificationSummary(true);
  return true;
}

async function softDeleteContentFlow(targetType, targetID) {
  const values = await showReasonNoteModal({
    title: "Soft delete content",
    description: "This hides the content for regular users and records an audit entry.",
    submitLabel: "Soft delete",
    noteLabel: "Note",
    defaultReason: "other",
  });
  if (!values) return false;

  await apiFetch(`/api/moderation/${targetType === "post" ? "posts" : "comments"}/${encodeURIComponent(targetID)}/soft-delete`, {
    method: "POST",
    body: JSON.stringify({
      reason: values.reason,
      note: values.note,
    }),
  });
  await loadModerationHistory(true);
  await loadModerationReports(true);
  await ensureNotificationSummary(true);
  return true;
}

async function restoreContentFlow(targetType, targetID) {
  const values = await showNoteModal({
    title: "Restore content",
    description: "Restoring makes the saved content visible again.",
    submitLabel: "Restore",
    noteLabel: "Note",
    required: false,
  });
  if (values == null) return false;

  await apiFetch(`/api/moderation/${targetType === "post" ? "posts" : "comments"}/${encodeURIComponent(targetID)}/restore`, {
    method: "POST",
    body: JSON.stringify({ note: values.note }),
  });
  await loadModerationHistory(true);
  await ensureNotificationSummary(true);
  return true;
}

async function hardDeleteContentFlow(targetType, targetID) {
  const confirmed = await showConfirmModal({
    title: "Hard delete content",
    description: "This permanently removes the content.",
    confirmLabel: "Continue",
  });
  if (!confirmed) return false;
  const values = await showReasonNoteModal({
    title: "Hard delete content",
    description: "Leave the reason and note for the audit trail.",
    submitLabel: "Hard delete",
    noteLabel: "Note",
    defaultReason: "other",
  });
  if (!values) return false;

  await apiFetch(`/api/moderation/${targetType === "post" ? "posts" : "comments"}/${encodeURIComponent(targetID)}/hard-delete`, {
    method: "POST",
    body: JSON.stringify({
      reason: values.reason,
      note: values.note,
    }),
  });
  await loadModerationHistory(true);
  await ensureNotificationSummary(true);
  return true;
}

async function toggleDeleteProtectionFlow(postID, nextProtected) {
  const values = await showNoteModal({
    title: nextProtected ? "Protect post" : "Remove protection",
    description: nextProtected
      ? "Protected posts cannot be soft-deleted or hard-deleted until protection is removed."
      : "Removing protection allows delete actions again.",
    submitLabel: nextProtected ? "Protect" : "Remove protection",
    noteLabel: "Note",
    required: false,
  });
  if (values == null) return false;

  await apiFetch(`/api/moderation/posts/${encodeURIComponent(postID)}/protection`, {
    method: "POST",
    body: JSON.stringify({
      protected: Boolean(nextProtected),
      note: values.note,
    }),
  });
  await loadModerationHistory(true);
  await ensureNotificationSummary(true);
  return true;
}

async function requestRoleFlow(requestedRole) {
  const values = await showNoteModal({
    title: `Request ${humanizeRole(requestedRole)}`,
    description: "Leave a short note for the reviewer.",
    submitLabel: "Send request",
    noteLabel: "Note",
    required: false,
  });
  if (values == null) return false;

  await apiFetch("/api/moderation/requests", {
    method: "POST",
    body: JSON.stringify({
      requestedRole,
      note: values.note,
    }),
  });
  await loadManagementRequests(true).catch(() => {});
  await ensureNotificationSummary(true);
  return true;
}

async function updatePostCategoriesFlow(post) {
  await ensureCategories();
  const values = await showFormModal({
    title: "Change post categories",
    description: "Admin and owner can reassign categories at any time.",
    submitLabel: "Save categories",
    fields: [
      {
        type: "checkbox-group",
        name: "categories",
        label: "Categories",
        value: findPostCategories(post),
        options: (state.categories || []).map((category) => ({
          value: String(category.id),
          label: category.name,
        })),
      },
      {
        type: "textarea",
        name: "note",
        label: "Note",
        maxLength: 2000,
      },
    ],
  });
  if (!values) return false;
  const categoryIDs = Array.isArray(values.categories)
    ? values.categories.map((value) => Number.parseInt(value, 10)).filter((value) => Number.isFinite(value) && value > 0)
    : [];
  if (categoryIDs.length === 0) {
    throw new Error("Choose at least one category.");
  }

  await apiFetch(`/api/moderation/posts/${encodeURIComponent(post.id)}/categories`, {
    method: "POST",
    body: JSON.stringify({
      categories: categoryIDs,
      note: values.note,
    }),
  });
  await ensureCategories();
  await loadModerationHistory(true);
  await loadManagementJournal(true);
  return true;
}

async function feedView() {
  await ensureUser(true);
  await ensureCategories();
  const posts = (await loadPosts()) || [];

  const content = `
    <section class="page-head">
      <div>
        <h1>Explore</h1>
        <p>Discover new posts, comments, and category threads.</p>
      </div>
    </section>
    <div class="quick-filter-row">
      <button class="quick-pill ${!state.filters.mine && !state.filters.liked ? "is-active" : ""}" data-quick="all" type="button">${icon("filter")} All</button>
      <a class="quick-pill" data-link href="/new">${icon("post")} Create</a>
    </div>
    <section class="feed-stack">
      ${posts.length ? posts.map(renderPostCard).join("") : renderEmpty("No posts yet", "Try another filter or create a new post.", state.user ? "/new" : "/login", state.user ? "Create post" : "Login")}
    </section>
  `;

  return {
    html: renderLayout({ mode: "feed", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      bindFeedFilters();
      bindPostReactions();
      document.querySelectorAll("[data-quick]").forEach((btn) => {
        btn.addEventListener("click", () => {
          const mode = btn.getAttribute("data-quick");
          if (mode === "all") {
            state.filters.mine = false;
            state.filters.liked = false;
            state.filters.q = "";
          } else if (mode === "mine" && state.user) {
            state.filters.mine = !state.filters.mine;
            if (state.filters.mine) state.filters.liked = false;
          } else if (mode === "liked" && state.user) {
            state.filters.liked = !state.filters.liked;
            if (state.filters.liked) state.filters.mine = false;
          }
          router();
        });
      });
    },
  };
}

async function dmView(params) {
  await ensureUser(true);
  await ensureUsersLoaded();
  await loadDMPeers(true);

  const peerID = normalizeUserID(params && params.peer);
  if (!state.user) {
    return {
      html: renderLayout({
        mode: "dm",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Direct Messages</h2><p>Login to open a conversation.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  const hasPeer = peerID && peerID !== getCurrentUserID() && state.dmPeers.some((peer) => normalizeUserID(peer && peer.id) === peerID);
  state.dmPeerID = hasPeer ? peerID : "";
  state.dmMessages = [];
  state.dmLoading = false;
  state.dmLoadingOlder = false;
  state.dmHasMore = false;
  state.dmOlderCursor = null;
  state.dmOlderLoadAt = 0;
  state.dmDraftAttachment = null;
  state.dmAttachmentUploading = false;
  if (state.dmPeerID) {
    setDMPeerUnreadCount(state.dmPeerID, 0);
    try {
      await loadDMConversation(state.dmPeerID, DM_HISTORY_PAGE_SIZE);
      const latestMessageID = getLatestDMMessageID(state.dmMessages);
      if (latestMessageID > 0) {
        void markDMConversationRead(state.dmPeerID, latestMessageID).catch((err) => {
          debugWSWarn("dm read sync failed", err);
        });
      }
    } catch (err) {
      if (!err || err.status !== 404) throw err;
      state.dmPeerID = "";
      state.dmMessages = [];
      state.dmLoading = false;
      state.dmLoadingOlder = false;
      state.dmHasMore = false;
      state.dmOlderCursor = null;
      state.dmOlderLoadAt = 0;
      state.dmDraftAttachment = null;
      state.dmAttachmentUploading = false;
    }
  }

  const content = `
    <section class="dm-layout">
      <section id="dm-peers-panel" class="surface form-card dm-peers">
        ${renderDMPeersContent()}
      </section>
      <section id="dm-view" class="surface form-card dm-chat">
        ${renderDMViewContent()}
      </section>
    </section>
  `;

  return {
    html: renderLayout({ mode: "dm", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      syncPresencePanel();
      syncDMPeersPanel();
      syncDMView({ scrollMode: state.dmPeerID ? "bottom" : "" });
    },
  };
}

async function dmIndexView() {
  return dmView({});
}

async function profileView(params) {
  await ensureUser(true);
  await ensureAuthProviders();
  if (state.user) {
    await ensureUsersLoaded();
  }

  const routeUsername = normalizeUsername(decodeURIComponent((params && params.username) || ""));
  if (!routeUsername) {
    return {
      html: renderLayout({
        mode: "profile",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Profile</h2><p>User not found.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  let profile;
  try {
    profile = await loadPublicProfile(routeUsername);
  } catch (err) {
    if (err && err.status === 404) {
      return {
        html: renderLayout({
          mode: "profile",
          hideHeading: true,
          content: `<section class="surface form-card"><h2>Profile</h2><p>User not found.</p></section>`,
        }),
        onMount: bindHeaderActions,
      };
    }
    throw err;
  }

  const isSelf = Boolean(state.user) && normalizeUsername(profile && profile.username).toLowerCase() === getCurrentUsername().toLowerCase();
  const setupMode = isSelf && state.user && state.user.needsProfileSetup && new URLSearchParams(location.search).get("setup") === "1";
  const heading = getDisplayNameOrUsername(profile);
  const subtitle = `@${normalizeUsername(profile && profile.username) || routeUsername}`;
  const editableProfile = isSelf ? state.user : profile;
  const profileRole = normalizeRole(editableProfile && editableProfile.role);
  const profileBadges = getUserBadges(editableProfile);
  const selfDisplayName = isSelf ? String(state.user && state.user.displayName ? state.user.displayName : "").trim() : String(profile && profile.displayName ? profile.displayName : "").trim();
  const selfFirstName = getProfileFieldValue(editableProfile, "firstName");
  const selfLastName = getProfileFieldValue(editableProfile, "lastName");
  const selfAge = getProfileAgeValue(editableProfile);
  const selfGender = getProfileFieldValue(editableProfile, "gender");
  const statusNotice = isSelf ? getProfileStatusNotice() : "";
  const profilePath = getProfilePath(routeUsername);

  const content = `
    <section class="page-head">
      <div>
        <h1>${escapeHTML(heading)}</h1>
        <p class="profile-handle">${escapeHTML(subtitle)}</p>
        <div class="profile-role-row">
          <span class="center-status-label">${escapeHTML(humanizeRole(profileRole))}</span>
          ${renderStaffBadges(profileBadges)}
        </div>
      </div>
      ${
        !isSelf && state.user
          ? `<button class="btn btn-ghost" type="button" data-action="toggle-author-follow" data-username="${escapeHTML(routeUsername)}" data-following="${profile.isFollowing ? "1" : "0"}">${profile.isFollowing ? "Unfollow author" : "Follow author"}</button>`
          : ""
      }
    </section>
    <section class="surface form-card profile-card">
      <div class="section-row">
        <h2>${isSelf ? "Your profile" : "Profile"}</h2>
        <p>${isSelf ? "Display name is optional. Username stays unchanged." : "Public profile."}</p>
      </div>
      ${statusNotice ? renderNotice(statusNotice) : ""}
      ${setupMode ? renderNotice("Complete your profile setup now or skip. You can update these fields later from your profile.") : ""}
      ${renderProfileRoleActions(editableProfile, isSelf)}
      ${
        isSelf
          ? `
            <form id="profile-form" class="form-stack">
              <label class="field">
                <span>Display name</span>
                <input type="text" name="displayName" maxlength="64" value="${escapeHTML(selfDisplayName)}" placeholder="Leave blank to use your username" />
              </label>
              <label class="field">
                <span>First name</span>
                <input type="text" name="firstName" value="${escapeHTML(selfFirstName)}" placeholder="Optional" />
              </label>
              <label class="field">
                <span>Last name</span>
                <input type="text" name="lastName" value="${escapeHTML(selfLastName)}" placeholder="Optional" />
              </label>
              <label class="field">
                <span>Age</span>
                <input type="number" name="age" min="0" max="150" step="1" value="${escapeHTML(selfAge)}" placeholder="Optional" />
              </label>
              <label class="field">
                <span>Gender</span>
                <input type="text" name="gender" value="${escapeHTML(selfGender)}" placeholder="Optional" />
              </label>
              <div class="side-note">Username: ${escapeHTML(subtitle)}</div>
              <div class="form-actions">
                <button class="btn btn-primary" type="submit">Save</button>
                ${setupMode ? `<button class="btn btn-ghost" type="button" data-action="profile-skip">Skip</button>` : ""}
              </div>
              <div id="profile-error"></div>
            </form>
          `
          : `
            <div class="profile-readonly">
              ${renderProfileField("Display name", getDisplayNameOrUsername(profile))}
              ${renderProfileField("Username", subtitle)}
              ${renderProfileField("Role", humanizeRole(profileRole))}
              ${renderProfileField("First name", getProfileFieldValue(profile, "firstName"))}
              ${renderProfileField("Last name", getProfileFieldValue(profile, "lastName"))}
              ${renderProfileField("Age", getProfileAgeValue(profile))}
              ${renderProfileField("Gender", getProfileFieldValue(profile, "gender"))}
            </div>
          `
      }
    </section>
    ${isSelf ? renderLinkedAccountsPanel(routeUsername) : ""}
  `;

  return {
    html: renderLayout({ mode: "profile", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      syncPresencePanel();

      if (!isSelf) return;

      const form = document.getElementById("profile-form");
      if (form) {
        form.addEventListener("submit", async (event) => {
          event.preventDefault();
          const errorBox = document.getElementById("profile-error");
          if (errorBox) errorBox.innerHTML = "";

          const data = new FormData(form);
          const ageRaw = String(data.get("age") || "").trim();
          try {
            await apiFetch("/api/me/profile", {
              method: "PUT",
              body: JSON.stringify({
                displayName: data.get("displayName"),
                firstName: data.get("firstName"),
                lastName: data.get("lastName"),
                age: ageRaw === "" ? null : Number.parseInt(ageRaw, 10),
                gender: data.get("gender"),
              }),
            });
            await ensureUser(true);
            navigate(profilePath);
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) {
              errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to update profile.");
            }
          }
        });
      }

      const skipButton = document.querySelector("[data-action='profile-skip']");
      if (skipButton) {
        skipButton.addEventListener("click", async () => {
          const errorBox = document.getElementById("profile-error");
          if (errorBox) errorBox.innerHTML = "";
          try {
            await apiFetch("/api/me/profile", {
              method: "PUT",
              body: JSON.stringify({ skip: true }),
            });
            await ensureUser(true);
            navigate(profilePath);
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) {
              errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to update profile.");
            }
          }
        });
      }

      document.querySelectorAll("[data-action='unlink-provider']").forEach((button) => {
        button.addEventListener("click", async () => {
          const provider = button.getAttribute("data-provider");
          const errorBox = document.getElementById("local-merge-error");
          if (errorBox) errorBox.innerHTML = "";
          try {
            await apiFetch(`/api/profile/linked-accounts/${encodeURIComponent(provider)}/unlink`, { method: "POST" });
            await ensureUser(true);
            router();
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) {
              errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to unlink provider.");
            }
          }
        });
      });

      const localMergeForm = document.getElementById("local-merge-form");
      if (localMergeForm) {
        localMergeForm.addEventListener("submit", async (event) => {
          event.preventDefault();
          const errorBox = document.getElementById("local-merge-error");
          if (errorBox) errorBox.innerHTML = "";

          const data = new FormData(localMergeForm);
          try {
            const result = await apiFetch("/api/profile/local-account/merge", {
              method: "POST",
              body: JSON.stringify({
                login: data.get("login"),
                password: data.get("password"),
              }),
            });
            navigate(result && result.redirectPath ? result.redirectPath : profilePath);
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) {
              errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to start merge flow.");
            }
          }
        });
      }
    },
  };
}

function renderAuthLayout(kind) {
  const isLogin = kind === "login";
  const authNotice = getAuthStatusNotice();
  const nextPath = getSearchParam("next") || "/";
  const content = `
    <section class="auth-grid">
      <div class="surface auth-info">
        <div class="auth-badge">Forum Access</div>
        <h2>${isLogin ? "Welcome back" : "Create your account"}</h2>
        <p>${isLogin ? "Login to post, comment, and manage reactions." : "Register to join conversations and publish posts."}</p>
        <ul class="auth-feature-list">
          <li>Create posts with multiple categories</li>
          <li>Comment and react on posts and comments</li>
          <li>Use created/liked filters in feed</li>
        </ul>
      </div>
      <div class="surface auth-form-card">
        <form id="${kind}-form" class="form-stack">
          <div class="form-intro">
            <h2>${isLogin ? "Login" : "Register"}</h2>
            <p>${isLogin ? "Use your email or username and password." : "Email, username and password are required."}</p>
          </div>
          ${authNotice ? renderNotice(authNotice) : ""}
          ${
            isLogin
              ? `
                <label class="field"><span>Email or username</span><input type="text" name="login" required /></label>
                <label class="field"><span>Password</span><input type="password" name="password" required /></label>
              `
              : `
                <label class="field"><span>Email</span><input type="email" name="email" required /></label>
                <label class="field"><span>Username</span><input type="text" name="username" required /></label>
                <label class="field"><span>Password</span><input type="password" name="password" required /></label>
              `
          }
          <div class="form-actions">
            <button class="btn btn-primary" type="submit">${isLogin ? "Login" : "Create account"}</button>
            <a class="btn btn-ghost" data-link href="${isLogin ? "/register" : "/login"}">${isLogin ? "Register" : "Login"}</a>
          </div>
          ${renderOAuthButtons({ intent: "login", nextPath, title: "Or continue with" })}
          <div id="${kind}-error"></div>
        </form>
      </div>
    </section>
  `;
  return renderLayout({ mode: kind, hideHeading: true, content });
}

async function loginView() {
  await ensureAuthProviders();
  return {
    html: renderAuthLayout("login"),
    onMount: () => {
      bindHeaderActions();
      const form = document.getElementById("login-form");
      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        const data = new FormData(form);
        try {
          await apiFetch("/api/login", {
            method: "POST",
            body: JSON.stringify({
              login: data.get("login"),
              password: data.get("password"),
            }),
          });
          await ensureUser(true);
          ensureRealtimeSocket();
          const nextPath = getSearchParam("next");
          if (!maybeRedirectToProfileSetup()) {
            navigate(nextPath && nextPath.startsWith("/") ? nextPath : "/");
          }
        } catch (err) {
          if (err && err.handled) return;
          document.getElementById("login-error").innerHTML = renderNotice(err.message);
        }
      });
    },
  };
}

async function registerView() {
  await ensureAuthProviders();
  return {
    html: renderAuthLayout("register"),
    onMount: () => {
      bindHeaderActions();
      const form = document.getElementById("register-form");
      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        const data = new FormData(form);
        const payload = {
          email: data.get("email"),
          username: data.get("username"),
          password: data.get("password"),
        };
        try {
          await apiFetch("/api/register", { method: "POST", body: JSON.stringify(payload) });
          await apiFetch("/api/login", {
            method: "POST",
            body: JSON.stringify({ email: payload.email, password: payload.password }),
          });
          await ensureUser(true);
          ensureRealtimeSocket();
          const nextPath = getSearchParam("next");
          if (!maybeRedirectToProfileSetup()) {
            navigate(nextPath && nextPath.startsWith("/") ? nextPath : "/");
          }
        } catch (err) {
          if (err && err.handled) return;
          document.getElementById("register-error").innerHTML = renderNotice(err.message);
        }
      });
    },
  };
}

async function accountLinkView() {
  await ensureUser(true);
  await ensureAuthProviders();

  const flowToken = getSearchParam("flow");
  if (!flowToken) {
    return {
      html: renderLayout({
        mode: "login",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Account linking</h2><p>Link flow token is missing.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  let flow;
  try {
    flow = await apiFetch(`/api/auth/flows/${encodeURIComponent(flowToken)}`);
  } catch (err) {
    return {
      html: renderLayout({
        mode: "login",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Account linking</h2>${renderNotice(err && err.message ? err.message : "Failed to load link flow.")}</section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  const link = flow && flow.link;
  if (!link) {
    return {
      html: renderLayout({
        mode: "login",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Account linking</h2><p>Link flow is invalid.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  const providerLabel = getProviderLabel(link.externalIdentity && link.externalIdentity.provider);
  const canConfirmWithPassword = Boolean(link.existingAccount && link.existingAccount.hasPassword);
  const nextPath = String(link.nextPath || "/").trim() || "/";
  const content = `
    <section class="auth-grid">
      <div class="surface auth-info">
        <div class="auth-badge">Account Confirmation</div>
        <h2>Existing account found</h2>
        <p>We found a forum account with the same email. It will not be linked automatically.</p>
        ${renderFlowUserSummaryCard("Existing forum account", link.existingAccount)}
      </div>
      <div class="surface auth-form-card">
        <div class="form-stack">
          <div class="form-intro">
            <h2>Link ${escapeHTML(providerLabel)}</h2>
            <p>Confirm ownership of the existing forum account, then complete the link explicitly.</p>
          </div>
          <div class="flow-summary-card">
            <div class="flow-summary-head">
              <strong>External identity</strong>
              <span class="status-pill is-linked">${escapeHTML(providerLabel)}</span>
            </div>
            <div class="flow-summary-grid">
              ${renderProfileField("Display name", link.externalIdentity && link.externalIdentity.displayName)}
              ${renderProfileField("Email", link.externalIdentity && link.externalIdentity.email)}
              ${renderProfileField("Provider login", link.externalIdentity && link.externalIdentity.username)}
            </div>
          </div>
          ${
            link.currentSessionMatchesExisting
              ? `
                <div class="notice-box">Current session already belongs to the matching account. Finish linking explicitly.</div>
                <div class="form-actions">
                  <button class="btn btn-primary" type="button" data-action="complete-account-link">Link ${escapeHTML(providerLabel)}</button>
                  <a class="btn btn-ghost" data-link href="${escapeHTML(nextPath)}">Cancel</a>
                </div>
              `
              : canConfirmWithPassword
                ? `
                  <form id="link-confirm-form" class="form-stack">
                    <label class="field"><span>Email or username</span><input type="text" name="login" required /></label>
                    <label class="field"><span>Password</span><input type="password" name="password" required /></label>
                    <div class="form-actions">
                      <button class="btn btn-primary" type="submit">Confirm and link</button>
                      <a class="btn btn-ghost" data-link href="/login?next=${encodeURIComponent(`/account-link?flow=${flowToken}`)}">Login manually</a>
                    </div>
                  </form>
                `
                : `
                  ${renderNotice(`This existing account has no local password. Sign in to that account using one of its linked providers, then start linking ${providerLabel} again from the profile screen.`)}
                  <div class="provider-pill-row">${renderLinkedProviderPills(link.existingAccount && link.existingAccount.linkedAccounts)}</div>
                  <div class="form-actions">
                    <a class="btn btn-primary" data-link href="/login">Go to login</a>
                    <a class="btn btn-ghost" data-link href="/">Back to feed</a>
                  </div>
                `
          }
          <div id="account-link-error"></div>
        </div>
      </div>
    </section>
  `;

  return {
    html: renderLayout({ mode: "login", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();

      const errorBox = document.getElementById("account-link-error");
      const confirmForm = document.getElementById("link-confirm-form");
      if (confirmForm) {
        confirmForm.addEventListener("submit", async (event) => {
          event.preventDefault();
          if (errorBox) errorBox.innerHTML = "";
          const data = new FormData(confirmForm);
          try {
            const result = await apiFetch(`/api/auth/flows/${encodeURIComponent(flowToken)}/confirm-local`, {
              method: "POST",
              body: JSON.stringify({
                login: data.get("login"),
                password: data.get("password"),
              }),
            });
            await ensureUser(true);
            navigate(result && result.redirectPath ? result.redirectPath : nextPath);
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to confirm link.");
          }
        });
      }

      const completeButton = document.querySelector("[data-action='complete-account-link']");
      if (completeButton) {
        completeButton.addEventListener("click", async () => {
          if (errorBox) errorBox.innerHTML = "";
          try {
            const result = await apiFetch(`/api/auth/flows/${encodeURIComponent(flowToken)}/complete`, {
              method: "POST",
              body: JSON.stringify({}),
            });
            await ensureUser(true);
            navigate(result && result.redirectPath ? result.redirectPath : nextPath);
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to complete link.");
          }
        });
      }
    },
  };
}

async function accountMergeView() {
  await ensureUser(true);
  await ensureAuthProviders();

  const flowToken = getSearchParam("flow");
  if (!flowToken) {
    return {
      html: renderLayout({
        mode: "profile",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Account merge</h2><p>Merge flow token is missing.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  let flow;
  try {
    flow = await apiFetch(`/api/auth/flows/${encodeURIComponent(flowToken)}`);
  } catch (err) {
    return {
      html: renderLayout({
        mode: "profile",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Account merge</h2>${renderNotice(err && err.message ? err.message : "Failed to load merge flow.")}</section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  const merge = flow && flow.merge;
  if (!merge) {
    return {
      html: renderLayout({
        mode: "profile",
        hideHeading: true,
        content: `<section class="surface form-card"><h2>Account merge</h2><p>Merge flow is invalid.</p></section>`,
      }),
      onMount: bindHeaderActions,
    };
  }

  const content = `
    <section class="surface form-card flow-page-card">
      <div class="section-row">
        <h2>Merge accounts</h2>
        <p>Merge is explicit and transactional. Content stays with the canonical account.</p>
      </div>
      ${merge.reason === "provider_conflict" ? renderNotice(`This ${getProviderLabel(merge.provider)} identity is already linked to another forum account. Review and confirm the merge explicitly.`) : ""}
      <div class="flow-compare-grid">
        ${renderFlowUserSummaryCard("Canonical account", merge.canonicalUser)}
        ${renderFlowUserSummaryCard("Source account", merge.sourceUser)}
      </div>
      <div class="flow-summary-card">
        <div class="flow-summary-head">
          <strong>Final account after merge</strong>
          <span class="status-pill is-linked">explicit confirm required</span>
        </div>
        <div class="flow-summary-grid">
          ${renderProfileField("Default display name", merge.defaultDisplayName)}
          ${renderProfileField("Final username", `@${normalizeUsername(merge.finalUsername)}`)}
          ${renderProfileField("Final login email", merge.finalEmail)}
        </div>
      </div>
      <form id="merge-complete-form" class="form-stack">
        <label class="field">
          <span>Display name after merge</span>
          <input type="text" name="displayName" maxlength="64" value="${escapeHTML(merge.defaultDisplayName || "")}" placeholder="Leave as suggested or change it explicitly" />
        </label>
        <div class="form-actions">
          <button class="btn btn-primary" type="submit">Confirm merge</button>
          <a class="btn btn-ghost" data-link href="${escapeHTML(getProfilePath(state.user && state.user.username))}">Cancel</a>
        </div>
        <div id="account-merge-error"></div>
      </form>
    </section>
  `;

  return {
    html: renderLayout({ mode: "profile", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      const form = document.getElementById("merge-complete-form");
      const errorBox = document.getElementById("account-merge-error");
      if (!form) return;

      form.addEventListener("submit", async (event) => {
        event.preventDefault();
        if (errorBox) errorBox.innerHTML = "";
        const data = new FormData(form);
        try {
          const result = await apiFetch(`/api/auth/flows/${encodeURIComponent(flowToken)}/complete`, {
            method: "POST",
            body: JSON.stringify({
              displayName: data.get("displayName"),
            }),
          });
          await ensureUser(true);
          navigate(result && result.redirectPath ? result.redirectPath : "/");
        } catch (err) {
          if (err && err.handled) return;
          if (errorBox) errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to complete merge.");
        }
      });
    },
  };
}

async function newPostView() {
  await ensureUser(true);
  await ensureCategories();
  const categoryChoices = (state.categories || [])
    .map(
      (cat) => `
          <label class="category-choice">
            <input type="checkbox" name="categories" value="${cat.id}" ${state.user ? "" : "disabled"} />
            <span>${escapeHTML(cat.name)}</span>
          </label>
        `
    )
    .join("");

  const content = `
    <section class="page-head page-head-inline">
      <div>
        <h1>New Post</h1>
        <p>Create a post with title, content and multiple categories.</p>
      </div>
      <a class="btn btn-ghost btn-compact" data-link href="/" aria-label="Back">${icon("back")}</a>
    </section>
    <section class="surface form-card">
      ${!state.user ? renderNotice("Login to create a post.") : ""}
      <form id="post-form" class="form-stack">
        <label class="field"><span>Title</span><input type="text" name="title" required ${state.user ? "" : "disabled"} /></label>
        <label class="field"><span>Body</span><textarea name="body" required ${state.user ? "" : "disabled"}></textarea></label>
        <input id="post-attachment-input" type="file" accept="image/jpeg,image/png,image/gif" hidden ${state.user && !state.postAttachmentUploading ? "" : "disabled"} />
        <div id="post-attachment-preview">
          ${renderAttachmentPreview(state.postDraftAttachment, "post")}
        </div>
        <div class="field">
          <span>Categories</span>
          <div class="category-grid">
            ${categoryChoices || '<div class="side-note">No categories</div>'}
          </div>
        </div>
        <div class="form-actions">
          <button class="btn btn-ghost btn-compact attachment-picker-btn" type="button" data-action="post-pick-attachment" ${state.user && !state.postAttachmentUploading ? "" : "disabled"}>${icon("paperclip")} ${state.postAttachmentUploading ? "Uploading..." : "Attach image"}</button>
          <button class="btn btn-primary" type="submit" ${state.user ? "" : "disabled"}>${icon("send")} Publish</button>
          <a class="btn btn-ghost btn-cancel" data-link href="/">Cancel</a>
        </div>
        <div id="post-error"></div>
      </form>
    </section>
  `;

  return {
    html: renderLayout({ mode: "new", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      syncPostAttachmentControls();
      syncPostAttachmentPreview();
      const form = document.getElementById("post-form");
      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        if (!state.user) return;
        if (state.postAttachmentUploading) {
          document.getElementById("post-error").innerHTML = renderNotice("Image is still uploading.");
          return;
        }
        const data = new FormData(form);
        const selectedCategories = data
          .getAll("categories")
          .map((id) => Number(id))
          .filter((id) => Number.isFinite(id) && id > 0);
        if (selectedCategories.length === 0) {
          document.getElementById("post-error").innerHTML = renderNotice("chose category");
          return;
        }
        try {
          const post = await apiFetch("/api/posts", {
            method: "POST",
            body: JSON.stringify({
              title: data.get("title"),
              body: data.get("body"),
              categories: selectedCategories,
              ...(getAttachmentNumericID(state.postDraftAttachment) ? { attachmentId: getAttachmentNumericID(state.postDraftAttachment) } : {}),
            }),
          });
          state.postDraftAttachment = null;
          state.postAttachmentUploading = false;
          navigate(`/post/${post.id}`);
        } catch (err) {
          if (err && err.handled) return;
          document.getElementById("post-error").innerHTML = renderNotice(err.message);
        }
      });
    },
  };
}

function getCommentParentID(comment) {
  const raw = comment?.parent_id ?? comment?.parentId ?? comment?.reply_to_id ?? comment?.replyToId ?? null;
  if (raw == null) return null;
  const id = Number(raw);
  return Number.isFinite(id) && id > 0 ? id : null;
}

function renderCommentComposeForm({ postID, replyTarget = null, inline = false } = {}) {
  const replyID = replyTarget ? String(replyTarget.commentID || "") : "";
  const replyAuthor = replyTarget ? String(replyTarget.author || "comment") : "comment";
  const isReply = Boolean(replyTarget && replyID);
  const draft = isReply ? String(replyTarget.draft || "") : "";
  return `
    <form class="form-stack comment-compose-form${inline ? " is-inline-reply" : ""}" data-comment-compose-form="${escapeHTML(String(postID || ""))}"${isReply ? ` data-inline-reply-form="${escapeHTML(replyID)}"` : ""}>
      ${
        isReply
          ? `
            <div class="reply-target-box">
              <span>Replying to ${escapeHTML(replyAuthor)}</span>
              <button class="reply-target-cancel" type="button" data-action="cancel-comment-reply" aria-label="Cancel reply">Cancel</button>
            </div>
          `
          : ""
      }
      <input type="hidden" name="parent_id" value="${escapeHTML(replyID)}" />
      <label class="field">
        <span>${isReply ? "Reply" : "Comment"}</span>
        <textarea class="comment-compose-textarea" name="body" required ${state.user ? "" : "disabled"}>${escapeHTML(draft)}</textarea>
      </label>
      <div class="form-actions">
        <button class="btn btn-primary" type="submit" ${state.user ? "" : "disabled"}>${icon("send")} ${isReply ? "Post Reply" : "Post Comment"}</button>
      </div>
      <div class="comment-compose-error"></div>
    </form>
  `;
}

function renderComment(comment, { isReply = false, postID = "" } = {}) {
  const authorUsername = getAuthorUsername(comment);
  const author = getAuthorDisplay(comment);
  const isOwner = isCurrentUserOwner(comment);
  const deleted = isDeletedComment(comment);
  const isEditing = !deleted && normalizeUserID(comment && comment.id) === normalizeUserID(state.editingCommentID);
  const editable = canEditComment(comment);
  const commentText = deleted ? "[deleted]" : String(comment?.body || "");
  const activeReply = getActiveCommentReply(postID || comment?.post_id || comment?.postId || "");
  const showInlineReply = !deleted && activeReply && normalizeUserID(activeReply.commentID) === normalizeUserID(comment && comment.id);
  return `
    <article id="comment-${escapeHTML(String(comment.id))}" class="surface comment-card${isReply ? " is-reply" : ""}${deleted ? " is-deleted" : ""}">
      <div class="author-line">
        ${avatarMarkup(author, comment.avatarUrl, "xs")}
        <div>
          <div class="author-meta-row">
            <a class="author-name author-link" data-link href="${escapeHTML(getProfilePath(authorUsername))}">${escapeHTML(author)}</a>
            ${renderStaffBadges(getUserBadges(comment))}
          </div>
          <div class="meta-line">${escapeHTML(formatDate(comment.created_at))}</div>
        </div>
      </div>
      ${renderCommentFlags(comment)}
      ${
        isEditing
          ? `
            <form class="comment-edit-form form-stack" data-comment-edit-form="${escapeHTML(String(comment.id))}">
              <label class="field">
                <span>Edit comment</span>
                <textarea name="body" rows="4" required>${escapeHTML(state.editingCommentDraft || comment.body || "")}</textarea>
              </label>
              <div class="form-actions">
                <button class="btn btn-primary" type="submit">Save</button>
                <button class="btn btn-ghost" type="button" data-action="cancel-edit-comment">Cancel</button>
              </div>
            </form>
          `
          : `<p class="comment-text">${escapeHTML(commentText)}</p>`
      }
      ${
        deleted
          ? ""
          : `
            <div class="action-row" data-comment="${comment.id}">
              <button class="action-pill" type="button" data-action="like">${icon("like")} ${comment.likes || 0}</button>
              <button class="action-pill" type="button" data-action="dislike">${icon("dislike")} ${comment.dislikes || 0}</button>
              <button class="action-pill action-pill-reply" type="button" data-action="reply-comment" data-reply-comment-id="${comment.id}" data-reply-author="${escapeHTML(author)}">Reply</button>
              ${isOwner ? `<button class="action-pill" type="button" data-action="edit-comment" data-edit-comment-id="${escapeHTML(String(comment.id))}" ${editable ? "" : "disabled"}>${editable ? "Edit" : "Edit expired"}</button>` : ""}
              ${isOwner ? `<button class="action-pill" type="button" data-action="delete-comment" data-delete-comment-id="${escapeHTML(String(comment.id))}">Delete</button>` : ""}
            </div>
          `
      }
      ${renderCommentModerationActions(comment)}
      ${
        showInlineReply
          ? `
            <div class="comment-inline-compose">
              ${renderCommentComposeForm({ postID: postID || comment?.post_id || comment?.postId, replyTarget: activeReply, inline: true })}
            </div>
          `
          : ""
      }
    </article>
  `;
}

function renderCommentThreads(comments, { postID = "" } = {}) {
  const ordered = Array.isArray(comments) ? comments : [];
  const byID = new Map(ordered.map((c) => [Number(c.id), c]));
  const roots = [];
  const repliesByParent = new Map();

  ordered.forEach((comment) => {
    const id = Number(comment.id);
    const parentID = getCommentParentID(comment);

    if (!parentID || parentID === id) {
      roots.push(comment);
      return;
    }

    const parent = byID.get(parentID);
    if (!parent) {
      roots.push(comment);
      return;
    }

    // One nesting level max: if parent is already a reply, attach to the root.
    const rootParentID = getCommentParentID(parent) || parentID;
    const rootParent = byID.get(rootParentID);
    if (!rootParent || getCommentParentID(rootParent)) {
      roots.push(comment);
      return;
    }

    if (!repliesByParent.has(rootParentID)) repliesByParent.set(rootParentID, []);
    repliesByParent.get(rootParentID).push(comment);
  });

  return roots
    .map((comment) => {
      const replies = repliesByParent.get(Number(comment.id)) || [];
      return `
        <div class="comment-thread">
          ${renderComment(comment, { postID })}
          ${
            replies.length
              ? `
                <div class="comment-thread-children">
                  ${replies.map((reply) => renderComment(reply, { isReply: true, postID })).join("")}
                </div>
              `
              : ""
          }
        </div>
      `;
    })
    .join("");
}

async function postView(params) {
  await ensureUser(true);
  await ensureCategories();
  let post;
  try {
    post = await apiFetch(`/api/posts/${params.id}`);
  } catch (err) {
    if (err && err.status === 404) {
      return {
        html: renderLayout({
          mode: "post",
          hideHeading: true,
          content: `<section class="surface error-card"><h2>Post deleted</h2><p>This post is no longer available.</p><a class="btn btn-primary" data-link href="/">Back to feed</a></section>`,
        }),
        onMount: bindHeaderActions,
      };
    }
    throw err;
  }
  const postIDKey = String(post.id);
  const commentQuery = String(state.commentSearchByPost[postIDKey] || "").trim();
  const commentsParams = new URLSearchParams();
  if (commentQuery) commentsParams.set("q", commentQuery);
  const comments = (await apiFetch(commentsParams.toString() ? `/api/posts/${post.id}/comments?${commentsParams.toString()}` : `/api/posts/${post.id}/comments`)) || [];
  const replyTargetStillVisible = Array.isArray(comments) && comments.some((comment) => normalizeUserID(comment && comment.id) === normalizeUserID(state.pendingCommentReply && state.pendingCommentReply.commentID));
  if (getActiveCommentReply(post.id) && !replyTargetStillVisible) {
    clearPendingCommentReply();
  }
  const activeReply = getActiveCommentReply(post.id);
  const authorUsername = getAuthorUsername(post);
  const author = getAuthorDisplay(post);
  const categoriesMarkup = categoryTags(post.categories);
  const viewsCount = post.views_count ?? post.views ?? 0;
  const attachmentMarkup = renderImageAttachment(post && post.attachment, "post-detail-image", post && post.title ? `${post.title} attachment` : "Post attachment");
  const isOwner = isCurrentUserOwner(post);
  const editMode = isOwner && getSearchParam("edit") === "1";

  const content = `
    <section class="post-hero surface">
      <div class="hero-top">
        <div class="hero-crumb">POST / ${escapeHTML(String(post.id))}</div>
        <a class="btn btn-ghost btn-compact" data-link href="/" aria-label="Back">${icon("back")}</a>
      </div>
      <div class="hero-grid">
        <div class="hero-main">
          <div class="hero-main-layout">
            <div class="hero-main-content">
              <h1 class="hero-title">${escapeHTML(post.title)}</h1>
              <div class="author-line">
                ${avatarMarkup(author, post.avatarUrl, "sm")}
                <div class="author-meta">
                  <div class="author-meta-row">
                    <a class="author-name author-link" data-link href="${escapeHTML(getProfilePath(authorUsername))}">${escapeHTML(author)}</a>
                    ${renderStaffBadges(getUserBadges(post))}
                    ${
                      categoriesMarkup
                        ? `
                          <span class="meta-sep" aria-hidden="true">&bull;</span>
                          <div class="tag-row tag-row-inline">${categoriesMarkup}</div>
                        `
                        : ""
                    }
                  </div>
                  <div class="meta-line">${escapeHTML(formatDate(post.created_at))}</div>
                </div>
              </div>
              ${renderPostFlags(post)}
              <p class="hero-body">${escapeHTML(post.body)}</p>
              ${attachmentMarkup ? `<div class="post-detail-media">${attachmentMarkup}</div>` : ""}
              <div class="action-row" data-post="${post.id}">
                <button class="action-pill" type="button" data-action="like" aria-label="Like">${icon("like")} ${post.likes || 0}</button>
                <button class="action-pill" type="button" data-action="dislike" aria-label="Dislike">${icon("dislike")} ${post.dislikes || 0}</button>
                <a class="action-pill" href="#comments-section" aria-label="Comments">${icon("comment")} ${post.comments_count ?? comments.length}</a>
                ${
                  !isOwner && state.user
                    ? `<button class="action-pill" type="button" data-action="toggle-post-subscription" data-post-id="${escapeHTML(String(post.id))}" data-subscribed="${post.isSubscribed ? "1" : "0"}">${post.isSubscribed ? "Unsubscribe" : "Subscribe"}</button>`
                    : ""
                }
                ${
                  !isOwner && state.user
                    ? `<button class="action-pill" type="button" data-action="toggle-author-follow" data-username="${escapeHTML(authorUsername)}" data-following="${post.isFollowingAuthor ? "1" : "0"}">${post.isFollowingAuthor ? "Unfollow author" : "Follow author"}</button>`
                    : ""
                }
                ${isOwner ? `<a class="action-pill" data-link href="/post/${post.id}?edit=1">Edit</a>` : ""}
                ${isOwner ? `<button class="action-pill" type="button" data-action="delete-post" data-post-id="${escapeHTML(String(post.id))}">Delete</button>` : ""}
              </div>
              ${renderPostModerationActions(post)}
            </div>
            <div class="hero-main-side">
              <button class="post-side-btn post-side-menu" type="button" data-action="under-construction" aria-label="More actions">
                ${icon("menu")}
              </button>
              <div class="hero-main-side-bottom">
                <button class="post-side-pill" type="button" data-action="under-construction" aria-label="Viewed">
                  ${icon("eye")}
                  <span>${escapeHTML(formatCompactCount(viewsCount))}</span>
                </button>
                <button class="post-side-pill post-side-pill-icon" type="button" data-action="under-construction" aria-label="Share">
                  ${icon("share")}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    ${
      editMode
        ? `
          <section class="surface form-card">
            <div class="section-row">
              <h2>Edit post</h2>
              <p>Update your title, body and categories.</p>
            </div>
            <form id="post-edit-form" class="form-stack">
              <label class="field"><span>Title</span><input type="text" name="title" required value="${escapeHTML(post.title || "")}" /></label>
              <label class="field"><span>Body</span><textarea name="body" required>${escapeHTML(post.body || "")}</textarea></label>
              <div class="field">
                <span>Categories</span>
                <div class="category-grid">
                  ${(state.categories || [])
                    .map((cat) => {
                      const checked = Array.isArray(post.categories) && post.categories.some((entry) => String(entry.id) === String(cat.id));
                      return `
                        <label class="check-chip">
                          <input type="checkbox" name="categories" value="${escapeHTML(String(cat.id))}" ${checked ? "checked" : ""} />
                          <span>${escapeHTML(cat.name)}</span>
                        </label>
                      `;
                    })
                    .join("")}
                </div>
              </div>
              <div class="form-actions">
                <button class="btn btn-primary" type="submit">Save changes</button>
                <a class="btn btn-ghost btn-cancel" data-link href="/post/${post.id}">Cancel</a>
              </div>
              <div id="post-edit-error"></div>
            </form>
          </section>
        `
        : ""
    }

    ${
      commentQuery || activeReply
        ? ""
        : `
          <section class="surface form-card">
            <div class="section-row">
              <h2>Add Comment</h2>
              <p>${state.user ? "Join the discussion." : "Login to comment."}</p>
            </div>
            ${!state.user ? renderNotice("Login to create a comment.") : ""}
            ${renderCommentComposeForm({ postID: post.id })}
          </section>
        `
    }

    <section id="comments-section" class="surface comments-card">
      <div class="section-row">
        <h2>Comments</h2>
        <p>${commentQuery ? `${comments.length} found` : comments.length ? `${comments.length} total` : "No comments yet"}</p>
      </div>
      <div id="post-typing-indicator" class="typing-indicator-slot">
        ${renderTypingIndicator(getPostTypingLabel(post.id))}
      </div>
      <div class="comments-stack">
        ${comments.length ? renderCommentThreads(comments, { postID: post.id }) : renderEmpty("No comments yet", "Be the first to leave a comment.")}
      </div>
    </section>
  `;

  return {
    html: renderLayout({ mode: "post", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      syncActivePostTypingSubscription(post.id);
      syncPostTypingIndicator(post.id);
      bindPostReactions();
      bindCommentReactions();
      bindCommentOwnerActions();
      bindCommentReplyActions();
      bindCommentComposerAutosize();
      bindCommentComposeForms(post.id);
      const postEditForm = document.getElementById("post-edit-form");
      if (postEditForm) {
        postEditForm.addEventListener("submit", async (event) => {
          event.preventDefault();
          const errorBox = document.getElementById("post-edit-error");
          if (errorBox) errorBox.innerHTML = "";
          const data = new FormData(postEditForm);
          const selectedCategories = data
            .getAll("categories")
            .map((id) => Number(id))
            .filter((id) => Number.isFinite(id) && id > 0);
          try {
            await apiFetch(`/api/posts/${post.id}`, {
              method: "PUT",
              body: JSON.stringify({
                title: data.get("title"),
                body: data.get("body"),
                categories: selectedCategories,
              }),
            });
            navigate(`/post/${post.id}`);
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) errorBox.innerHTML = renderNotice(err.message || "Failed to update post.");
          }
        });
      }
      if (location.hash) {
        const target = document.querySelector(location.hash);
        if (target) {
          target.scrollIntoView({ block: "start" });
        }
      }
    },
  };
}

const routes = [
  { path: "/", view: feedView },
  { path: "/center", view: centerView },
  { path: "/login", view: loginView },
  { path: "/register", view: registerView },
  { path: "/account-link", view: accountLinkView },
  { path: "/account-merge", view: accountMergeView },
  { path: "/dm", view: dmIndexView },
  { path: "/dm/:peer", view: dmView },
  { path: "/u/:username", view: profileView },
  { path: "/new", view: newPostView },
  { path: "/post/:id", view: postView },
];

function matchRoute(pathname) {
  const current = pathname.split("/").filter(Boolean);
  for (const route of routes) {
    const routeParts = route.path.split("/").filter(Boolean);
    if (routeParts.length !== current.length) continue;
    const params = {};
    let matched = true;
    for (let i = 0; i < routeParts.length; i += 1) {
      const p = routeParts[i];
      if (p.startsWith(":")) {
        params[p.slice(1)] = current[i];
      } else if (p !== current[i]) {
        matched = false;
        break;
      }
    }
    if (matched) return { route, params };
  }
  return null;
}

async function router() {
  applyTheme(state.theme);
  handleTypingRouteTransition(location.pathname);
  const bypassProfileSetup = location.pathname === "/account-link" || location.pathname === "/account-merge";
  if (state.user && !bypassProfileSetup && maybeRedirectToProfileSetup()) return;
  const match = matchRoute(location.pathname) || { route: routes[0], params: {} };
  const activePeerID = getActiveDMPeerIDFromPath(location.pathname);
  const activePostID = getActivePostIDFromPath(location.pathname);
  if (!activePeerID) {
    state.dmPeerID = "";
    state.dmMessages = [];
    state.dmLoading = false;
    state.dmLoadingOlder = false;
    state.dmHasMore = false;
    state.dmOlderCursor = null;
    state.dmOlderLoadAt = 0;
    state.dmDraftAttachment = null;
    state.dmAttachmentUploading = false;
  }
  if (!activePostID) {
    state.editingCommentID = "";
    state.editingCommentDraft = "";
    clearPendingCommentReply();
  } else if (state.pendingCommentReply && String(state.pendingCommentReply.postID) !== String(activePostID)) {
    clearPendingCommentReply();
  }
  if (location.pathname !== "/new") {
    state.postDraftAttachment = null;
    state.postAttachmentUploading = false;
  }
  try {
    const view = await match.route.view(match.params || {});
    app.innerHTML = view.html;
    if (view.onMount) view.onMount();
    ensureRealtimeSocket();
  } catch (err) {
    app.innerHTML = renderLayout({
      mode: "feed",
      hideHeading: true,
      content: `<div class="surface error-card"><h2>UI Error</h2><p>${escapeHTML(err.message || "Unknown error")}</p><a class="btn btn-primary" data-link href="/">Back to feed</a></div>`,
    });
    bindHeaderActions();
    ensureRealtimeSocket();
  }
}

window.addEventListener("popstate", router);

document.addEventListener("click", (e) => {
  const openProfile = e.target.closest("[data-action='open-profile']");
  if (openProfile) {
    e.preventDefault();
    navigate(getProfilePath(openProfile.getAttribute("data-username")));
    return;
  }

  const dmOpen = e.target.closest("[data-action='dm-open']");
  if (dmOpen) {
    e.preventDefault();
    openDM(dmOpen.getAttribute("data-dm-open"));
    return;
  }

  const dmClose = e.target.closest("[data-action='dm-close']");
  if (dmClose) {
    e.preventDefault();
    closeDMConversation();
    return;
  }

  const dmPickAttachment = e.target.closest("[data-action='dm-pick-attachment']");
  if (dmPickAttachment) {
    e.preventDefault();
    const input = document.getElementById("dm-attachment-input");
    if (input) input.click();
    return;
  }

  const dmRemoveAttachment = e.target.closest("[data-action='dm-remove-attachment']");
  if (dmRemoveAttachment) {
    e.preventDefault();
    state.dmDraftAttachment = null;
    syncDMComposerAttachmentPreview();
    return;
  }

  const postPickAttachment = e.target.closest("[data-action='post-pick-attachment']");
  if (postPickAttachment) {
    e.preventDefault();
    const input = document.getElementById("post-attachment-input");
    if (input) input.click();
    return;
  }

  const postRemoveAttachment = e.target.closest("[data-action='post-remove-attachment']");
  if (postRemoveAttachment) {
    e.preventDefault();
    state.postDraftAttachment = null;
    syncPostAttachmentPreview();
    return;
  }

  const homeFeedNav = e.target.closest("[data-action='open-home-feed']");
  if (homeFeedNav) {
    e.preventDefault();
    state.filters.mine = false;
    state.filters.liked = false;
    state.filters.q = "";
    navigate("/");
    return;
  }

  const likedPostsNav = e.target.closest("[data-action='open-liked-posts']");
  if (likedPostsNav) {
    e.preventDefault();
    if (!state.user) {
      alert("Login or register to use this function");
      return;
    }
    state.filters.liked = true;
    state.filters.mine = false;
    navigate("/");
    return;
  }

  const createdPostsNav = e.target.closest("[data-action='open-created-posts']");
  if (createdPostsNav) {
    e.preventDefault();
    if (!state.user) {
      alert("Login or register to use this function");
      return;
    }
    state.filters.mine = true;
    state.filters.liked = false;
    navigate("/");
    return;
  }

  const toggleSubscriptionButton = e.target.closest("[data-action='toggle-post-subscription']");
  if (toggleSubscriptionButton) {
    e.preventDefault();
    if (!state.user) {
      alert("Login to manage post subscriptions.");
      return;
    }
    const postID = toggleSubscriptionButton.getAttribute("data-post-id");
    const subscribed = toggleSubscriptionButton.getAttribute("data-subscribed") === "1";
    togglePostSubscription(postID, subscribed)
      .then(() => router())
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to update post subscription.");
      });
    return;
  }

  const toggleFollowButton = e.target.closest("[data-action='toggle-author-follow']");
  if (toggleFollowButton) {
    e.preventDefault();
    if (!state.user) {
      alert("Login to follow authors.");
      return;
    }
    const username = toggleFollowButton.getAttribute("data-username");
    const following = toggleFollowButton.getAttribute("data-following") === "1";
    toggleAuthorFollow(username, following)
      .then(() => router())
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to update author follow.");
      });
    return;
  }

  const deletePostButton = e.target.closest("[data-action='delete-post']");
  if (deletePostButton) {
    e.preventDefault();
    const postID = deletePostButton.getAttribute("data-post-id");
    if (!postID) return;
    if (!confirm("Delete this post?")) return;
    deletePostByID(postID)
      .then(() => {
        if (location.pathname.startsWith("/post/")) {
          navigate("/");
          return;
        }
        router();
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to delete post.");
      });
    return;
  }

  const reportButton = e.target.closest("[data-action='open-report-modal']");
  if (reportButton) {
    e.preventDefault();
    if (!state.user) {
      alert("Login to send a report.");
      return;
    }
    const targetType = reportButton.getAttribute("data-target-type");
    const targetID = reportButton.getAttribute("data-target-id");
    if (!targetType || !targetID) return;
    submitReportFlow(targetType, targetID)
      .then((submitted) => {
        if (submitted) alert("Report submitted.");
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to submit report.");
      });
    return;
  }

  const appealButton = e.target.closest("[data-action='open-appeal-modal']");
  if (appealButton) {
    e.preventDefault();
    if (!state.user) {
      alert("Login to send an appeal.");
      return;
    }
    const targetType = appealButton.getAttribute("data-target-type");
    const targetID = appealButton.getAttribute("data-target-id");
    if (!targetType || !targetID) return;
    submitAppealFlow(targetType, targetID)
      .then((submitted) => {
        if (submitted) {
          if (location.pathname === "/center") {
            refreshCenterRouteIfOpen();
          } else {
            router();
          }
        }
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to submit appeal.");
      });
    return;
  }

  const softDeleteButton = e.target.closest("[data-action='open-soft-delete-modal']");
  if (softDeleteButton) {
    e.preventDefault();
    const targetType = softDeleteButton.getAttribute("data-target-type");
    const targetID = softDeleteButton.getAttribute("data-target-id");
    if (!targetType || !targetID) return;
    softDeleteContentFlow(targetType, targetID)
      .then((done) => {
        if (!done) return;
        if (location.pathname === "/center") {
          refreshCenterRouteIfOpen();
        } else {
          router();
        }
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to soft delete content.");
      });
    return;
  }

  const restoreButton = e.target.closest("[data-action='restore-content']");
  if (restoreButton) {
    e.preventDefault();
    const targetType = restoreButton.getAttribute("data-target-type");
    const targetID = restoreButton.getAttribute("data-target-id");
    if (!targetType || !targetID) return;
    restoreContentFlow(targetType, targetID)
      .then((done) => {
        if (!done) return;
        if (location.pathname === "/center") {
          refreshCenterRouteIfOpen();
        } else {
          router();
        }
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to restore content.");
      });
    return;
  }

  const hardDeleteButton = e.target.closest("[data-action='open-hard-delete-modal']");
  if (hardDeleteButton) {
    e.preventDefault();
    const targetType = hardDeleteButton.getAttribute("data-target-type");
    const targetID = hardDeleteButton.getAttribute("data-target-id");
    if (!targetType || !targetID) return;
    hardDeleteContentFlow(targetType, targetID)
      .then((done) => {
        if (!done) return;
        if (location.pathname === "/center") {
          refreshCenterRouteIfOpen();
        } else if (targetType === "post" && location.pathname.startsWith("/post/")) {
          navigate("/");
        } else {
          router();
        }
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to hard delete content.");
      });
    return;
  }

  const protectionButton = e.target.closest("[data-action='toggle-delete-protection']");
  if (protectionButton) {
    e.preventDefault();
    const postID = protectionButton.getAttribute("data-post-id");
    const nextProtected = protectionButton.getAttribute("data-next-protected") === "1";
    if (!postID) return;
    toggleDeleteProtectionFlow(postID, nextProtected)
      .then((done) => {
        if (!done) return;
        router();
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to update delete protection.");
      });
    return;
  }

  const requestRoleButton = e.target.closest("[data-action='request-role']");
  if (requestRoleButton) {
    e.preventDefault();
    const requestedRole = requestRoleButton.getAttribute("data-requested-role");
    if (!requestedRole) return;
    requestRoleFlow(requestedRole)
      .then((done) => {
        if (done) alert("Role request sent.");
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to send role request.");
      });
    return;
  }

  const directRoleChangeButton = e.target.closest("[data-action='change-user-role']");
  if (directRoleChangeButton && location.pathname !== "/center") {
    e.preventDefault();
    const userID = directRoleChangeButton.getAttribute("data-user-id");
    const nextRole = directRoleChangeButton.getAttribute("data-next-role");
    if (!userID || !nextRole) return;
    changeUserRoleByID(userID, nextRole).catch((err) => {
      if (err && err.handled) return;
      alert(err.message || "Failed to change role.");
    });
    return;
  }

  const approvePostButton = e.target.closest("[data-action='queue-approve-post']");
  if (approvePostButton && location.pathname !== "/center") {
    e.preventDefault();
    const postID = approvePostButton.getAttribute("data-post-id");
    if (!postID) return;
    approveQueuedPost(postID)
      .then(() => router())
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to approve post.");
      });
    return;
  }

  const editCategoriesButton = e.target.closest("[data-action='edit-post-categories']");
  if (editCategoriesButton) {
    e.preventDefault();
    const postID = editCategoriesButton.getAttribute("data-post-id");
    const routePostID = getActivePostIDFromPath();
    if (!postID || !routePostID || normalizeUserID(postID) !== normalizeUserID(routePostID)) return;
    apiFetch(`/api/posts/${encodeURIComponent(routePostID)}`)
      .then((post) => updatePostCategoriesFlow(post))
      .then((done) => {
        if (!done) return;
        router();
      })
      .catch((err) => {
        if (err && err.handled) return;
        alert(err.message || "Failed to update categories.");
      });
    return;
  }

  const notificationsButton = e.target.closest("[data-action='open-notifications']");
  if (notificationsButton) {
    e.preventDefault();
    openNotifications();
    return;
  }

  const stub = e.target.closest("[data-action='under-construction']");
  if (stub) {
    e.preventDefault();
    alert("Function under construction");
    return;
  }

  const link = e.target.closest("[data-link]");
  if (!link) return;
  const href = link.getAttribute("href");
  if (!href || href.startsWith("http")) return;
  e.preventDefault();
  navigate(href);
});

document.addEventListener("submit", (e) => {
  const form = e.target.closest("#dm-form");
  if (!form) return;

  e.preventDefault();
  const data = new FormData(form);
  const body = String(data.get("body") || "").trim();
  const errorBox = document.getElementById("dm-error");
  const attachment = state.dmDraftAttachment;

  if (errorBox) errorBox.innerHTML = "";
  if (state.dmAttachmentUploading) {
    if (errorBox) errorBox.innerHTML = renderNotice("Image is still uploading.");
    return;
  }
  if (!body && !attachment) {
    if (errorBox) errorBox.innerHTML = renderNotice("Message or image is required.");
    return;
  }

  try {
    stopLocalTyping(TYPING_SCOPE_DM, { send: true });
    sendDMMessage(body, attachment);
    form.reset();
  } catch (err) {
    if (errorBox) {
      errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to send message.");
      return;
    }
    alert(err && err.message ? err.message : "Failed to send message.");
  }
});

document.addEventListener("change", async (e) => {
  const dmInput = e.target.closest("#dm-attachment-input");
  if (dmInput) {
    const file = dmInput.files && dmInput.files[0];
    const errorBox = document.getElementById("dm-error");
    if (errorBox) errorBox.innerHTML = "";
    if (!file) return;

    const validationMessage = validateAttachmentFile(file);
    if (validationMessage) {
      state.dmDraftAttachment = null;
      syncDMComposerAttachmentPreview();
      if (errorBox) {
        errorBox.innerHTML = renderNotice(validationMessage);
      }
      dmInput.value = "";
      return;
    }

    state.dmAttachmentUploading = true;
    syncDMView({ scrollMode: "preserve" });
    try {
      state.dmDraftAttachment = await uploadImageAttachment(file);
      syncDMComposerAttachmentPreview();
    } catch (err) {
      state.dmDraftAttachment = null;
      if (errorBox) {
        errorBox.innerHTML = renderNotice(getAttachmentErrorMessage(err));
      }
    } finally {
      dmInput.value = "";
      state.dmAttachmentUploading = false;
      syncDMView({ scrollMode: "preserve" });
    }
    return;
  }

  const postInput = e.target.closest("#post-attachment-input");
  if (!postInput) return;

  const file = postInput.files && postInput.files[0];
  const errorBox = document.getElementById("post-error");
  if (errorBox) errorBox.innerHTML = "";
  if (!file) return;

  const validationMessage = validateAttachmentFile(file);
  if (validationMessage) {
    state.postDraftAttachment = null;
    syncPostAttachmentPreview();
    if (errorBox) {
      errorBox.innerHTML = renderNotice(validationMessage);
    }
    postInput.value = "";
    return;
  }

  state.postAttachmentUploading = true;
  syncPostAttachmentControls();
  syncPostAttachmentPreview();
  try {
    state.postDraftAttachment = await uploadImageAttachment(file);
  } catch (err) {
    state.postDraftAttachment = null;
    if (errorBox) {
      errorBox.innerHTML = renderNotice(getAttachmentErrorMessage(err));
    }
  } finally {
    postInput.value = "";
    state.postAttachmentUploading = false;
    syncPostAttachmentControls();
    syncPostAttachmentPreview();
  }
});

document.addEventListener("input", (e) => {
  const target = e.target;
  if (!(target instanceof HTMLElement) || !state.user) return;

  const dmTextarea = target.closest("#dm-form textarea[name='body']");
  if (dmTextarea) {
    const peerID = normalizeUserID(state.dmPeerID);
    if (peerID) startLocalTyping(TYPING_SCOPE_DM, peerID);
    return;
  }

  const commentTextarea = target.closest("[data-comment-compose-form] textarea[name='body']");
  if (!commentTextarea) return;

  if (state.pendingCommentReply && target instanceof HTMLTextAreaElement && target.closest("[data-inline-reply-form]")) {
    state.pendingCommentReply.draft = target.value;
  }

  const postID = normalizeUserID(getActivePostIDFromPath() || "");
  if (!postID) return;
  startLocalTyping(TYPING_SCOPE_POST, postID);
});

document.addEventListener("focusout", (e) => {
  const target = e.target;
  if (!(target instanceof HTMLElement) || !state.user) return;

  if (target.closest("#dm-form textarea[name='body']")) {
    stopLocalTyping(TYPING_SCOPE_DM, { send: true });
    return;
  }

  if (target.closest("[data-comment-compose-form] textarea[name='body']")) {
    stopLocalTyping(TYPING_SCOPE_POST, { send: true });
  }
});

(async () => {
  await ensureUser(true);
  if (maybeRedirectToProfileSetup()) return;
  ensureRealtimeSocket();
  router();
})();
