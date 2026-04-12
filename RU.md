# pgcompare
`pgcompare` — локальный CLI-инструмент для сравнения производительности PostgreSQL-запросов до и после оптимизации с генерацией одного HTML-отчёта.

English version: [README.md](./README.md).

## Установка

### Ссылка на релизы
- Последний релиз: https://github.com/pg-tools/pgcompare/releases/latest

### Linux/macOS через install script

Установить последнюю версию в `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh
```

Установить конкретную версию:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh -s -- -v v0.1.0
```

Установить в произвольную директорию:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh -s -- -b /usr/local/bin
```

### Homebrew (macOS)

```bash
brew tap pg-tools/tap
brew install pg-tools/tap/pgcompare
```

## Проверка установки

```bash
pgcompare --help
```

## Что делает pgcompare

`pgcompare` выполняет один и тот же сценарий два раза:

1. Читает `pgcompare.yaml` и `.env` из одной директории проекта.
2. Подготавливает состояние `before` с одним значением `MIGRATION_VERSION`.
3. Прогоняет `before`-запросы и снимает планы `EXPLAIN ANALYZE`.
4. Пересоздаёт окружение для состояния `after`.
5. Прогоняет `after`-запросы и снова снимает планы.
6. Генерирует один HTML-отчёт со сравнением.

Инструмент подходит для оценки эффекта от:

- изменений схемы
- новых или изменённых индексов
- переписанных SQL-запросов
- изменений в миграциях
- изменений в тестовых данных

## Требования

- установленный Docker
- доступный в `PATH` `docker compose` v2 или `docker-compose` v1
- PostgreSQL, доступный с хоста по `localhost:$POSTGRES_PORT`
- директория проекта, где лежат `.env`, `pgcompare.yaml` и SQL-файлы из конфига

Если собирать из исходников, нужен Go `1.25`.

## Структура проекта

`pgcompare` считает корнем проекта директорию, в которой находится `pgcompare.yaml`. Из неё он загружает `.env`, в ней запускает внешние команды и туда же по умолчанию пишет отчёт.

Рекомендуемая структура:

```text
student-project/
├── .env
├── docker-compose.yml
├── pgcompare.yaml
├── queries_before.sql
└── queries_after.sql
```

Пример `.env`:

```dotenv
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=app
POSTGRES_PORT=5432
MIGRATION_VERSION=0
```

`POSTGRES_PORT` можно не указывать, тогда используется `5432`. `MIGRATION_VERSION` рекомендуется держать в `.env`, если ваш проект уже использует эту переменную для ручного запуска. Во время сравнения `pgcompare` всё равно подменяет её своим значением из `pgcompare.yaml`.

## Конфигурация

Пример `pgcompare.yaml`:

```yaml
migration:
  before_version: "3"
  after_version: "5"

setup:
  command: "$DOCKER_COMPOSE up -d postgres && $DOCKER_COMPOSE run --rm -T migrate && $DOCKER_COMPOSE run --rm -T seed"

benchmark:
  before_queries: queries_before.sql
  after_queries: queries_after.sql
  iterations: 100
  concurrency: 4

report:
  description:
    - query: find_active_users
      what: Перевести запрос с последовательного сканирования на индексный доступ.
      changes: |
        CREATE INDEX idx_users_active_created_at
          ON users (active, created_at DESC);
      expected: Снизить p95 и увеличить QPS для этого запроса.
```

Пояснения к флагам setup-команды:

- `up -d postgres` — явно запускает контейнер PostgreSQL перед миграциями, гарантируя доступность базы
- `-T` — отключает выделение TTY. По умолчанию `docker compose run` пытается выделить TTY, что приводит к ошибке при запуске из `pgcompare` (неинтерактивный контекст). Без `-T` setup-команда завершится с ошибкой
- `--rm` — удаляет контейнер после завершения. Без этого флага каждый запуск `pgcompare run` будет оставлять остановленные контейнеры migrate/seed

Как работает переключение миграций:

`pgcompare` сам не накатывает миграции напрямую. Он дважды запускает ваш `setup.command` и подменяет `MIGRATION_VERSION`:

- сначала значением `migration.before_version`
- затем значением `migration.after_version`

Поэтому ваш механизм наката миграций должен явно использовать `MIGRATION_VERSION`. Если миграционный скрипт или Docker-сервис игнорируют эту переменную, обе фазы подготовят одно и то же состояние базы.

Типовой вариант:

`.env`

```dotenv
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=app
POSTGRES_PORT=5432
MIGRATION_VERSION=0
```

`docker-compose.yml`

```yaml
services:
  migrate:
    env_file:
      - .env
    environment:
      MIGRATION_VERSION: ${MIGRATION_VERSION}
    command: sh -c "./migrate up --to ${MIGRATION_VERSION}"
```

`MIGRATION_VERSION` удобно держать в `.env` как значение по умолчанию для ручного локального запуска. Во время `pgcompare run` это значение всё равно будет автоматически переопределено для фаз `before` и `after`.

Структура конфигурации:

### `migration`

- `before_version`: значение, которое будет подставлено в `MIGRATION_VERSION` для первой фазы
- `after_version`: значение, которое будет подставлено в `MIGRATION_VERSION` для второй фазы

Именно эти два значения задают, какие состояния схемы будут сравниваться. Оба состояния должны корректно собираться вашим проектом.

### `setup`

- `command`: shell-команда, которая запускается в директории проекта

Эта команда должна полностью подготовить базу. На практике обычно она поднимает контейнеры, накатывает миграции и при необходимости выполняет сидирование. Команда должна завершаться с `exit 0` только тогда, когда PostgreSQL уже готов к тестовым запросам.

### `benchmark`

- `before_queries`: SQL-файл для фазы `before`
- `after_queries`: SQL-файл для фазы `after`
- `iterations`: общее число выполнений каждого запроса
- `concurrency`: число параллельных воркеров для бенчмарка

Пути к SQL-файлам считаются относительно директории, в которой лежит `pgcompare.yaml`.

### `report`

- `description`: необязательный список блоков, который показывается в начале отчёта

Каждый элемент `description` может содержать:

- `query`: имя запроса для подписи в отчёте
- `what`: короткое описание того, что оптимизировалось
- `changes`: SQL-изменения или изменения схемы
- `expected`: ожидаемый эффект от оптимизации

Во время запуска `pgcompare` также подставляет переменную `DOCKER_COMPOSE`, чтобы одна и та же команда работала и с Compose v1, и с Compose v2.

## Формат SQL-файлов

Каждый SQL-файл должен содержать именованные запросы:

```sql
-- name: find_active_users
SELECT id, email
FROM users
WHERE active = true
ORDER BY created_at DESC
LIMIT 100;

-- name: count_orders_by_status
SELECT status, COUNT(*)
FROM orders
GROUP BY status;
```

Правила:

- в обоих файлах должны быть одни и те же имена запросов
- порядок запросов в обоих файлах должен совпадать
- внутри одного файла имена запросов должны быть уникальными

## Что есть в отчёте

HTML-отчёт отвечает на три практических вопроса:

1. Что именно было изменено?
2. Стало ли быстрее?
3. Как изменился план выполнения?

В верхней части отчёта показывается описание оптимизации из `report.description`. Здесь удобно зафиксировать цель изменения, сам SQL или изменение схемы и ожидаемый результат.

Сводка по всем запросам показывает в компактном виде:

- p95 до и после
- изменение p95 в процентах
- speedup
- QPS до и после
- изменение QPS в процентах

Значение speedup рассчитывается по p95:

```text
speedup = p95_before / p95_after
```

Для каждого запроса отдельно отчёт показывает:

- p50, p95, p99, min, max, mean и standard deviation
- QPS и error rate
- процентные изменения между фазами `before` и `after`
- краткую сводку обнаруженных изменений в плане
- отображение планов `before` и `after`

Если `report.description` не заполнен, в начале отчёта показывается предупреждение, чтобы было видно, что поясняющая часть отсутствует.

## Использование

После подготовки `.env`, `pgcompare.yaml` и SQL-файлов запустите:

```bash
pgcompare run --config ./pgcompare.yaml
```

По умолчанию отчёт будет записан в `report.html` рядом с конфигом.

Записать отчёт в свой путь:

```bash
pgcompare run --config ./pgcompare.yaml --out ./artifacts/report.html
```

Включить подробные логи:

```bash
pgcompare run --config ./pgcompare.yaml --verbose
```

Посмотреть справку:

```bash
pgcompare run --help
```

## Примечания

- Подключение к PostgreSQL сейчас строится через `localhost` на основе значений из `.env`
- Если `--out` не указан, файл отчёта создаётся как `report.html` рядом с `pgcompare.yaml`
- Интерфейс HTML-отчёта сейчас на русском языке
