# Как проверить, что логи контор приходят в Yandex Cloud Logging

## 1. Через Yandex Cloud CLI (yc)

```bash
# Установи yc CLI если еще не установлен
# https://cloud.yandex.ru/docs/cli/quickstart

# Читай логи из каталога b1g7tng74uda3ahpg6oi
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --limit=50

# Фильтр по resource_type (логи с VM контор)
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='resource_type="bookmaker-vm"' --limit=20

# Поиск тестового сообщения
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='message:"TEST_LOG_FROM_BOOKMAKER_VM"' --limit=10

# Поиск по маркеру из bookmaker-service
yc logging read --folder-id=b1g7tng74uda3ahpg6oi --filter='message:"Bookmaker service running on separate VM"' --limit=10
```

## 2. Через веб-интерфейс Yandex Cloud Logging

1. Открой [Yandex Cloud Console](https://console.cloud.yandex.ru/)
2. Перейди в **Cloud Logging**
3. Выбери каталог **b1g7tng74uda3ahpg6oi**
4. В поиске используй фильтры:

**Фильтр 1: по resource_type**
```
resource_type="bookmaker-vm"
```

**Фильтр 2: по тексту из логов**
```
message:"Bookmaker service running on separate VM"
```
или
```
message:"TEST_LOG_FROM_BOOKMAKER_VM"
```

**Фильтр 3: по имени контейнера (если Fluent Bit добавляет container_name)**
```
container_name:"vodeneevbet-fonbet"
```
или
```
container_name:"vodeneevbet-pinnacle"
```

**Фильтр 4: по времени (последние 10 минут)**
```
timestamp>="2026-02-09T11:40:00Z"
```

## 3. Что должно быть видно

Если логи приходят, ты увидишь записи с:
- **resource_type**: `bookmaker-vm`
- **message**: содержит логи из контейнеров (например, "Fonbet: Successfully processed matches...", "Pinnacle888: processing league...")
- **timestamp**: время записи
- Возможно поля: `container_name`, `log`, `stream` (stdout/stderr)

## 4. Если логи не появляются

Проверь на VM 158.160.159.73:

```bash
# Статус Fluent Bit
ssh -i ~/.ssh/github_actions_deploy -l dmgalochkin 158.160.159.73 "sudo systemctl status fluent-bit"

# Логи Fluent Bit (последние 50 строк)
ssh -i ~/.ssh/github_actions_deploy -l dmgalochkin 158.160.159.73 "sudo journalctl -u fluent-bit -n 50 --no-pager"

# Проверка отправки (ищи ошибки авторизации или сети)
ssh -i ~/.ssh/github_actions_deploy -l dmgalochkin 158.160.159.73 "sudo journalctl -u fluent-bit --since '10 minutes ago' | grep -i 'error\|fail\|auth'"

# Проверка ключа
ssh -i ~/.ssh/github_actions_deploy -l dmgalochkin 158.160.159.73 "sudo ls -la /opt/vodeneevbet/bookmaker-services/yc-logging-key.json"

# Проверка конфига
ssh -i ~/.ssh/github_actions_deploy -l dmgalochkin 158.160.159.73 "sudo cat /etc/fluent-bit/fluent-bit.conf | grep -A 5 OUTPUT"
```

## 5. Тестовое сообщение

Чтобы сгенерировать тестовое сообщение прямо сейчас:

```bash
ssh -i ~/.ssh/github_actions_deploy -l dmgalochkin 158.160.159.73 \
  "sudo docker exec vodeneevbet-fonbet sh -c 'echo \"TEST_LOG_CHECK_$(date +%Y%m%d_%H%M%S)\"'"
```

Затем подожди 5-10 секунд и проверь в Yandex Cloud Logging по фильтру:
```
message:"TEST_LOG_CHECK"
```
