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
- В `fluent-bit.conf` заменить `<FOLDER_ID_ГДЕ_ЛОГ_ГРУППА>` на реальный `folder_id` каталога (тот же, что в YC_FOLDER_ID у parser/core)

## 3. Fluent Bit + плагин yc-logging

По [документации](https://cloud.yandex.ru/docs/logging/operations/fluent-bit) установи Fluent Bit и плагин `fluent-bit-plugin-yandex`, затем используй конфиг из этой папки (`fluent-bit.conf`). Путь к ключу в конфиге должен совпадать с п. 2.

После запуска Fluent Bit логи контейнеров vodeneevbet-* будут уходить в ту же лог-группу каталога (по умолчанию для этого folder_id) или можно задать `group_id` в конфиге OUTPUT.
