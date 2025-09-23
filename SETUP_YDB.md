# Настройка YDB с аутентификацией

## 1. Создание Service Account в Yandex Cloud

1. Перейдите в [консоль Yandex Cloud](https://console.cloud.yandex.ru/)
2. Выберите ваш каталог
3. Перейдите в **IAM & Access** → **Service accounts**
4. Нажмите **Create service account**
5. Заполните:
   - **Name**: `arb-finder-service`
   - **Description**: `Service account for arbitrage finder`
6. Нажмите **Create**

## 2. Назначение ролей

1. Нажмите на созданный Service Account
2. Перейдите на вкладку **Access bindings**
3. Нажмите **Assign access bindings**
4. Добавьте роли:
   - **YDB Database User** (для работы с базой данных)
   - **YDB Database Viewer** (для просмотра метаданных)
5. Нажмите **Save**

## 3. Создание ключа аутентификации

1. В том же Service Account перейдите на вкладку **Keys**
2. Нажмите **Create new key** → **Create authorized key**
3. Выберите алгоритм **RSA_2048**
4. Нажмите **Create**
5. **Скачайте JSON файл** - это ваш ключ аутентификации

## 4. Сохранение ключа

1. Сохраните скачанный JSON файл как `keys/service-account-key.json`
2. Убедитесь, что файл имеет правильные права доступа:
   ```bash
   chmod 600 keys/service-account-key.json
   ```

## 5. Тестирование

После настройки запустите тест:

```bash
go run test_ydb_full.go
```

Если все настроено правильно, вы увидите:
- Успешное подключение к YDB
- Создание таблиц
- Сохранение тестовых данных
- Получение данных из YDB

## 6. Обновление парсера

После успешного тестирования обновите парсер для использования полного YDB клиента:

```go
// В internal/parser/main.go замените:
ydbClient, err := storage.NewYDBSimpleClient(&cfg.YDB)
// На:
ydbClient, err := storage.NewYDBFullClient(&cfg.YDB)
```
