const app = document.getElementById("app");
const THEME_KEY = "theme";
const DEBUG_WS = false;
const DM_HISTORY_PAGE_SIZE = 10;
const DM_HISTORY_THROTTLE_MS = 400;

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
  dmReturnPath: "",
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
  theme: getInitialTheme(),
};

let realtimeSocket = null;

applyTheme(state.theme);

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

function getProfileAgeValue(profile) {
  const age = Number(profile && profile.age);
  if (!Number.isFinite(age) || age <= 0) return "";
  return String(age);
}

function getProfileFieldValue(profile, key) {
  return String((profile && profile[key]) || "").trim();
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
  state.dmReturnPath = "";
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
  state.dmReturnPath = "";
  if (!preserveUnread) {
    state.dmUnreadByPeer = {};
  }
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
  state.user = null;
  state.filters.mine = false;
  state.filters.liked = false;
  clearDMState();
  clearPresenceState();
  closeRealtimeSocket();
}

function closeRealtimeSocket() {
  if (!realtimeSocket) return;
  const socket = realtimeSocket;
  realtimeSocket = null;
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
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
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
        <button class="icon-btn icon-btn-plain notification-btn" type="button" data-action="under-construction" aria-label="Notifications">${icon("bell")}</button>
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

function normalizeDMPeer(peer) {
  if (!peer || typeof peer !== "object") return null;
  const id = normalizeUserID(peer.id);
  const username = normalizeUsername(peer.username);
  const displayName = String(peer.displayName || peer.display_name || "").trim();
  const lastMessageAt = Number(peer.lastMessageAt ?? peer.last_message_at ?? 0);
  if (!id || !username) return null;
  return {
    id,
    username,
    displayName,
    lastMessageAt: Number.isFinite(lastMessageAt) && lastMessageAt > 0 ? lastMessageAt : 0,
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
      const unread = Number(state.dmUnreadByPeer[id] || 0);
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
              </div>
              ${unread > 0 ? `<span class="dm-peer-badge">${escapeHTML(String(unread))}</span>` : ""}
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
    <form id="dm-form" class="form-stack">
      <label class="field">
        <span>Message</span>
        <textarea name="body" rows="3" required ${state.dmLoading ? "disabled" : ""}></textarea>
      </label>
      <div class="form-actions">
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
  return `
    <div class="dm-msg ${directionClass}">
      <div class="dm-msg-body">
        <div class="dm-meta">${escapeHTML(fromName)} · ${escapeHTML(formatDate(message.createdAt))}</div>
        <div class="dm-bubble">${escapeHTML(message.body)}</div>
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
  if (!id || !fromID || !toID || !body || !createdAt) return null;

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
}

function syncDMPeersPanel() {
  const panel = document.getElementById("dm-peers-panel");
  if (!panel) return;
  panel.innerHTML = renderDMPeersContent();
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
      const unread = Number(state.dmUnreadByPeer[id] || 0);
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
                >DM${unread > 0 ? ` (${unread})` : ""}</button>
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

function renderPostCard(post) {
  const authorUsername = getAuthorUsername(post);
  const author = getAuthorDisplay(post);
  const categoriesMarkup = categoryTags(post.categories);
  const viewsCount = post.views_count ?? post.views ?? 0;
  return `
    <article class="surface post-card">
      <div class="post-card-main">
        <h3 class="post-card-title"><a data-link href="/post/${post.id}">${escapeHTML(post.title)}</a></h3>
        <div class="author-line">
          ${avatarMarkup(author, post.avatarUrl, "sm")}
          <div class="author-meta">
            <div class="author-meta-row">
              <a class="author-name author-link" data-link href="${escapeHTML(getProfilePath(authorUsername))}">${escapeHTML(author)}</a>
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
        <p class="post-card-body">${escapeHTML(post.body)}</p>
        <div class="action-row" data-post="${post.id}">
          <button class="action-pill" type="button" data-action="like" aria-label="Like">${icon("like")} ${post.likes || 0}</button>
          <button class="action-pill" type="button" data-action="dislike" aria-label="Dislike">${icon("dislike")} ${post.dislikes || 0}</button>
          <a class="action-pill" data-link href="/post/${post.id}" aria-label="Comments">${icon("comment")} ${post.comments_count ?? (Array.isArray(post.comments) ? post.comments.length : 0)}</a>
        </div>
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

function bindHeaderActions() {
  document.querySelectorAll("[data-action='toggle-theme']").forEach((el) => {
    el.addEventListener("click", toggleTheme);
  });

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
  const form = document.getElementById("comment-form");
  const parentInput = form ? form.querySelector("input[name='parent_id']") : null;
  const replyBox = document.getElementById("reply-target-box");
  const replyText = document.getElementById("reply-target-text");
  const textarea = form ? form.querySelector("textarea[name='body']") : null;

  const clearReply = () => {
    if (parentInput) parentInput.value = "";
    if (replyBox) replyBox.hidden = true;
    if (replyText) replyText.textContent = "";
  };

  const openReply = (commentID, authorLabel) => {
    if (!parentInput) return false;
    const id = String(commentID || "").trim();
    if (!id) return false;
    parentInput.value = id;
    if (replyText) replyText.textContent = `Replying to ${authorLabel || "comment"}`;
    if (replyBox) replyBox.hidden = false;
    textarea?.focus();
    return true;
  };

  const cancelBtn = document.getElementById("cancel-reply-btn");
  if (cancelBtn) {
    cancelBtn.addEventListener("click", () => {
      clearReply();
      textarea?.focus();
    });
  }

  document.querySelectorAll("[data-action='reply-comment']").forEach((btn) => {
    btn.addEventListener("click", () => {
      if (!state.user) {
        alert("Login to reply.");
        return;
      }
      const id = btn.getAttribute("data-reply-comment-id");
      const author = btn.getAttribute("data-reply-author") || "comment";
      if (!id) return;

      if (openReply(id, author)) return;

      // If comment search is active, Add Comment is hidden. Clear search and
      // reopen reply mode for the clicked comment after rerender.
      const activePostID = getActivePostIDFromPath();
      if (!activePostID) return;
      const postKey = String(activePostID);
      const activeCommentQuery = String(state.commentSearchByPost[postKey] || "").trim();
      if (!activeCommentQuery) return;

      state.pendingCommentReply = {
        postID: postKey,
        commentID: String(id),
        author: String(author || "comment"),
      };
      state.commentSearchByPost[postKey] = "";
      router();
    });
  });

  const activePostID = getActivePostIDFromPath();
  const pending = state.pendingCommentReply;
  if (pending && activePostID && String(pending.postID) === String(activePostID)) {
    if (openReply(pending.commentID, pending.author)) {
      state.pendingCommentReply = null;
    }
  }

  return { clearReply, openReply };
}

function bindCommentComposerAutosize() {
  const textarea = document.querySelector("#comment-form textarea[name='body']");
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
}

async function ensureUser(force = false) {
  if (state.user && !force) {
    if (!state.usersLoaded) {
      await ensureUsersLoaded();
    }
    ensureRealtimeSocket();
    return;
  }

  const previousUserID = normalizeUserID(state.user && state.user.id);
  try {
    state.user = await apiFetch("/api/me");
  } catch (_) {
    clearAuthenticatedState();
    return;
  }

  const currentUserID = normalizeUserID(state.user && state.user.id);
  if (force || !state.usersLoaded || previousUserID !== currentUserID) {
    await ensureUsersLoaded(force || previousUserID !== currentUserID);
  }
  if (force) closeRealtimeSocket();
  ensureRealtimeSocket();
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
          .map((user) => ({
            id: normalizeUserID(user && user.id),
            username: normalizeUsername(user && user.username),
            name: String(user && user.name ? user.name : "").trim(),
          }))
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
    state.dmPeers = Array.isArray(peers) ? sortDMPeers(peers.map(normalizeDMPeer).filter(Boolean)) : [];
    state.dmPeersLoaded = true;
  } catch (_) {
    state.dmPeers = [];
    state.dmPeersLoaded = false;
  }

  syncDMPeersPanel();
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

  state.dmUnreadByPeer[peer] = 0;
  if (!isDMRoute()) {
    state.dmReturnPath = `${location.pathname || "/"}${location.search || ""}`;
  }
  syncPresencePanel();
  syncDMPeersPanel();
  navigate(`/dm/${peer}`);
}

function closeDMConversation() {
  const target = state.dmReturnPath || "/dm";
  clearDMConversationState(true);
  syncPresencePanel();
  navigate(target);
}

async function loadPublicProfile(username) {
  return apiFetch(`/api/u/${encodeURIComponent(normalizeUsername(username))}`);
}

function sendDMMessage(body) {
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
    })
  );
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

  if (payload.type === "pm:new") {
    const message = normalizeDMMessage(payload.message);
    if (!message) return;
    updateDMPeerActivity(message);

    if (isMessageForPeer(message, state.dmPeerID)) {
      const thread = document.getElementById("dm-thread");
      const shouldStickBottom = isDMThreadNearBottom(thread);
      appendDMMessage(message);
      if (normalizeUserID(message.from && message.from.id) === state.dmPeerID) {
        state.dmUnreadByPeer[state.dmPeerID] = 0;
      }
      syncDMView({ scrollMode: shouldStickBottom ? "bottom" : "preserve" });
      syncPresencePanel();
      syncDMPeersPanel();
      return;
    }

    if (normalizeUserID(message.to && message.to.id) === getCurrentUserID()) {
      const peerID = normalizeUserID(message.from && message.from.id);
      if (peerID && peerID !== getCurrentUserID()) {
        state.dmUnreadByPeer[peerID] = Number(state.dmUnreadByPeer[peerID] || 0) + 1;
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
      };
    })
  );

  if (changed) {
    syncDMPeersPanel();
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
  if (state.dmPeerID) {
    state.dmUnreadByPeer[state.dmPeerID] = 0;
    try {
      await loadDMConversation(state.dmPeerID, DM_HISTORY_PAGE_SIZE);
    } catch (err) {
      if (!err || err.status !== 404) throw err;
      state.dmPeerID = "";
      state.dmMessages = [];
      state.dmLoading = false;
      state.dmLoadingOlder = false;
      state.dmHasMore = false;
      state.dmOlderCursor = null;
      state.dmOlderLoadAt = 0;
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
  const selfDisplayName = isSelf ? String(state.user && state.user.displayName ? state.user.displayName : "").trim() : String(profile && profile.displayName ? profile.displayName : "").trim();
  const selfFirstName = getProfileFieldValue(editableProfile, "firstName");
  const selfLastName = getProfileFieldValue(editableProfile, "lastName");
  const selfAge = getProfileAgeValue(editableProfile);
  const selfGender = getProfileFieldValue(editableProfile, "gender");

  const content = `
    <section class="page-head">
      <div>
        <h1>${escapeHTML(heading)}</h1>
        <p class="profile-handle">${escapeHTML(subtitle)}</p>
      </div>
    </section>
    <section class="surface form-card profile-card">
      <div class="section-row">
        <h2>${isSelf ? "Your profile" : "Profile"}</h2>
        <p>${isSelf ? "Display name is optional. Username stays unchanged." : "Public profile."}</p>
      </div>
      ${setupMode ? renderNotice("Complete your profile setup now or skip. You can update these fields later from your profile.") : ""}
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
              ${renderProfileField("First name", getProfileFieldValue(profile, "firstName"))}
              ${renderProfileField("Last name", getProfileFieldValue(profile, "lastName"))}
              ${renderProfileField("Age", getProfileAgeValue(profile))}
              ${renderProfileField("Gender", getProfileFieldValue(profile, "gender"))}
            </div>
          `
      }
    </section>
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
            navigate("/");
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
            navigate("/");
          } catch (err) {
            if (err && err.handled) return;
            if (errorBox) {
              errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to update profile.");
            }
          }
        });
      }
    },
  };
}

function renderAuthLayout(kind) {
  const isLogin = kind === "login";
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
          <div id="${kind}-error"></div>
        </form>
      </div>
    </section>
  `;
  return renderLayout({ mode: kind, hideHeading: true, content });
}

async function loginView() {
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
          if (!maybeRedirectToProfileSetup()) {
            navigate("/");
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
          if (!maybeRedirectToProfileSetup()) {
            navigate("/");
          }
        } catch (err) {
          if (err && err.handled) return;
          document.getElementById("register-error").innerHTML = renderNotice(err.message);
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
        <div class="field">
          <span>Categories</span>
          <div class="category-grid">
            ${categoryChoices || '<div class="side-note">No categories</div>'}
          </div>
        </div>
        <div class="form-actions">
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
      const form = document.getElementById("post-form");
      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        if (!state.user) return;
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
            }),
          });
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

function renderComment(comment, { isReply = false } = {}) {
  const authorUsername = getAuthorUsername(comment);
  const author = getAuthorDisplay(comment);
  return `
    <article class="surface comment-card${isReply ? " is-reply" : ""}">
      <div class="author-line">
        ${avatarMarkup(author, comment.avatarUrl, "xs")}
        <div>
          <a class="author-name author-link" data-link href="${escapeHTML(getProfilePath(authorUsername))}">${escapeHTML(author)}</a>
          <div class="meta-line">${escapeHTML(formatDate(comment.created_at))}</div>
        </div>
      </div>
      <p class="comment-text">${escapeHTML(comment.body)}</p>
      <div class="action-row" data-comment="${comment.id}">
        <button class="action-pill" type="button" data-action="like">${icon("like")} ${comment.likes || 0}</button>
        <button class="action-pill" type="button" data-action="dislike">${icon("dislike")} ${comment.dislikes || 0}</button>
        <button class="action-pill action-pill-reply" type="button" data-action="reply-comment" data-reply-comment-id="${comment.id}" data-reply-author="${escapeHTML(author)}">Reply</button>
      </div>
    </article>
  `;
}

function renderCommentThreads(comments) {
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
          ${renderComment(comment)}
          ${
            replies.length
              ? `
                <div class="comment-thread-children">
                  ${replies.map((reply) => renderComment(reply, { isReply: true })).join("")}
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
  const post = await apiFetch(`/api/posts/${params.id}`);
  const postIDKey = String(post.id);
  const commentQuery = String(state.commentSearchByPost[postIDKey] || "").trim();
  const commentsParams = new URLSearchParams();
  if (commentQuery) commentsParams.set("q", commentQuery);
  const comments = (await apiFetch(commentsParams.toString() ? `/api/posts/${post.id}/comments?${commentsParams.toString()}` : `/api/posts/${post.id}/comments`)) || [];
  const authorUsername = getAuthorUsername(post);
  const author = getAuthorDisplay(post);
  const categoriesMarkup = categoryTags(post.categories);
  const viewsCount = post.views_count ?? post.views ?? 0;

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
              <p class="hero-body">${escapeHTML(post.body)}</p>
              <div class="action-row" data-post="${post.id}">
                <button class="action-pill" type="button" data-action="like" aria-label="Like">${icon("like")} ${post.likes || 0}</button>
                <button class="action-pill" type="button" data-action="dislike" aria-label="Dislike">${icon("dislike")} ${post.dislikes || 0}</button>
                <a class="action-pill" href="#comments-section" aria-label="Comments">${icon("comment")} ${post.comments_count ?? comments.length}</a>
              </div>
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
      commentQuery
        ? ""
        : `
          <section class="surface form-card">
            <div class="section-row">
              <h2>Add Comment</h2>
              <p>${state.user ? "Join the discussion." : "Login to comment."}</p>
            </div>
            ${!state.user ? renderNotice("Login to create a comment.") : ""}
            <form id="comment-form" class="form-stack">
              <div id="reply-target-box" class="reply-target-box" hidden>
                <span id="reply-target-text"></span>
                <button id="cancel-reply-btn" class="reply-target-cancel" type="button" aria-label="Cancel reply">Cancel</button>
              </div>
              <input type="hidden" name="parent_id" value="" />
              <label class="field"><span>Comment</span><textarea class="comment-compose-textarea" name="body" required ${state.user ? "" : "disabled"}></textarea></label>
              <div class="form-actions">
                <button class="btn btn-primary" type="submit" ${state.user ? "" : "disabled"}>${icon("send")} Post Comment</button>
              </div>
              <div id="comment-error"></div>
            </form>
          </section>
        `
    }

    <section id="comments-section" class="surface comments-card">
      <div class="section-row">
        <h2>Comments</h2>
        <p>${commentQuery ? `${comments.length} found` : comments.length ? `${comments.length} total` : "No comments yet"}</p>
      </div>
      <div class="comments-stack">
        ${comments.length ? renderCommentThreads(comments) : renderEmpty("No comments yet", "Be the first to leave a comment.")}
      </div>
    </section>
  `;

  return {
    html: renderLayout({ mode: "post", hideHeading: true, content }),
    onMount: () => {
      bindHeaderActions();
      bindPostReactions();
      bindCommentReactions();
      bindCommentComposerAutosize();
      const replyUI = bindCommentReplyActions();
      const form = document.getElementById("comment-form");
      if (form) {
        form.addEventListener("submit", async (e) => {
          e.preventDefault();
          if (!state.user) return;
          const data = new FormData(form);
          const parentRaw = String(data.get("parent_id") || "").trim();
          const parentID = parentRaw ? Number(parentRaw) : null;
          try {
            await apiFetch(`/api/posts/${post.id}/comments`, {
              method: "POST",
              body: JSON.stringify({
                body: data.get("body"),
                ...(Number.isFinite(parentID) && parentID > 0 ? { parent_id: parentID } : {}),
              }),
            });
            if (replyUI && replyUI.clearReply) replyUI.clearReply();
            router();
          } catch (err) {
            if (err && err.handled) return;
            document.getElementById("comment-error").innerHTML = renderNotice(err.message);
          }
        });
      }
    },
  };
}

const routes = [
  { path: "/", view: feedView },
  { path: "/login", view: loginView },
  { path: "/register", view: registerView },
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
  if (state.user && maybeRedirectToProfileSetup()) return;
  const match = matchRoute(location.pathname) || { route: routes[0], params: {} };
  const activePeerID = getActiveDMPeerIDFromPath(location.pathname);
  if (!activePeerID) {
    state.dmPeerID = "";
    state.dmMessages = [];
    state.dmLoading = false;
    state.dmLoadingOlder = false;
    state.dmHasMore = false;
    state.dmOlderCursor = null;
    state.dmOlderLoadAt = 0;
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

  if (errorBox) errorBox.innerHTML = "";
  if (!body) {
    if (errorBox) errorBox.innerHTML = renderNotice("Message is required.");
    return;
  }

  try {
    sendDMMessage(body);
    form.reset();
  } catch (err) {
    if (errorBox) {
      errorBox.innerHTML = renderNotice(err && err.message ? err.message : "Failed to send message.");
      return;
    }
    alert(err && err.message ? err.message : "Failed to send message.");
  }
});

(async () => {
  await ensureUser(true);
  if (maybeRedirectToProfileSetup()) return;
  ensureRealtimeSocket();
  router();
})();

