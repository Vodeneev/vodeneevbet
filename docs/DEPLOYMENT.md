# Руководство по деплою

Система автоматического деплоя обеспечивает, что актуальная версия сервисов всегда запущена на виртуальных машинах.

## Архитектура деплоя

- **vm-parsers** (158.160.197.172): Запускает Parser Service
- **vm-core-services** (158.160.200.253): Запускает Calculator Service

## Быстрый старт

### Деплой всех сервисов

**Linux/Mac (bash):**
```bash
make deploy-all
# или
./scripts/deploy/deploy-all.sh
```

### Переменные окружения для ручного деплоя

Скрипты `scripts/deploy/deploy-*.sh` ожидают, что вы укажете, откуда тянуть образы:

```bash
export IMAGE_OWNER="vodeneev"   # namespace в GHCR (обычно владелец репозитория)
export IMAGE_TAG="main"         # или конкретный тег

# если образы приватные:
export GHCR_TOKEN="..."         # PAT с read:packages
export GHCR_USERNAME="vodeneev" # опционально

# если нужно синкнуть ./keys на VM (по умолчанию скрипт не трогает ключи):
export COPY_KEYS=1
```

### Деплой отдельных сервисов

**Parser Service:**
```bash
make deploy-parsers
# или
./scripts/deploy/deploy-parsers.sh
```

**Core Services (Calculator):**
```bash
make deploy-core
# или
./scripts/deploy/deploy-core-services.sh
```

## Что делает скрипт деплоя

Актуальные скрипты деплоя используют **Docker Compose на VM** (без rsync кода и без сборки Go на сервере):

1. **Проверка подключения** — SSH до VM
2. **Подготовка директорий** — `/opt/vodeneevbet/{parsers,core}`
3. **Загрузка `docker-compose.yml`** — из `deploy/vm-*/docker-compose.yml`
4. **Синк `configs/`** — кладётся рядом с compose (ключи по умолчанию не трогаем)
5. **Pull & up** — `docker compose pull && docker compose up -d`

## Управление сервисами

### Проверка статуса

```bash
make status
```

Или вручную:
```bash
# Parser
ssh vm-parsers "sudo docker ps --filter name=vodeneevbet-parser"

# Calculator
ssh vm-core-services "sudo docker ps --filter name=vodeneevbet-calculator"
```

### Просмотр логов

```bash
# Parser logs
make logs-parser
# или
ssh vm-parsers "sudo docker logs -f vodeneevbet-parser"

# Calculator logs
make logs-calculator
# или
ssh vm-core-services "sudo docker logs -f vodeneevbet-calculator"
```

### Остановка/Запуск

```bash
# Остановить все сервисы
make stop-all

# Запустить все сервисы
make start-all
```

## Legacy: systemd деплой

Старые systemd-скрипты сохранены для истории в `scripts/deploy/legacy/`, но **не рекомендуются**:
- `scripts/deploy/legacy/deploy-parsers.systemd.sh`
- `scripts/deploy/legacy/deploy-core-services.systemd.sh`

## Требования

### На удаленных машинах должно быть установлено:

1. **Docker**
2. **Docker Compose** (плагин `docker compose` или `docker-compose`)
3. **SSH доступ** с правами sudo для пользователя `vodeneevm`

### На локальной машине:

1. **SSH клиент** с настроенным доступом к VM
2. **rsync** (для синка `configs/`)
3. **Make** (опционально)

## Автоматизация деплоя через CI/CD

### GitHub Actions

Workflow `.github/workflows/deploy.yml`:

- собирает образы `parser` и `calculator` в GHCR
- по SSH деплоит на две VM через `docker compose`

**Secrets (обязательные):**
- `SSH_PRIVATE_KEY` — приватный ключ для SSH (без passphrase)
- `VM_PARSERS_HOST` — IP/DNS vm-parsers
- `VM_CORE_HOST` — IP/DNS vm-core-services

**Secrets (опциональные):**
- `VM_USER` — пользователь на VM (если отличается)
- `GHCR_TOKEN` + `GHCR_USERNAME` — если образы в GHCR приватные (PAT с `read:packages`)

## Troubleshooting

### Проблема: "Cannot connect to VM"

**Решение:**
1. Проверьте SSH подключение: `ssh vm-parsers`
2. Убедитесь, что SSH config настроен правильно
3. Проверьте доступность порта 22: `Test-NetConnection -ComputerName 158.160.197.172 -Port 22`

### Проблема: "Permission denied"

**Решение:**
1. Убедитесь, что пользователь `vodeneevm` имеет права sudo
2. Проверьте права на директорию: `ssh vm-parsers "ls -la /opt/vodeneevbet"`

### Проблема: "go: command not found"

**Решение:**
Для compose-деплоя Go на VM не нужен. Убедитесь, что на VM установлены Docker и Docker Compose.

### Проблема: Сервис не запускается

**Решение:**
1. Проверьте логи: `ssh vm-parsers "sudo docker logs --tail=200 vodeneevbet-parser"`
2. Проверьте конфигурацию: `ssh vm-parsers "cat /opt/vodeneevbet/parsers/configs/local.yaml"`
3. Проверьте директорию: `ssh vm-parsers "ls -la /opt/vodeneevbet/parsers"`

## Структура файлов на VM

После деплоя структура на VM выглядит так:

```
/opt/vodeneevbet/
├── parsers/
│   ├── docker-compose.yml
│   ├── .env
│   ├── configs/
│   └── keys/   # обычно вручную (секреты)
└── core/
    ├── docker-compose.yml
    ├── .env
    ├── configs/
    └── keys/   # обычно вручную (секреты)
```

## Безопасность

- SSH ключи должны быть защищены
- Service account ключи для YDB не должны попадать в git
- Используйте `.gitignore` для исключения чувствительных файлов
- Регулярно обновляйте зависимости: `go mod tidy`
