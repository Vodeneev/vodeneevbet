package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type RedisClient struct {
	client *redis.Client
}

func NewRedisClient(addr, password string, db int) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// Проверяем подключение
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisClient{client: client}, nil
}

// StoreOdd сохраняет коэффициент в Redis
func (r *RedisClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	key := fmt.Sprintf("odds:%s:%s:%s", odd.Bookmaker, odd.MatchID, odd.Market)
	
	data, err := json.Marshal(odd)
	if err != nil {
		return fmt.Errorf("failed to marshal odd: %w", err)
	}

	// Устанавливаем TTL в 1 час
	return r.client.Set(ctx, key, data, time.Hour).Err()
}

// GetOddsByMatch получает все коэффициенты для матча
func (r *RedisClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	pattern := fmt.Sprintf("odds:*:%s:*", matchID)
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}

	var odds []*models.Odd
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			continue // Пропускаем невалидные ключи
		}

		var odd models.Odd
		if err := json.Unmarshal([]byte(data), &odd); err != nil {
			continue // Пропускаем невалидные данные
		}

		odds = append(odds, &odd)
	}

	return odds, nil
}

// GetAllMatches получает все доступные матчи
func (r *RedisClient) GetAllMatches(ctx context.Context) ([]string, error) {
	pattern := "odds:*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}

	matches := make(map[string]bool)
	for _, key := range keys {
		// Извлекаем matchID из ключа (формат: odds:bookmaker:matchID:market)
		parts := splitKey(key)
		if len(parts) >= 3 {
			matches[parts[2]] = true
		}
	}

	var result []string
	for matchID := range matches {
		result = append(result, matchID)
	}

	return result, nil
}

// Close закрывает соединение с Redis
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// Вспомогательная функция для разбора ключа
func splitKey(key string) []string {
	// Простая реализация разбора ключа
	// В реальном проекте лучше использовать более надежный парсер
	var parts []string
	var current string
	for _, char := range key {
		if char == ':' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
