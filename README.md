# IPTV backend

Мини-бэкенд IPTV/OTT на Go и PostgreSQL. Сейчас реализованы HTTP-процесс,
конфигурация, подключение к БД, автоматические миграции и demo-данные.
Доступны каталог каналов и выдача HLS URL с HTTP Basic Auth.

## Запуск

Нужны Docker Engine и Docker Compose. Перед первым запуском из корня
проекта скопируйте пример настроек:

```sh
cp .env.example .env
docker compose up
```

Compose собирает приложение и запускает его после готовности PostgreSQL 17.
Go на хосте не требуется. HTTP по умолчанию доступен по
`http://127.0.0.1:8080`; порт БД на хост не публикуется.

```sh
docker compose stop
docker compose up
```

Данные БД сохраняются в named volume `postgres_data`, в том числе после
`docker compose down`. После изменения исходников пересоберите образ:

```sh
docker compose up --build
```

Compose берёт настройки из `.env` или окружения процесса; окружение имеет
приоритет. `.env.example` содержит публичные значения для локального demo
и отдельной тестовой БД. Локальный `.env` исключён из Git и Docker build context.

`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` и `DATABASE_URL` обязательны.
В `DATABASE_URL` укажите те же пользователя, пароль и БД; hostname внутри
Compose — `db`, порт — `5432`. В URL экранируйте спецсимволы в логине
и пароле через percent-encoding. `HTTP_PORT` задаёт порт на хосте (по умолчанию `8080`),
`DB_MAX_CONNS` — размер pool (по умолчанию `10`), `LOG_LEVEL` — уровень логов
(по умолчанию `info`). Без TLS внутри Docker-сети этот стенд предназначен
для локального запуска.

`POSTGRES_*` применяются при инициализации пустой БД: правка `.env` не меняет
пользователей и пароли в существующем volume. Go-приложение само `.env`
не читает; при запуске вне Compose передайте доступный `DATABASE_URL`
через окружение процесса.

До открытия HTTP-порта приложение создаёт таблицы `users`, `channels`,
`schema_migrations` и `data_seeds`. SQL-файлы встроены в бинарник через `embed`
и применяются в порядке имён; схема и записи версий фиксируются одной транзакцией.
Повторный запуск пропускает применённые миграции и сохраняет данные.
Ошибка миграции останавливает запуск, изменения откатываются. На миграции
отведено 3 секунды. Поддерживается один экземпляр приложения; применённые
SQL-файлы не изменяют, обновления добавляют новой миграцией.

После миграций приложение однократно создаёт двух demo-пользователей и пять
каналов; четыре канала активны, пятый выключен. Данные и отметка `base_demo`
в `data_seeds` сохраняются одной транзакцией. При повторном запуске отметка
сохраняет пользовательские изменения, включая смену пароля и удаление записей.
Ошибка seed откатывает его данные и отметку, останавливает запуск; после
устранения причины следующий старт повторит seed. На seed отведено 3 секунды.

Публичные demo-учётные данные для локальной проверки:

| Логин | Пароль | Активен |
| --- | --- | --- |
| `viewer` | `viewer_demo` | да |
| `inactive` | `inactive_demo` | нет |

В БД сохраняются bcrypt-хэши с cost 10 и случайной солью. Пароли длиннее
72 байт UTF-8 отклоняются при хэшировании и проверке без усечения.
HLS URL имеют вид `https://stream.example.com/live/ch1/index.m3u8`;
потоковый сервер не предоставляется.

Образ собирается в два этапа: Go 1.26 компилирует статический бинарник,
а runtime `scratch` содержит бинарник и CA-сертификаты. Приложение работает
без root; exec `ENTRYPOINT` передаёт SIGTERM непосредственно процессу.
Приложение даёт HTTP-запросам до 10 секунд на завершение, затем закрывает
pool БД. Compose ждёт остановки процесса до 15 секунд.

## Каталог каналов

```sh
curl -u viewer:viewer_demo http://127.0.0.1:8080/api/v1/channels
```

`GET /api/v1/channels` возвращает `200` и JSON вида
`{"channels":[{"id":1,"number":1,"name":"Первый"}]}`. В списке только
активные каналы, упорядоченные по `number`, затем `id`; пустой каталог —
`{"channels":[]}`. URL потоков в каталог не входят.

Доступ разрешён активному пользователю с верным паролем. Отсутствующие или
неверные credentials дают `401` с `WWW-Authenticate: Basic realm="iptv"`.
Внутренний сбой даёт `500` без подробностей БД. Формат ошибок:
`{"error":{"code":"internal_error","message":"internal server error"}}`;
для `401` используются `code: unauthorized` и `message: authentication required`.
SQL-запрос каталога ограничен 3 секундами и учитывает отмену HTTP-запроса.

## Playback

```sh
curl -u viewer:viewer_demo http://127.0.0.1:8080/api/v1/channels/1/playback
```

`GET /api/v1/channels/{id}/playback` возвращает `200` и JSON вида
`{"channel_id":1,"url":"https://stream.example.com/live/ch1/index.m3u8"}`.
URL берётся из `stream_url` активного канала в БД; потоковый сервер не нужен.
Доступ защищён тем же Basic Auth, что и каталог.

ID разбирается как положительный десятичный `int64` (от 1 до 9223372036854775807).
Неверное число, ноль, отрицательное значение или переполнение дают `400`
с `code: invalid_id` и `message: channel id must be a positive int64`.
Несуществующий или выключенный канал даёт `404` с `code: channel_not_found`
и `message: channel not found`. Формат JSON-ошибок и безопасный `500` совпадают
с каталогом. SQL использует параметр ID, timeout 3 секунды и context HTTP-запроса.

## HTTP-контракт

Оба ресурса поддерживают `HEAD`: статус и заголовки соответствуют `GET`, тело
не передаётся. В `/api/v1` и `/api/v1/…` Basic Auth проверяется до канонизации
пути, выбора маршрута, метода и разбора ID. После успешной Auth неверный
метод для известного пути даёт `405` с `Allow: GET, HEAD`,
`code: method_not_allowed` и `message: method not allowed`.

После успешной Auth `ServeMux` перенаправляет пути с повторными `/`
и сегментами `.`/`..` на канонический URL.

Неизвестный путь, включая лишний завершающий `/` у ресурса, даёт JSON `404`
с `code: not_found` и `message: resource not found`. Формат JSON относится
к прикладным ошибкам; ошибки разбора HTTP до handler формирует `net/http`.

## Проверки

Для команд Makefile нужны Go 1.26+ и GNU Make в Linux или WSL:

```sh
make check
make test
docker compose config --quiet
```

`make check` проверяет форматирование, build, vet и unit-тесты.
`make test` запускает только unit-тесты. Обе команды автономны:
Docker и PostgreSQL им не нужны. Все targets описаны в `make help`.

Workflow GitHub Actions настроен на `push` и `pull_request`: job `check`
выполняет `make check`, а `integration` поднимает PostgreSQL 17 через
`compose.test.yaml`, выполняет `make integration` и останавливает стенд
даже при ошибке проверки. CI использует публичные настройки `.env.example`
и явный `TEST_DATABASE_URL`. Версия Go берётся из `go.mod`, кэширование
модулей и build cache обеспечивает `actions/setup-go`.

Integration-тесты используют отдельную временную PostgreSQL 17,
по умолчанию на `127.0.0.1:15432`, без demo-volume:

```sh
docker compose -f compose.test.yaml -p iptv-test up -d --wait db
(
  . ./.env
  export TEST_DATABASE_URL
  make integration
)
docker compose -f compose.test.yaml -p iptv-test down
```

Настройки test-стенда задаются переменными `TEST_POSTGRES_USER`,
`TEST_POSTGRES_PASSWORD`, `TEST_POSTGRES_DB`, `TEST_POSTGRES_PORT`
и `TEST_DATABASE_URL` из `.env`. Пользователь, пароль, имя БД и порт
в `TEST_DATABASE_URL` должны соответствовать test-стенду. Файл примера
совместим с shell; значения со спецсимволами заключайте в одинарные кавычки.
Если настройки уже экспортированы в окружение, запускайте `make integration`
напрямую, без загрузки `.env` в shell.

Остановите test-стенд и при ошибке тестов. `TEST_DATABASE_URL` обязателен;
используйте только отдельную test-БД.

Внешняя зависимость `github.com/jackc/pgx/v5/pgxpool` предоставляет PostgreSQL
драйвер и pool соединений. `golang.org/x/crypto/bcrypt` нужен для безопасного
хэширования и проверки паролей, которых нет в stdlib. Остальной код использует
stdlib.
