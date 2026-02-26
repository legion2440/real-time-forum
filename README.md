## 💬 forum

Учебный веб-форум на Go + SQLite с SPA (1 HTML) на чистом JavaScript, без фронтенд-фреймворков и без CDN.

Функционал по заданию:
- регистрация/логин (cookie sessions, expiration, 1 active session)
- посты/комменты + категории (multi)
- лайки/дизлайки (посты и комменты)
- фильтры (categories / created / liked)
- Docker

## ✅ Быстрый старт

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

### Docker (скрипты для аудита)

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

## 🧪 Тестирование

```bash
go test ./...
go vet ./...
go test -race -count=1 ./...
```

## 🗄️ База данных (SQLite)

По умолчанию сервер использует файл БД `forum.db` (создается при первом запуске, если отсутствует).

### Посмотреть данные через sqlite3
```bash
sqlite3 forum.db
.tables
SELECT id, username, email FROM users;
SELECT id, user_id, title FROM posts;
SELECT id, post_id, user_id, body FROM comments;
.quit
```

## 🔎 Поиск

Поиск реализован через стратегии (пакет `internal/search`) и сейчас работает через SQLite `LIKE`.
Задел на расширение (эвристики/FTS) сделан через интерфейсы стратегий, чтобы можно было заменить реализацию без переписывания хендлеров.

## 🧯 Debug: проверить HTTP 500 (dev-only)

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

Если `DEBUG_500` не задан, endpoint должен вести себя как отсутствующий (404).


## Статусы и ошибки (HTTP)
- UI deep-links должны отдавать SPA (200):
  - `/`, `/login`, `/register`, `/new`, `/post/1`
- API 404:
  - `GET /api/does-not-exist` -> 404
- Asset 404:
  - `GET /assets/nope.svg` -> 404
- 400:
  - пустой логин/пустой пост/пустой коммент -> 400
- 401:
  - гость пытается создать пост/коммент или поставить реакцию -> 401
- 405:
  - неверный HTTP method -> 405
- 500:
  - `/api/debug/500` при `DEBUG_500=1`

## Ошибки и устойчивость
- Единый формат ошибок/ответов: `internal/http/errors.go`, `internal/http/response.go`
- Panic recovery (сервер не падает, возвращает 500): `internal/http/server.go`
- Dev-only 500 endpoint: `internal/http/handlers_debug.go`

## Tests
- Unit tests сервиса: `internal/service/auth_test.go`
- Repo tests: `internal/repo/sqlite/*_test.go`

## 🗂️ Структура проекта

```text
forum/
├─ cmd/server/                 # точка входа (main) для HTTP-сервера
│  └─ main.go                  # bootstrap приложения: сборка зависимостей, запуск сервера
├─ internal/                   # приватный код приложения (не импортируется извне модуля)
│  ├─ domain/                  # доменные сущности/типы (User, Post, Comment, Session и т.д.)
│  ├─ http/                    # HTTP-слой: router, handlers, middleware, cookies, responses/errors
│  ├─ platform/                # инфраструктурные утилиты: id/uuid, clock/time, и т.п.
│  ├─ repo/                    # слой доступа к данным (репозитории, интерфейсы и реализации)
│  │  └─ sqlite/               # SQLite-реализация: запросы, схемы/миграции, подключения
│  ├─ search/                  # поиск (например, FTS/индексация/ранжирование) если используется
│  └─ service/                 # бизнес-логика (use cases): оркестрация domain + repo + search
├─ scripts/                    # вспомогательные скрипты (аудит/запуск docker/утилиты)
│  ├─ audit_smoke.sh
│  ├─ docker-run.ps1
│  └─ docker-run.sh
├─ web/                        # фронт/статика (то, что сервер отдает браузеру)
│  ├─ assets/                  # изображения/иконки/гифки и прочая статика
│  ├─ app.js                   # клиентский JS
│  ├─ index.html               # входная HTML-страница
│  └─ styles.css               # стили
├─ Dockerfile                  # сборка контейнера
├─ go.mod                      # Go module
└─ README.md                   # документация проекта
```

## 📑 Оглавление (TOC)

- [💬 forum](#-forum)
- [✅ Быстрый старт](#-быстрый-старт)
  - [Запуск локально](#запуск-локально)
  - [Docker (скрипты для аудита)](#docker-скрипты-для-аудита)
- [🧪 Тестирование](#-тестирование)
- [🗄️ База данных (SQLite)](#️-база-данных-sqlite)
- [🔎 Поиск](#-поиск)
- [🧯 Debug: проверить HTTP 500 (dev-only)](#-debug-проверить-http-500-dev-only)
- [🗂️ Структура проекта](#️-структура-проекта)
- [📑 Оглавление (TOC)](#-оглавление-toc)


## 🧑‍💻 Автор
- Nazar Yestayev (@nyestaye / @legion2440)