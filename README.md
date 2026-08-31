# Perfume Price Bot

Telegram-бот сравнивает точные варианты парфюмерии в Randewoo, Allure Parfum, Orental, Духи.рф и Aroma Butik. Доставка в цену не входит.

## Local

```bash
cp .env.example .env
# заполнить TELEGRAM_BOT_TOKEN
set -a; source .env; set +a
go run ./cmd/perfumebot
```

## Docker on Yandex Cloud VPS

```bash
cp .env.example .env
# заполнить TELEGRAM_BOT_TOKEN
docker compose up -d --build
docker compose ps
```

SQLite хранится в volume `perfume-data`. Токен не коммитится. Healthcheck выполняется самим бинарником.

## Реестр Fragrantica

```bash
go run ./cmd/shopregistry import --input-dir data/fragrantica/topic-235155 --pages 16
go run ./cmd/shopregistry list
go run ./cmd/shopregistry evidence --domain artparfum.ru
```

Импорт добавляет evidence, но не повышает trust новых доменов автоматически.
