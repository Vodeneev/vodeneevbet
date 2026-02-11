# Как проверить, что логи контор приходят в Yandex Cloud Logging

Логи bookmaker-services отправляются напрямую через Yandex Cloud Logging SDK (как и на других ВМ).

## 1. Через Yandex Cloud CLI (yc)

```bash
# Установи yc CLI если еще не установлен
# https://cloud.yandex.ru/docs/cli/quickstart

# Читай логи из каталога b1g7tng74uda3ahpg6oi
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --limit=50

# Фильтр по метке сервиса (fonbet)
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='service_label="bookmaker-fonbet"' --limit=20

# Фильтр по метке сервиса (pinnacle)
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='service_label="bookmaker-pinnacle"' --limit=20

# Фильтр по метке сервиса (pinnacle888)
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='service_label="bookmaker-pinnacle888"' --limit=20

# Фильтр по метке сервиса (marathonbet)
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='service_label="bookmaker-marathonbet"' --limit=20

# Поиск по маркеру из bookmaker-service
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='message:"Bookmaker service running on separate VM"' --limit=10

# Поиск тестового сообщения
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='message:"TEST_LOG_CHECK"' --limit=10
```

## 2. Через веб-интерфейс Yandex Cloud Logging

1. Открой [Yandex Cloud Console](https://console.cloud.yandex.ru/)
2. Перейди в **Cloud Logging**
3. Выбери каталог **b1g7tng74uda3ahpg6oi**
4. В поиске используй фильтры:

**Фильтр 1: по метке сервиса**
```
service_label="bookmaker-fonbet"
```
или
```
service_label="bookmaker-pinnacle"
```
или
```
service_label="bookmaker-pinnacle888"
```
или
```
service_label="bookmaker-marathonbet"
```

**Фильтр 2: по тексту из логов**
```
message:"Bookmaker service running on separate VM"
```
или
```
message:"TEST_LOG_CHECK"
```

**Фильтр 3: по метке проекта (все сервисы vodeneevbet)**
```
project_label="vodeneevbet"
```

**Фильтр 4: по времени (последние 10 минут)**
```
timestamp>="2026-02-09T11:40:00Z"
```

## 3. Что должно быть видно

Если логи приходят, ты увидишь записи с:
- **service_label**: `bookmaker-fonbet`, `bookmaker-pinnacle`, `bookmaker-pinnacle888` или `bookmaker-marathonbet`
- **project_label**: `vodeneevbet`
- **cluster_label**: `production`
- **message**: содержит логи из контейнеров (например, "Fonbet: Successfully processed matches...", "Pinnacle888: processing league...")
- **timestamp**: время записи

## 4. Если логи не появляются

Проверь на VM 158.160.159.73:

```bash
# Проверка переменных окружения в контейнере
ssh user@158.160.159.73 "sudo docker exec vodeneevbet-fonbet env | grep YC_"

# Логи контейнера (последние 50 строк)
ssh user@158.160.159.73 "sudo docker logs vodeneevbet-fonbet --tail 50"

# Проверка .env файла на VM
ssh user@158.160.159.73 "cat /opt/vodeneevbet/bookmaker-services/.env"

# Проверка статуса контейнеров
ssh user@158.160.159.73 "sudo docker ps --filter name=vodeneevbet"

# Проверка ошибок в логах (ищи сообщения об ошибках инициализации Yandex Cloud Logging)
ssh user@158.160.159.73 "sudo docker logs vodeneevbet-fonbet 2>&1 | grep -i 'error\|fail\|logging'"
```

## 5. Тестовое сообщение

Чтобы сгенерировать тестовое сообщение прямо сейчас:

```bash
ssh user@158.160.159.73 \
  "sudo docker exec vodeneevbet-fonbet sh -c 'echo \"TEST_LOG_CHECK_$(date +%Y%m%d_%H%M%S)\"'"
```

Затем подожди 5-10 секунд и проверь в Yandex Cloud Logging по фильтру:
```
message:"TEST_LOG_CHECK"
```

## 6. Проверка всех ВМ

Для проверки логов со всех ВМ используй скрипт `scripts/check-all-vm-logs.sh` (если создан) или проверяй вручную:

- **vm-parsers** (158.160.168.187): `service_label="parser"`
- **vm-core** (158.160.222.217): `service_label="calculator"` или `service_label="telegram-bot"`
- **vm-bookmaker-services** (158.160.159.73): `service_label="bookmaker-fonbet"`, `service_label="bookmaker-pinnacle"`, `service_label="bookmaker-pinnacle888"`, `service_label="bookmaker-marathonbet"`

Общий фильтр для всех сервисов:
```
project_label="vodeneevbet"
```
