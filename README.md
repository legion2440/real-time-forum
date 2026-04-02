# forum (security)

Учебный веб-форум на Go + SQLite с SPA (1 HTML) на чистом JavaScript, без фронтенд-фреймворков и без CDN.  
Проект собран по заданию "real-time-forum": личные сообщения + real-time (WebSocket) поверх базового форума (посты/комменты).

Важно: в `go.mod` модуль называется `forum` (исторически).

## Оглавление (TOC)
- [Что реализовано по заданию](#что-реализовано-по-заданию)
- [Регистрация и вход](#регистрация-и-вход)
- [Способы аутентификации](#способы-аутентификации)
- [Привязка аккаунтов и поведение при объединении](#привязка-аккаунтов-и-поведение-при-объединении)
- [Посты и комменты](#посты-и-комменты)
- [Личные сообщения (DM) + работа в реальном времени](#личные-сообщения-dm--работа-в-реальном-времени)
- [Unified center: Activity + Notifications](#unified-center-activity--notifications)
- [Бонусы (сверх задания)](#бонусы-сверх-задания)
- [Разрешённые пакеты](#разрешённые-пакеты)
- [Запуск и настройка](#запуск-и-настройка)
- [Требования для локального запуска](#требования-для-локального-запуска)
- [Переменные окружения](#переменные-окружения)
- [Локальный HTTPS запуск (mkcert)](#локальный-https-запуск-mkcert)
- [Локальная настройка OAuth](#локальная-настройка-oauth)
- [Docker](#docker)
- [Тестирование](#тестирование)
- [Проверка безопасности и данных через CLI](#проверка-безопасности-и-данных-через-cli)
- [Статусы и ошибки (HTTP)](#статусы-и-ошибки-http)
- [Отладка: проверка HTTP 500 (только для dev)](#отладка-проверка-http-500-только-для-dev)
- [Структура проекта](#структура-проекта)
- [Авторы](#авторы)

## Что реализовано по заданию

### Регистрация и вход
- Регистрация и логин обязательны для работы с форумом.
- Логин: 1 поле "e-mail or username" + password (можно войти по нику или e-mail).
- Logout доступен с любой страницы.

Примечание:
- Поля профиля (`first name` / `last name` / `age` / `gender`) сделаны необязательными на первичной регистрации.
- После первого логина пользователь попадает на страницу профиля (`profile setup`) и может заполнить их там.

### Способы аутентификации
- `local`: email or username + password
- `google`: OAuth 2.0 Authorization Code Flow
- `github`: OAuth 2.0 Authorization Code Flow
- `facebook`: OAuth 2.0 Authorization Code Flow

OAuth реализован через реестр провайдеров (`internal/oauth`) и универсальные обработчики:
- `GET /auth/{provider}/login`
- `GET /auth/{provider}/callback`

Архитектура сделана расширяемой: чтобы добавить нового провайдера, например Steam, Twitch или My.Games, достаточно реализовать новый адаптер провайдера с общим интерфейсом и зарегистрировать его в реестре провайдеров.

### Привязка аккаунтов и поведение при объединении
- Локальная регистрация и вход остаются активными и не заменяются OAuth.
- На странице профиля есть блок `Linked accounts` со статусом провайдеров: `linked` / `not linked`.
- Авторизованный пользователь может явно привязать провайдера из профиля.
- Отвязка блокируется, если после неё у аккаунта не останется ни одного корректного способа входа.
- Совпадение e-mail из OAuth не приводит к автоматической привязке или автоматическому объединению для неавторизованного пользователя.
- Если OAuth возвращает e-mail, который уже используется существующим аккаунтом форума, приложение запускает явный сценарий подтверждения.
- Если нужно объединить два существующих аккаунта, приложение запускает явный сценарий merge.
- Канонический аккаунт выбирается по `created_at` (по умолчанию побеждает более старый аккаунт).
- Display name по умолчанию при merge тоже берётся от более старого аккаунта и должен быть подтверждён явно.
- Merge выполняется транзакционно и переносит связанные identity, а также текущие пользовательские данные форума в канонический аккаунт.
- Если merge небезопасен для известного edge case, операция явно отклоняется, а не продолжается частично.

### Посты и комменты
- Посты с категориями.
- Комментарии к постам.
- Лента постов, комментарии видны после открытия поста.
- Typing in progress для комментариев: пользователи, открывшие один и тот же пост, видят в real time, что кто-то печатает комментарий.

### Личные сообщения (DM) + работа в реальном времени
- Список пользователей для чата: online/offline, видим всегда.
- Сортировка как в Discord: по последнему сообщению, если сообщений нет - по алфавиту.
- История сообщений:
  - при открытии диалога грузятся последние 10 сообщений
  - пагинация: при скролле вверх догружается ещё по 10, с throttle (без спама scroll event)
- Формат сообщения: дата отправки + имя пользователя.
- Real-time доставка сообщений через WebSocket без refresh.
- Typing in progress для DM: собеседник видит в real time, что пользователь печатает сообщение.

### Unified center: Activity + Notifications
- В приложении есть единый центр `/center`.
- Верхний уровень:
  - `Activity`
  - `Notifications`
- Внутри `Notifications` есть подвкладки:
  - `DM`
  - `My content`
  - `Subscriptions`

`Activity` показывает собственную активность пользователя:
- мои посты
- мои like/dislike
- мои комментарии вместе с контекстом поста

`Notifications` показывает входящие события:
- новые DM
- like/dislike моего поста
- комментарий к моему посту
- like/dislike моего комментария
- новые комментарии в подписанном посте
- новые посты у автора, на которого я подписан

Дополнительно:
- уведомления сохраняются в SQLite
- обновляются в real time через существующий WebSocket hub
- поддерживаются `read/unread`, `mark one as read`, `mark all as read`
- колокольчик использует aggregate unread по unified center

Подписки:
- подписка на пост - ручная
- автор автоматически подписан на свой пост
- подписка на автора - ручная
- follow автора уведомляет только о новых постах, не о комментариях

Edit / Remove:
- пользователь может редактировать только свои посты
- пользователь может удалять только свои посты
- пользователь может редактировать только свои комментарии
- редактирование комментария разрешено только в течение 30 минут после создания
- пользователь может удалять свои комментарии без этого ограничения

Важно:
- self-actions не создают уведомления
- переключение реакции `like -> dislike` или `dislike -> like` создаёт новое уведомление под новое состояние

## Бонусы (сверх задания)
- Профили пользователей (display name + необязательные поля `first/last/age/gender`).
- Unread для DM: серверный `true` + локальный cache (`localStorage`) как быстрый fallback.
- Загрузка изображений (JPEG/PNG/GIF, до 20MB) для постов и личных сообщений.
- Кастомная 404 страница.
- Реакции like/dislike на посты и комментарии.
- Unified center `/center` с вкладками `Activity` и `Notifications`.
- Подписки на посты и авторов.
- Persisted notifications + real-time push.
- Edit/remove своих постов и комментариев.

## Разрешённые пакеты
- Только стандартные пакеты Go +:
  - `github.com/gorilla/websocket`
  - `github.com/mattn/go-sqlite3`
  - `golang.org/x/crypto/bcrypt`
  - `github.com/google/uuid`

Frontend: без React/Angular/Vue и любых библиотек/фреймворков.

## Запуск и настройка

### Требования для локального запуска

#### Windows (Git Bash / MINGW64 или MSYS2 UCRT64)
Важно: `github.com/mattn/go-sqlite3` требует CGO и C-компилятор (`gcc`).

```bash
export CGO_ENABLED=1
```

#### Linux / WSL / macOS
```bash
export CGO_ENABLED=1
```

### Переменные окружения
- `FORUM_DB_PATH` - путь к SQLite файлу (по умолчанию `forum.db`)
- `FORUM_HTTP_ADDR` - адрес HTTP redirect server (по умолчанию `127.0.0.1:8080`)
- `FORUM_HTTPS_ADDR` - адрес основного HTTPS server (по умолчанию `127.0.0.1:8443`)
- `FORUM_BASE_URL` - канонический public base URL приложения; должен точно совпадать с `scheme/host/port`, используемыми в OAuth callback URL
- `TLS_CERT_FILE` - путь к TLS certificate PEM
- `TLS_KEY_FILE` - путь к TLS private key PEM
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

Примечания:
- HTTP используется только для redirect на HTTPS.
- Основной dev/deploy switch ожидается через значения env/config, а не через изменение кода.
- HTTPS server fail-fast на старте, если TLS certificate/key не заданы или не найдены.
- Приложение не подхватывает `.env` автоматически из кода. Для локального запуска переменные нужно загрузить в окружение shell вручную.

Минимальный пример:
```bash
export FORUM_DB_PATH=./forum.db
export FORUM_HTTP_ADDR=127.0.0.1:8080
export FORUM_HTTPS_ADDR=127.0.0.1:8443
export FORUM_BASE_URL=https://127.0.0.1:8443
export TLS_CERT_FILE=./certs/dev-cert.pem
export TLS_KEY_FILE=./certs/dev-key.pem
```

### Локальный HTTPS запуск (mkcert)
Для локального HTTPS должен быть установлен `mkcert`.

Проект по умолчанию ожидает сертификаты в:
- `certs/dev-cert.pem`
- `certs/dev-key.pem`

1. Установите `mkcert`.
2. Один раз инициализируйте локальный CA:
```bash
mkcert -install
```

3. Сгенерируйте локальные сертификаты:
```bash
mkcert -cert-file certs/dev-cert.pem -key-file certs/dev-key.pem 127.0.0.1 localhost ::1
```

4. Создайте локальный `.env` из шаблона и заполните реальные значения:
```bash
cp .env.example .env
```

## Модерация и роли

Теперь форум поддерживает пять ролей:

- `guest`: только просмотр
- `user`: посты, комментарии, реакции, репорты
- `moderator`: очередь модерации, soft delete, эскалационные репорты
- `admin`: одобрение модераторов, прямое управление ролью moderator, категории, hard delete
- `owner`: единственная верхнеуровневая staff-роль, одобрение админов, delete protection, purge истории

Сейчас staff-badge'и отображаются в профиле, постах и комментариях:

- `owner`
- `admin`
- `moder`

### Bootstrap первого owner

Owner никогда не создаётся автоматически при обычном запуске сервера.

Создать первого owner нужно явно:

```bash
go run ./cmd/server bootstrap-owner --email owner@example.com --username owner --password secret
```

Опционально можно указать свой путь к БД:

```bash
go run ./cmd/server bootstrap-owner --db ./forum.db --email owner@example.com --username owner --password secret
```

Правила:

- bootstrap работает только пока в системе нет существующего `admin` или `owner`
- bootstrap создаёт ровно одного `owner`
- передача owner через UI не поддерживается, понизить owner тоже нельзя

### Поток модерации

- новые посты сразу публичны, но помечаются как `visible + under_review`
- `under_review` виден всем в UI поста
- при approve пост исчезает из очереди модерации, а также сохраняются `approved_by` / `approved_at`
- редактирование уже одобренного поста не отправляет его обратно в очередь автоматически
- комментарии не проходят очередь `under_review` и модерируются через репорты

Семантика delete / restore / appeal:

- `moderator` / `admin` / `owner` могут делать soft delete постов и комментариев
- `admin` / `owner` могут делать hard delete
- `owner` может включать и отключать delete protection для поста
- защищённые посты нельзя удалить ни через soft delete, ни через hard delete, пока защита не снята
- пользователи видят soft-deleted посты и комментарии как `[deleted]`
- `moderator` может восстанавливать только тот контент, который сам отправил в soft delete
- `admin` / `owner` могут восстанавливать любой сохранённый soft-deleted контент
- авторы контента могут подавать appeal на moderation decisions; решения `moderator` идут на `admin`, решения `admin` идут на `owner`

Репорты:

- репорты от `user` попадают `moderator` + `admin` + `owner`
- репорты от `moderator` эскалируются только `admin` + `owner`
- при закрытии репорта он исчезает из активной очереди у остальных получателей

Категории:

- системная категория `other` существует всегда
- `other` стабильно хранится в БД и не может быть удалена
- при удалении любой другой категории все её посты переносятся в `other`

### Единый центр

UI модерации и администрирования остаётся внутри существующего SPA-маршрута `/center`:

- `Notifications`: All, Deleted, My reports, Appeals
- `Moderation`: Under review, Reports, History
- `Management`: Requests, Roles, Categories, Journal

5. Загрузите переменные окружения в текущую shell-сессию и запустите сервер:
```bash
set -a
source .env
set +a

go run ./cmd/server
```

Проверка:
- `https://127.0.0.1:8443/`
- `http://127.0.0.1:8080/` (`308 Permanent Redirect` -> HTTPS)

### Локальная настройка OAuth
1. Создайте OAuth-приложения в кабинетах разработчика Google, GitHub и при необходимости Facebook.
2. Настройте callback URL каждого провайдера на это приложение:
   - Google: `https://127.0.0.1:8443/auth/google/callback`
   - GitHub: `https://127.0.0.1:8443/auth/github/callback`
   - Facebook: `https://127.0.0.1:8443/auth/facebook/callback`
3. Заполните соответствующие значения в `.env`.
4. Перед `go run ./cmd/server` загрузите `.env` в окружение:
```bash
set -a
source .env
set +a
```
5. Запустите сервер и используйте кнопки на страницах `Login` / `Register`:
   - `Continue with Google`
   - `Continue with GitHub`
   - `Continue with Facebook`

Примечания:
- OAuth-провайдеры необязательны. Если env-переменные провайдера не заданы, локальная аутентификация продолжает работать, а отсутствующий провайдер просто не показывается в UI.
- `FORUM_BASE_URL` должен точно совпадать с `scheme/host/port`, используемыми в OAuth callback URL.
- Для локального сценария Facebook Login может потребовать дополнительную настройку в Meta Developers.

Пример `.env` для локального запуска:
```env
FORUM_DB_PATH=./forum.db
FORUM_HTTP_ADDR=127.0.0.1:8080
FORUM_HTTPS_ADDR=127.0.0.1:8443
FORUM_BASE_URL=https://127.0.0.1:8443
TLS_CERT_FILE=./certs/dev-cert.pem
TLS_KEY_FILE=./certs/dev-key.pem

GOOGLE_CLIENT_ID=your_google_client_id
GOOGLE_CLIENT_SECRET=your_google_client_secret
GOOGLE_REDIRECT_URL=https://127.0.0.1:8443/auth/google/callback

GITHUB_CLIENT_ID=your_github_client_id
GITHUB_CLIENT_SECRET=your_github_client_secret
GITHUB_REDIRECT_URL=https://127.0.0.1:8443/auth/github/callback

FACEBOOK_CLIENT_ID=your_facebook_client_id
FACEBOOK_CLIENT_SECRET=your_facebook_client_secret
FACEBOOK_REDIRECT_URL=https://127.0.0.1:8443/auth/facebook/callback
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

Дополнительно покрыто тестами:
- центр активности и уведомлений (`center service/repo/http`)
- правила создания уведомлений и защита от self-notifications
- правила edit/remove для постов и комментариев
- graceful shutdown для HTTP redirect server, HTTPS server и WS hub
- integration test на app lifecycle shutdown через `internal/app`
- secure cookies, rate limiting, login throttling и WebSocket handshake limits

## Проверка безопасности и данных через CLI

### Посмотреть данные через sqlite3
```bash
sqlite3 forum.db
.tables
SELECT id, username, email FROM users;
SELECT id, user_id, title FROM posts;
SELECT id, post_id, user_id, body FROM comments;
.quit
```

### Проверить хранение паролей
Ожидаемо: в таблице пользователей должен храниться bcrypt hash, а не пароль в открытом виде.

```bash
sqlite3 forum.db
.headers on
.mode column
SELECT id, username, email, pass_hash FROM users;
.quit
```

Что проверять:
- значения в `pass_hash` не должны быть похожи на обычные пароли
- для bcrypt хешей обычно ожидается префикс вида `$2a$`, `$2b$` или `$2y$`

Быстрая выборка только по хешам:
```bash
sqlite3 forum.db "SELECT id, username, substr(pass_hash, 1, 4) AS prefix, length(pass_hash) AS len FROM users;"
```

### Проверить server-side сессии и формат токена
Ожидаемо: токен сессии должен храниться на сервере и выглядеть как UUID.

```bash
sqlite3 forum.db
.headers on
.mode column
SELECT token, user_id, expires_at FROM sessions;
.quit
```

Быстрая проверка первых токенов:
```bash
sqlite3 forum.db "SELECT token FROM sessions LIMIT 5;"
```

Дополнительно вручную:
- открой DevTools -> Application -> Cookies
- найди `__Host-forum_session` (/api/me)
- сравни значение cookie со значением `sessions.token` в БД для активной сессии
- токен должен выглядеть как UUID, например `8-4-4-4-12`

## Статусы и ошибки (HTTP)
- UI deep-links должны отдавать SPA (`200`):
  - `/`, `/login`, `/register`, `/new`, `/post/1`, `/dm/1`, `/u/demo`
- API `404`:
  - `GET /api/does-not-exist` -> `404`
- Asset `404`:
  - `GET /assets/nope.svg` -> `404`
- `400`:
  - пустой логин / пустой пост / пустой коммент -> `400`
- `401`:
  - гость пытается создать пост / коммент / открыть DM -> `401`
- `405`:
  - неверный HTTP method -> `405`
- `429`:
  - превышение rate limit на auth / write actions / WebSocket handshake
- `500`:
  - `/api/debug/500` при `DEBUG_500=1`

## Отладка: проверка HTTP 500 (только для dev)
В проекте есть dev-only endpoint для демонстрации `500` и проверки panic recovery.

Запуск:
```bash
export DEBUG_500=1
go run ./cmd/server
```

Проверка:
```bash
curl -k -i https://127.0.0.1:8443/api/debug/500
```

Ожидаемо: `HTTP/1.1 500 Internal Server Error` и JSON-ошибка.

Если `DEBUG_500` не задан, endpoint ведёт себя как отсутствующий (`404`).

## Структура проекта

```text
real-time-forum/
├─ cmd/server/                 # точка входа
│  └─ main.go                  # entrypoint и обработка финальной ошибки
├─ internal/                   # приватный код приложения
│  ├─ app/                     # composition root: Run(), bootstrap, lifecycle, graceful shutdown
│  ├─ domain/                  # доменные сущности/типы
│  ├─ http/                    # HTTP-слой: router, handlers, middleware, cookies, responses/errors
│  ├─ oauth/                   # OAuth providers, registry, normalized external identity
│  ├─ platform/                # утилиты: id/uuid, clock/time
│  ├─ realtime/ws/             # WS hub + события presence/pm/typing/notifications + lifecycle stop/done
│  ├─ repo/                    # слой доступа к данным (интерфейсы + реализации)
│  │  └─ sqlite/               # SQLite: schema.sql, миграции/legacy-safe апдейты, запросы, тесты
│  └─ service/                 # use cases: auth, posts, private messages, attachments, center
├─ scripts/                    # скрипты для docker/audit
│  ├─ audit_smoke.sh
│  ├─ docker-run.ps1
│  └─ docker-run.sh
├─ var/uploads/                # runtime-хранилище загруженных файлов (локально/в контейнере)
├─ web/                        # фронт/статика (SPA)
│  ├─ assets/
│  ├─ app.js                   # routes/views, включая unified center
│  ├─ index.html
│  ├─ 404.html                 # кастомная 404 страница
│  └─ styles.css
├─ Dockerfile
├─ go.mod
└─ README.md
```

## Авторы
- Nazar Yestayev (@nyestaye / @legion2440)
- Aibar Ramazan (@mmurnau)
- Magzhan Tastan (@mtastan)
- Talgat Tokayev (@tatokayev)
- Zhomart Utemissov (@zutemiss)
