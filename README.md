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

`docker compose up -d --build` собирает и запускает бот вместе с приватным
Fragrantica-sidecar; наружу sidecar не публикуется. SQLite хранится в volume
`perfume-data`. Токен не коммитится. Healthcheck выполняется самим бинарником.

Обогащение данными Fragrantica работает в режиме best effort: Cloudflare может
заблокировать запрос или изменить страницу. Это не мешает запуску и готовности
бота — поиск цен продолжит работать без карточки Fragrantica.

## Реестр Fragrantica

```bash
go run ./cmd/shopregistry import --input-dir data/fragrantica/topic-235155 --pages 16
go run ./cmd/shopregistry list
go run ./cmd/shopregistry evidence --domain artparfum.ru
```

Импорт добавляет evidence, но не повышает trust новых доменов автоматически.
