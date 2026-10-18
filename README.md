# IPTV backend

Мини-бэкенд IPTV/OTT на Go и PostgreSQL. Сейчас реализованы HTTP-процесс,
конфигурация, подключение к БД и автоматические миграции схемы.
API и тестовые пользователи пока отсутствуют;
HTTP-запросы возвращают 404.

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

До открытия HTTP-порта приложение создаёт таблицы `users`, `channels` и
`schema_migrations`. SQL-файлы встроены в бинарник через `embed` и применяются
в порядке имён; схема и записи версий фиксируются одной транзакцией.
Повторный запуск пропускает применённые миграции и сохраняет данные.
Ошибка миграции останавливает запуск, изменения откатываются. На миграции
отведено 3 секунды. Поддерживается один экземпляр приложения; применённые
SQL-файлы не изменяют, обновления добавляют новой миграцией.

Образ собирается в два этапа: Go 1.26 компилирует статический бинарник,
а runtime `scratch` содержит бинарник и CA-сертификаты. Приложение работает
без root; exec `ENTRYPOINT` передаёт SIGTERM непосредственно процессу.
Приложение даёт HTTP-запросам до 10 секунд на завершение, затем закрывает
pool БД. Compose ждёт остановки процесса до 15 секунд.

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
драйвер и pool соединений; остальной код использует stdlib.
