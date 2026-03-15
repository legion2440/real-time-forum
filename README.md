## forum (authentication)

Учебный веб-форум на Go + SQLite с SPA (1 HTML) на чистом JavaScript, без фронтенд-фреймворков и без CDN.
Проект собран по заданию "real-time-forum": личные сообщения + real-time (WebSocket) поверх базового форума (посты/комменты). 

Важно: в go.mod модуль называется `forum` (исторически).

## Что реализовано по заданию

### Registration and Login
- Регистрация и логин обязательны для работы с форумом.
- Логин: 1 поле "e-mail or username" + password (можно войти по нику или e-mail).
- Logout доступен с любой страницы.

Примечание:
- Поля профиля (first name / last name / age / gender) сделаны необязательными на первичной регистрации.
  После первого логина пользователь попадает на страницу профиля (profile setup) и может заполнить их там.

### Authentication Methods
- `local`: email or username + password
- `google`: OAuth 2.0 Authorization Code Flow
- `github`: OAuth 2.0 Authorization Code Flow
- `facebook`: OAuth 2.0 Authorization Code Flow

OAuth is implemented through a provider registry (`internal/oauth`) and generic handlers:
- `GET /auth/{provider}/login`
- `GET /auth/{provider}/callback`

The architecture is intentionally extensible: adding a new provider such as Steam, Twitch, or My.Games only requires a new provider adapter that implements the shared interface and registration in the provider registry.

### Linked Accounts And Merge Behavior
- Local login/register remains active and is not replaced by OAuth.
- Profile page includes `Linked accounts` with provider status: `linked` / `not linked`.
- A logged-in user can explicitly link a provider from profile.
- Unlink is blocked if it would leave the account without any valid sign-in method.
- Matching email from OAuth does not auto-link or auto-merge a logged-out user.
- If OAuth returns an email already used by an existing forum account, the app creates an explicit confirmation flow.
- If two existing accounts need to be combined, the app creates an explicit merge flow.
- Canonical account is selected by `created_at` (older account wins by default).
- Default display name during merge also comes from the older account and must be confirmed explicitly.
- Merge is transactional and moves linked identities plus current user-owned forum data to the canonical account.
- If merge would be unsafe for a known edge case, it is rejected explicitly instead of partially continuing.

### Posts and Comments
- Посты с категориями.
- Комментарии к постам.
- Лента постов, комментарии видны после открытия поста.
- Typing in progress для комментариев: пользователи, открывшие один и тот же пост, видят в real time, что кто-то печатает комментарий.

### Private Messages (DM) + real-time
- Список пользователей для чата: online/offline, видим всегда.
- Сортировка как в Discord: по последнему сообщению, если сообщений нет - по алфавиту.
- История сообщений:
  - При открытии диалога грузятся последние 10 сообщений.
  - Пангинация: при скролле вверх догружается еще по 10, с throttle (без спама scroll event).
- Формат сообщения: дата отправки + имя пользователя.
- Real-time доставка сообщений через WebSocket без refresh.
- Typing in progress для DM: собеседник видит в real time, что пользователь печатает сообщение.

## Bonus (сверх задания)
- Профили пользователей (display name + необязательные поля first/last/age/gender).
- Unread для DM: серверный "true" + локальный cache (localStorage) как быстрый fallback.
- Загрузка изображений (JPEG/PNG/GIF, до 20MB) для постов и личных сообщений.
- Кастомная 404 страница.
- Реакции like/dislike на посты и комментарии.

## Allowed packages
- Только стандартные пакеты Go +:
  - github.com/gorilla/websocket
  - github.com/mattn/go-sqlite3
  - golang.org/x/crypto/bcrypt
  - github.com/google/uuid

Frontend: без React/Angular/Vue и любых библиотек/фреймворков.

## Быстрый старт

### Запуск локально

#### Windows (Git Bash / MINGW64 или MSYS2 UCRT64)
Важно: `github.com/mattn/go-sqlite3` требует CGO и C-компилятор (gcc).

```bash
export CGO_ENABLED=1
go run ./cmd/server
```

Открыть:
- http://127.0.0.1:8080/

#### Linux / WSL / macOS
```bash
export CGO_ENABLED=1
go run ./cmd/server
```

### Переменные окружения
- `FORUM_DB_PATH` - путь к SQLite файлу (по умолчанию `forum.db`).
- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`
- `GOOGLE_REDIRECT_URL`
- `GITHUB_CLIENT_ID`
- `GITHUB_CLIENT_SECRET`
- `GITHUB_REDIRECT_URL`
- `FACEBOOK_CLIENT_ID`
- `FACEBOOK_CLIENT_SECRET`
- `FACEBOOK_REDIRECT_URL`
- `DEBUG_500`

Пример:
```bash
export FORUM_DB_PATH=./forum.db
```

OAuth providers are optional. If provider env vars are missing, local auth still works and the missing provider is simply not exposed in the UI.

### Local OAuth Setup
1. Create OAuth apps in Google, GitHub, and/or Facebook developer consoles.
2. Configure each provider callback URL to point to this app:
   - Google: `http://127.0.0.1:8080/auth/google/callback`
   - GitHub: `http://127.0.0.1:8080/auth/github/callback`
   - Facebook: `http://127.0.0.1:8080/auth/facebook/callback`
3. Export matching env vars before `go run ./cmd/server`.
4. Start the server and use `Login` / `Register` page buttons:
   - `Continue with Google`
   - `Continue with GitHub`
   - `Continue with Facebook`

Example:
```bash
export FORUM_DB_PATH=./forum.db
export GOOGLE_CLIENT_ID=your-google-client-id
export GOOGLE_CLIENT_SECRET=your-google-client-secret
export GOOGLE_REDIRECT_URL=http://127.0.0.1:8080/auth/google/callback

export GITHUB_CLIENT_ID=your-github-client-id
export GITHUB_CLIENT_SECRET=your-github-client-secret
export GITHUB_REDIRECT_URL=http://127.0.0.1:8080/auth/github/callback

export FACEBOOK_CLIENT_ID=your-facebook-client-id
export FACEBOOK_CLIENT_SECRET=your-facebook-client-secret
export FACEBOOK_REDIRECT_URL=http://127.0.0.1:8080/auth/facebook/callback
```

### Docker

#### Bash (Linux/macOS/Git Bash)
```bash
bash ./scripts/docker-run.sh
# другой порт
HOST_PORT=8081 bash ./scripts/docker-run.sh
```

#### PowerShell (Windows)
```powershell
.\scripts\docker-run.ps1
# если ExecutionPolicy мешает:
powershell -ExecutionPolicy Bypass -File .\scripts\docker-run.ps1
# другой порт:
.\scripts\docker-run.ps1 -HostPort 8081
```

## Тестирование
```bash
go test ./...
go vet ./...
go test -race -count=1 ./...
```

## База данных (SQLite)
- Схема встроена в бинарь (embed `internal/repo/sqlite/schema.sql`) и применяется при старте.
- База по умолчанию: `forum.db`.
- Категории сидятся автоматически при первом запуске.

### Посмотреть данные через sqlite3
```bash
sqlite3 forum.db
.tables
SELECT id, username, email FROM users;
SELECT id, user_id, title FROM posts;
SELECT id, post_id, user_id, body FROM comments;
.quit
```

## Статусы и ошибки (HTTP)
- UI deep-links должны отдавать SPA (200):
  - `/`, `/login`, `/register`, `/new`, `/post/1`, `/dm/1`, `/u/demo`
- API 404:
  - `GET /api/does-not-exist` -> 404
- Asset 404:
  - `GET /assets/nope.svg` -> 404
- 400:
  - пустой логин/пустой пост/пустой коммент -> 400
- 401:
  - гость пытается создать пост/коммент/открыть DM -> 401
- 405:
  - неверный HTTP method -> 405
- 500:
  - `/api/debug/500` при `DEBUG_500=1`

## Debug: проверить HTTP 500 (dev-only)
В проекте есть dev-only endpoint для демонстрации 500 и проверки panic recovery.

Запуск:
```bash
export DEBUG_500=1
go run ./cmd/server
```

Проверка:
```bash
curl -i http://127.0.0.1:8080/api/debug/500
```

Ожидаемо: `HTTP/1.1 500 Internal Server Error` и JSON-ошибка.

Если `DEBUG_500` не задан, endpoint ведет себя как отсутствующий (404).

## Структура проекта

```text
real-time-forum/
├─ cmd/server/                 # точка входа (main) для HTTP-сервера
│  └─ main.go                  # bootstrap: зависимости, запуск HTTP + WS
├─ internal/                   # приватный код приложения
│  ├─ domain/                  # доменные сущности/типы
│  ├─ http/                    # HTTP-слой: router, handlers, middleware, cookies, responses/errors
│  ├─ platform/                # утилиты: id/uuid, clock/time
│  ├─ realtime/ws/             # WS hub + события presence/pm/typing, подписки на post view
│  ├─ repo/                    # слой доступа к данным (интерфейсы + реализации)
│  │  └─ sqlite/               # SQLite: schema.sql, миграции/legacy-safe апдейты, запросы, тесты
│  └─ service/                 # use cases: auth, posts, private messages, attachments
├─ scripts/                    # скрипты для docker/audit
│  ├─ audit_smoke.sh
│  ├─ docker-run.ps1
│  └─ docker-run.sh
├─ var/uploads/                # runtime-хранилище загруженных файлов (локально/в контейнере)
├─ web/                        # фронт/статика (SPA)
│  ├─ assets/
│  ├─ app.js
│  ├─ index.html
│  ├─ 404.html                 # кастомная 404 страница
│  └─ styles.css
├─ Dockerfile
├─ go.mod
└─ README.md
```

## Оглавление (TOC)
- [real-time-forum](#real-time-forum)
- [Что реализовано по заданию](#что-реализовано-по-заданию)
  - [Registration and Login](#registration-and-login)
  - [Posts and Comments](#posts-and-comments)
  - [Private Messages (DM) + real-time](#private-messages-dm--real-time)
- [Bonus (сверх задания)](#bonus-сверх-задания)
- [Allowed packages](#allowed-packages)
- [Быстрый старт](#быстрый-старт)
  - [Запуск локально](#запуск-локально)
  - [Переменные окружения](#переменные-окружения)
  - [Docker](#docker)
- [Тестирование](#тестирование)
- [База данных (SQLite)](#база-данных-sqlite)
- [Статусы и ошибки (HTTP)](#статусы-и-ошибки-http)
- [Debug: проверить HTTP 500 (dev-only)](#debug-проверить-http-500-dev-only)
- [Структура проекта](#структура-проекта)
- [Оглавление (TOC)](#оглавление-toc)

## Авторы
- Nazar Yestayev (@nyestaye / @legion2440)
- Dastan Gabbassov (@dgabbass)
- Nurgul Ilyassova (@nilyasso)
- Davyd Semenov (@mmujajov)
- Yernazar Uxumbayev (@yuxumbay)