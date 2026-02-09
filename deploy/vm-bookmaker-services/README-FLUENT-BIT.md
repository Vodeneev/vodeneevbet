# Логи VM контор (158.160.159.73) в Yandex Cloud Logging

VM создана в **другом аккаунте** Yandex Cloud, поэтому авторизация — по ключу сервисного аккаунта из аккаунта, где лежит лог-группа.

## 1. Ключ в аккаунте с лог-группой

В каталоге, где у тебя parser/core и лог-группа:

```bash
# Сервисный аккаунт с ролью logging.writer (тот же, что для parser, или новый)
yc iam key create --service-account-name <имя_сервисного_аккаунта> --output yc-logging-key.json
```

Роль: `logging.writer` на каталог. Сохрани `yc-logging-key.json` — его нужно положить на VM контор.

## 2. На VM 158.160.159.73

- Положить ключ, например: `/opt/vodeneevbet/bookmaker-services/yc-logging-key.json`
- `chmod 600 yc-logging-key.json`
- В `fluent-bit.conf`:
  - Заменить `folder_id` на реальный ID каталога (тот же, что в YC_FOLDER_ID у parser/core)
  - **ВАЖНО**: Заменить `<ЗАМЕНИ_НА_GROUP_ID_ИЛИ_УДАЛИ_ЭТУ_СТРОКУ>` на реальный `group_id` лог-группы
    - Получить ID: `yc logging group list --folder-id=<folder_id>`
    - Или из GitHub Secrets: `YC_LOG_GROUP_ID`
    - Без `group_id` плагин может падать при отправке логов

## 3. Fluent Bit + плагин yc-logging

По [документации](https://cloud.yandex.ru/docs/logging/operations/fluent-bit) установи Fluent Bit и плагин `fluent-bit-plugin-yandex`, затем используй конфиг из этой папки (`fluent-bit.conf`). Путь к ключу в конфиге должен совпадать с п. 2.

После запуска Fluent Bit логи контейнеров vodeneevbet-* будут уходить в указанную лог-группу.

**Примечание**: Если логи не появляются и Fluent Bit падает (ABRT), проверь:
1. Указан ли `group_id` в конфиге (рекомендуется указывать явно)
2. Правильность пути к ключу и его доступность для пользователя fluent-bit (обычно root)
3. Логи Fluent Bit: `sudo journalctl -u fluent-bit -n 100`
