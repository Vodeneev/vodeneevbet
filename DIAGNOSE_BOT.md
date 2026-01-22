# Диагностика Telegram бота

## Быстрая проверка (выполните на сервере)

```bash
# 1. Проверить статус контейнера
sudo docker ps -a --filter "name=vodeneevbet-telegram-bot"

# 2. Посмотреть последние логи
sudo docker logs --tail=50 vodeneevbet-telegram-bot

# 3. Проверить ошибки в логах
sudo docker logs vodeneevbet-telegram-bot 2>&1 | grep -i "error\|panic\|fatal\|failed" | tail -20

# 4. Проверить переменные окружения
sudo docker exec vodeneevbet-telegram-bot env | grep -E "TELEGRAM_BOT_TOKEN|CALCULATOR_URL"

# 5. Проверить подключение к calculator
sudo docker exec vodeneevbet-telegram-bot wget -q --spider --timeout=3 http://calculator:8080/health && echo "OK" || echo "FAILED"

# 6. Проверить, получает ли бот обновления (должны быть логи "Received message")
sudo docker logs vodeneevbet-telegram-bot 2>&1 | grep -i "received message\|starting updates\|ready to receive"
```

## Полная диагностика

Загрузите скрипт на сервер и выполните:

```bash
# На вашем локальном компьютере
scp scripts/diagnose-telegram-bot.sh user@vm-host:/tmp/

# На сервере
ssh user@vm-host
sudo bash /tmp/diagnose-telegram-bot.sh
```

## Частые проблемы

1. **Бот не запущен** - проверьте `docker ps`
2. **Неверный токен** - проверьте переменную `TELEGRAM_BOT_TOKEN`
3. **Бот не получает обновления** - проверьте логи на наличие "Received message"
4. **Ошибки отправки** - проверьте логи на "Failed to send"
