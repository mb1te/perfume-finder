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
