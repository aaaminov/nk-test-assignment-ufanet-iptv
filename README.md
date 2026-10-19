# IPTV backend

Мини-бэкенд IPTV/OTT на Go и PostgreSQL. Сейчас реализованы HTTP-процесс,
конфигурация, подключение к БД, автоматические миграции и demo-данные.
Доступны каталог каналов и выдача HLS URL с HTTP Basic Auth.

## Запуск

Нужны Docker Engine и Docker Compose. Из корня проекта:

```sh
docker compose up
```

Compose собирает приложение и запускает его после готовности PostgreSQL 17.
Go на хосте и подготовка `.env` не требуются. HTTP доступен по
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

Compose использует публичные demo-настройки БД: база и пользователь `iptv_demo`,
пароль `iptv_demo`, без TLS внутри Docker-сети. Они предназначены для локального
запуска. `DATABASE_URL` приложения указывает на hostname `db`.
`DB_MAX_CONNS` (по умолчанию `10`) и `LOG_LEVEL` (по умолчанию `info`) можно
переопределить переменными окружения при запуске Compose.

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

Integration-тесты используют отдельную временную PostgreSQL 17 на
`127.0.0.1:15432`, без demo-volume:

```sh
docker compose -f compose.test.yaml -p iptv-test up -d --wait db
TEST_DATABASE_URL='postgres://iptv_test@127.0.0.1:15432/iptv_test?sslmode=disable' make integration
docker compose -f compose.test.yaml -p iptv-test down
```

Остановите test-стенд и при ошибке тестов. `TEST_DATABASE_URL` обязателен;
используйте только отдельную test-БД.

Внешняя зависимость `github.com/jackc/pgx/v5/pgxpool` предоставляет PostgreSQL
драйвер и pool соединений. `golang.org/x/crypto/bcrypt` нужен для безопасного
хэширования и проверки паролей, которых нет в stdlib. Остальной код использует
stdlib.
