# Подключение к виртуальным машинам

## Настройка SSH

SSH конфигурация уже настроена в `~/.ssh/config`. Теперь вы можете подключаться к машинам простыми командами:

```bash
# Подключение к VM с парсерами
ssh vm-parsers

# Подключение к VM с core сервисами
ssh vm-core-services
```

## Информация о машинах

### vm-parsers
- **IP**: 158.160.197.172
- **Пользователь**: vodeneevm
- **Назначение**: Запуск парсеров букмекеров

### vm-core-services
- **IP**: 158.160.200.253
- **Пользователь**: vodeneevm
- **Назначение**: Запуск core сервисов (API, Calculator)

## Тестирование подключения

Запустите скрипт для проверки доступности машин:

```powershell
.\scripts\test-connections.ps1
```

Или используйте прямые команды:

```powershell
# Проверка vm-parsers
Test-NetConnection -ComputerName 158.160.197.172 -Port 22

# Проверка vm-core-services
Test-NetConnection -ComputerName 158.160.200.253 -Port 22
```

## Быстрое подключение

Используйте готовые скрипты:

```powershell
# Подключение к парсерам
.\scripts\connect-parsers.ps1

# Подключение к core сервисам
.\scripts\connect-core-services.ps1
```

## Настройка SSH ключей (опционально)

Если хотите использовать SSH ключи вместо пароля:

1. Сгенерируйте ключ (если еще нет):
```bash
ssh-keygen -t rsa -b 4096 -C "your_email@example.com"
```

2. Скопируйте публичный ключ на обе машины:
```bash
ssh-copy-id vm-parsers
ssh-copy-id vm-core-services
```

3. Раскомментируйте строку `IdentityFile` в `~/.ssh/config`

## Troubleshooting

### Проблема: Connection refused
- Проверьте, что SSH сервер запущен на удаленных машинах
- Убедитесь, что порт 22 открыт в firewall

### Проблема: Permission denied
- Проверьте правильность имени пользователя
- Убедитесь, что у вас есть доступ к машине
- Проверьте SSH ключи, если используете

### Проблема: Host key verification failed
- Удалите старый ключ: `ssh-keygen -R 158.160.197.172`
- Подключитесь заново и подтвердите ключ хоста
