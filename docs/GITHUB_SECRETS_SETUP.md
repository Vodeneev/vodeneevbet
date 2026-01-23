# Настройка GitHub Secrets для деплоя

Для работы асинхронной обработки и уведомлений в Telegram необходимо настроить секреты в GitHub Actions.

## Необходимые секреты

### 1. POSTGRES_DSN (обязательно для async режима)

Строка подключения к Yandex Cloud Managed PostgreSQL.

**Формат:**
```
host=<host1>,<host2> port=6432 user=<username> password=<password> dbname=<dbname> sslmode=require target_session_attrs=read-write
```

**Пример для вашего кластера:**
```
host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevm password=your_secure_password dbname=db sslmode=require target_session_attrs=read-write
```

**⚠️ Важно:** Пользователь должен быть `vodeneevm` (не `vodeneevbet`!)

**Важно:**
- Указывайте все хосты через запятую для отказоустойчивости
- Параметр `target_session_attrs=read-write` обеспечивает подключение к хосту с правами записи

**Как получить:**
1. Откройте [Yandex Cloud Console](https://console.cloud.yandex.ru)
2. Managed Service for PostgreSQL → кластер **postgresql106**
3. Вкладка **Хосты** → скопируйте FQDN
4. Вкладка **Пользователи** → создайте/используйте пользователя
5. Вкладка **Базы данных** → создайте/используйте базу данных

### 2. TELEGRAM_BOT_TOKEN (обязательно для уведомлений)

Токен вашего Telegram бота.

**Как получить:**
1. Откройте [@BotFather](https://t.me/botfather) в Telegram
2. Отправьте `/newbot` и следуйте инструкциям
3. Скопируйте токен (формат: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`)

### 3. TELEGRAM_CHAT_ID (обязательно для уведомлений)

ID чата, куда будут отправляться уведомления.

**Как получить (выберите один способ):**

**Способ 1: Через @userinfobot (для личных чатов) - самый простой**
1. Откройте [@userinfobot](https://t.me/userinfobot) в Telegram
2. Отправьте `/start`
3. Бот покажет ваш Chat ID (число, например: `123456789`)

**Способ 2: Через @RawDataBot (для групп)**
1. Создайте группу или откройте существующую
2. Добавьте [@RawDataBot](https://t.me/RawDataBot) в группу
3. Отправьте любое сообщение в группу
4. Бот ответит JSON с данными - найдите `chat.id` (это и есть Chat ID группы)

**Способ 3: Через @getidsbot**
1. Откройте [@getidsbot](https://t.me/getidsbot) в Telegram
2. Отправьте `/start`
3. Бот покажет ваш Chat ID

**Способ 4: Через вашего бота (если уже запущен)**
1. Напишите вашему боту любое сообщение (например, `/start`)
2. Проверьте логи бота - там будет Chat ID из входящего сообщения
3. Или временно добавьте логирование в код бота для вывода Chat ID

**Примечание:** 
- Для личных чатов используйте ваш User ID (получите через @userinfobot)
- Для групп используйте Group ID (получите через @RawDataBot)
- Chat ID - это число (может быть отрицательным для групп)

## Настройка секретов в GitHub

### Через веб-интерфейс:

1. Откройте ваш репозиторий на GitHub
2. Перейдите в **Settings** → **Secrets and variables** → **Actions**
3. Если секрет уже существует:
   - Найдите нужный секрет в списке (например, `POSTGRES_DSN`)
   - Нажмите на него, затем нажмите **Update** (или **Edit**)
   - Вставьте новое значение
   - Нажмите **Update secret**
4. Если секрета нет:
   - Нажмите **New repository secret**
   - Добавьте каждый секрет:

     - **Name**: `POSTGRES_DSN`
     - **Secret**: `host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevm password=your_password dbname=db sslmode=require target_session_attrs=read-write`
     
     ⚠️ **Важно:** Используйте пользователя `vodeneevm` (не `vodeneevbet`!)

     - **Name**: `TELEGRAM_BOT_TOKEN`
     - **Secret**: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`

     - **Name**: `TELEGRAM_CHAT_ID`
     - **Secret**: `123456789`

   - Нажмите **Add secret**

### Через GitHub CLI:

```bash
# Установите GitHub CLI если еще не установлен
# brew install gh  # macOS
# apt install gh   # Linux

# Авторизуйтесь
gh auth login

# Добавьте или обновите секреты (команда set обновит существующий секрет)
gh secret set POSTGRES_DSN --body "host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevm password=your_password dbname=db sslmode=require target_session_attrs=read-write"
gh secret set TELEGRAM_BOT_TOKEN --body "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
gh secret set TELEGRAM_CHAT_ID --body "123456789"
```

## Проверка секретов

После добавления секретов:

1. Запустите деплой (push в `main` или через **Actions** → **Run workflow**)
2. Проверьте логи деплоя - не должно быть ошибок о missing secrets
3. После деплоя проверьте логи калькулятора:
   ```bash
   ssh vm-core-services 'sudo docker logs vodeneevbet-calculator'
   ```
4. Должны появиться строки:
   ```
   calculator: PostgreSQL diff storage initialized successfully
   calculator: telegram notifier initialized for chat <ID>
   calculator: starting async processing with interval 30s
   ```

## Существующие секреты

У вас уже настроены следующие секреты (не трогайте их):
- `VM_CORE_HOST` - хост VM для core сервисов
- `VM_PARSERS_HOST` - хост VM для парсеров
- `SSH_PRIVATE_KEY` - SSH ключ для подключения к VM
- `GHCR_TOKEN` - токен для GitHub Container Registry
- `GHCR_USERNAME` - username для GHCR (опционально)

## Безопасность

⚠️ **Важно:**
- Никогда не коммитьте секреты в код
- Не логируйте секреты в консоль
- Регулярно обновляйте пароли
- Используйте разные пароли для разных окружений (dev/staging/prod)

## Troubleshooting

### Ошибка: "postgres DSN is required"
- Убедитесь, что секрет `POSTGRES_DSN` добавлен в GitHub Secrets
- Проверьте, что значение не пустое

### Ошибка: "failed to initialize PostgreSQL storage"
- Проверьте правильность DSN строки
- Убедитесь, что FQDN, порт, пользователь, пароль и имя БД указаны верно
- Проверьте, что SSL включен (`sslmode=require`)

### Ошибка: "telegram notifier not initialized"
- Проверьте, что `TELEGRAM_BOT_TOKEN` и `TELEGRAM_CHAT_ID` добавлены
- Убедитесь, что токен валидный (можно проверить через [@BotFather](https://t.me/botfather))
