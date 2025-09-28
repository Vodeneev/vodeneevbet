# YDB TTL Cleanup - Автоматическая очистка устаревших данных

## Обзор

Система автоматической очистки устаревших коэффициентов в YDB с использованием встроенного TTL (Time To Live) механизма.

## Принцип работы

1. **TTL колонка**: `match_time` - время матча
2. **Правило удаления**: Коэффициенты удаляются через заданное время после матча
3. **Автоматизация**: YDB сам удаляет устаревшие данные в фоновом режиме

## Конфигурация

### В configs/local.yaml:

```yaml
ydb:
  ttl:
    enabled: true                    # Включить TTL
    expire_after: 4h                 # Удалять через 4 часа после матча
    auto_setup: true                 # Автоматически настраивать при запуске
```

### Параметры TTL:

- **enabled**: Включить/выключить TTL
- **expire_after**: Время жизни данных (например: `4h`, `2h30m`, `1d`)
- **auto_setup**: Автоматическая настройка при инициализации клиента

## Использование

### 1. Автоматическая настройка

TTL настраивается автоматически при запуске парсера, если `auto_setup: true`.

### 2. Ручное управление TTL

```bash
# Проверить статус TTL
cd internal/ttl-manager
go run main.go -action=status

# Настроить TTL (удалять через 2 часа)
go run main.go -action=setup -expire-after=2h

# Отключить TTL
go run main.go -action=disable
```

### 3. Программное управление

```go
// Настройка TTL
err := ydbClient.SetupTTL(ctx, 4*time.Hour)

// Проверка настроек
ttlSettings, err := ydbClient.GetTTLSettings(ctx)

// Отключение TTL
err := ydbClient.DisableTTL(ctx)
```

## Преимущества TTL подхода

✅ **Автоматическая очистка** - не требует дополнительного кода  
✅ **Производительность** - фоновая операция, не блокирует запросы  
✅ **Надежность** - встроенная функция YDB  
✅ **Гибкость** - настраиваемые интервалы  
✅ **Экономия места** - автоматическое освобождение дискового пространства  

## Рекомендуемые настройки

### Для продакшена:
```yaml
ttl:
  enabled: true
  expire_after: 4h    # 4 часа после матча
  auto_setup: true
```

### Для тестирования:
```yaml
ttl:
  enabled: true
  expire_after: 1h    # 1 час для быстрого тестирования
  auto_setup: true
```

### Для отладки:
```yaml
ttl:
  enabled: false      # Отключить TTL
  auto_setup: false
```

## Мониторинг

### Проверка статуса TTL:
```bash
cd internal/ttl-manager
go run main.go -action=status
```

### Логи YDB:
```
YDB: TTL configured successfully - odds will be deleted 4h0m0s after match_time
```

## Ограничения

1. **TTL колонка**: Должна быть типа `Timestamp`, `Date`, `Datetime`, `Uint32`, `Uint64`
2. **Одна TTL колонка**: Нельзя указать несколько TTL колонок
3. **Внешнее хранилище**: Поддерживается только Object Storage для вытеснения

## Troubleshooting

### TTL не работает:
1. Проверьте, что `match_time` содержит корректные временные метки
2. Убедитесь, что TTL включен: `enabled: true`
3. Проверьте статус: `go run main.go -action=status`

### Ошибка настройки TTL:
```
YDB: Warning - failed to setup TTL: ...
```
- Проверьте права доступа к YDB
- Убедитесь, что таблица `odds` существует
- Проверьте корректность конфигурации

## Миграция существующих данных

TTL можно настроить для существующих таблиц без потери данных:

```bash
# Настроить TTL для существующей таблицы
go run main.go -action=setup -expire-after=4h
```

Существующие данные будут удалены согласно новым TTL правилам.
