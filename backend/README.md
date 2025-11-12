# City Service Bot (Backend)
Backend на Go для телеграм-бота приёма обращений граждан (ЖКХ, благоустройство и пр.) с ролью администратора,
экспортом отчётов и массовыми рассылками.

## Возможности (MVP)
- Приём обращений в **личном** чате: текст, фото/видео, геолокация.
- Ответ пользователю: `Заявка принята, номер <id>`.
- Кнопка **«Мои обращения»** — статусы и краткая история.
- Роль администратора: /admin `<секрет>`, уведомления о новых заявках, изменение статусов, комментарии.
- Экспорт отчёта CSV/TXT за период (HTTP и /export).
- Массовая рассылка `/broadcast "Текст"` с предпросмотром и подтверждением.
- /help и FAQ-кнопки.
- В группах игнорирует пользовательские сообщения (только объявления/рассылки).
- Поддержка webhook (по умолчанию long polling).

## Быстрый старт
1. Установите Go 1.22+ и PostgreSQL 13+.
2. Создайте БД:
   ```bash
   createdb citybot
   psql citybot -f migrations/init.sql
   ```
3. Скопируйте `.env` и заполните:
   ```bash
   cp .env .env.local || true
   ```
4. Запуск локально (long polling):
   ```bash
   go run ./cmd
   ```
5. Webhook-режим (если есть публичный HTTPS):
   - Установите `USE_WEBHOOK=1`, `PUBLIC_BASE_URL`, `WEBHOOK_PATH` в `.env`.
   - Запустите `go run ./cmd` — бот выставит webhook.

## HTTP-эндпоинты
- `GET /healthz` — проверка.
- `POST {WEBHOOK_PATH}` — Telegram webhook (если USE_WEBHOOK=1).
- `GET /export?from=YYYY-MM-DD&to=YYYY-MM-DD&token=API_TOKEN` — CSV.
- `GET /admin/issues?status=new|active|done|rejected&token=API_TOKEN` — JSON список.
- `POST /admin/status` — JSON `{issue_id,status,comment,token}`.

> **Безопасность**: для простоты используется токен `API_TOKEN` (по умолчанию `ADMIN_SECRET`). Для продакшна замените на полноценную аутентификацию.

## Команды бота
- `/start`, `/help`
- `/admin <секрет>` — выдача прав администратора
- `/my` — «Мои обращения» (то же, что и кнопка)
- `/export 2025-11-01..2025-11-10` — CSV в ответ
- `/broadcast "Текст"` — предпросмотр и подтверждение

## Структура
```
backend/
├── cmd/
│   └── main.go
├── internal/
│   ├── config.go
│   ├── database.go
│   ├── models.go
│   ├── services.go
│   ├── bot.go
│   └── web.go
├── migrations/
│   └── init.sql
├── .env
├── go.mod
└── README.md
```

## Лицензия
MIT
