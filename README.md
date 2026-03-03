## real-time-forum

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

### Posts and Comments
- Посты с категориями.
- Комментарии к постам.
- Лента постов, комментарии видны после открытия поста.

### Private Messages (DM) + real-time
- Список пользователей для чата: online/offline, видим всегда.
- Сортировка как в Discord: по последнему сообщению, если сообщений нет - по алфавиту.
- История сообщений:
  - При открытии диалога грузятся последние 10 сообщений.
  - Пангинация: при скролле вверх догружается еще по 10, с throttle (без спама scroll event).
- Формат сообщения: дата отправки + имя пользователя.
- Real-time доставка сообщений через WebSocket без refresh.

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

Пример:
```bash
export FORUM_DB_PATH=./forum.db
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
│  ├─ realtime/ws/             # WS hub + события presence/pm
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
- Rauan Yelubayeva (@ryelubay)
