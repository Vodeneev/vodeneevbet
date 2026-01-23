# Настройка подключения к Yandex Cloud Managed PostgreSQL

## Информация о вашем кластере

- **Имя кластера**: postgresql106
- **ID кластера**: c9q4ti8th22kbmunjceg
- **Версия**: PostgreSQL 16
- **Окружение**: PRODUCTION
- **Хосты**: 
  - `rc1a-fec715cq0rept3kd.mdb.yandexcloud.net` (read-write)
  - `rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net` (read-write)
- **Порт**: 6432
- **Пользователь**: vodeneevbet

## Получение данных для подключения

### 1. Хосты базы данных

Ваш кластер имеет несколько хостов для отказоустойчивости. Используйте оба хоста через запятую в DSN строке:

```
rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net
```

### 2. Получите FQDN хостов (если нужно обновить)

1. Откройте [Yandex Cloud Console](https://console.cloud.yandex.ru)
2. Перейдите в **Managed Service for PostgreSQL**
3. Выберите кластер **postgresql106**
4. Перейдите на вкладку **Хосты**
5. Скопируйте **FQDN** всех хостов (через запятую для отказоустойчивости)

### 2. Создайте базу данных (если еще не создана)

1. В консоли кластера перейдите на вкладку **Базы данных**
2. Создайте базу данных (например, `arb_db` или используйте существующую `db`)

### 3. Настройте права доступа

Убедитесь, что пользователь `vodeneevbet` имеет права на создание таблиц в базе данных.

### 4. Получите пароль пользователя

Пароль пользователя `vodeneevbet` должен быть установлен в консоли Yandex Cloud или при создании пользователя.

## Настройка подключения

### Вариант 1: Через переменную окружения (рекомендуется)

```bash
export POSTGRES_DSN="host=<host1>,<host2> port=6432 user=<username> password=<password> dbname=<dbname> sslmode=require target_session_attrs=read-write"
```

**Пример для вашего кластера:**
```bash
export POSTGRES_DSN="host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevbet password=your_password dbname=db sslmode=require target_session_attrs=read-write"
```

### Вариант 2: Через конфигурационный файл

Отредактируйте `configs/production.yaml`:

```yaml
postgres:
  dsn: "host=<host1>,<host2> port=6432 user=<username> password=<password> dbname=<dbname> sslmode=require target_session_attrs=read-write"
```

**Пример для вашего кластера:**
```yaml
postgres:
  dsn: "host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevbet password=your_password dbname=db sslmode=require target_session_attrs=read-write"
```

### Важные параметры

- **Несколько хостов**: Указывайте все хосты через запятую для отказоустойчивости
- **target_session_attrs=read-write**: Обеспечивает подключение к хосту с правами записи
- **sslmode=require**: Обязательный SSL (для Go приложений достаточно `require`, не нужен `verify-full`)

## Тестирование подключения (опционально)

Для тестирования подключения через `psql`:

### 1. Установите PostgreSQL клиент

```bash
sudo apt update && sudo apt install --yes postgresql-client
```

### 2. Установите сертификат (для psql с verify-full)

```bash
mkdir -p ~/.postgresql && \
wget "https://storage.yandexcloud.net/cloud-certs/CA.pem" \
    --output-document ~/.postgresql/root.crt && \
chmod 0655 ~/.postgresql/root.crt
```

### 3. Подключитесь к базе данных

```bash
psql "host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net \
    port=6432 \
    sslmode=verify-full \
    dbname=db \
    user=vodeneevbet \
    target_session_attrs=read-write"
```

### 4. Проверьте подключение

```sql
SELECT version();
```

**Примечание**: Для Go приложений сертификат не требуется, достаточно `sslmode=require`. Сертификат нужен только для `psql` с `sslmode=verify-full`.

## Проверка подключения

После настройки запустите калькулятор:

```bash
go run ./cmd/calculator -config configs/production.yaml
```

В логах должно появиться:
```
calculator: PostgreSQL diff storage initialized successfully
```

## Безопасность

⚠️ **Важно**: Никогда не коммитьте пароли в репозиторий!

- Используйте переменные окружения для паролей
- Добавьте `.env` файл в `.gitignore`
- Используйте секреты в CI/CD системах

## Troubleshooting

### Ошибка: "connection refused"
- Проверьте, что используете правильный порт (6432, не 5432)
- Убедитесь, что FQDN указан правильно

### Ошибка: "SSL required"
- Добавьте `sslmode=require` в DSN строку

### Ошибка: "authentication failed"
- Проверьте имя пользователя и пароль
- Убедитесь, что пользователь существует в кластере

### Ошибка: "database does not exist"
- Создайте базу данных в консоли Yandex Cloud
- Проверьте, что имя базы данных указано правильно
