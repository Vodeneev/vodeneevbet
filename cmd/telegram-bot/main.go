package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/logging"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	defaultCalculatorURL = "http://localhost:8080"
)

type BotConfig struct {
	Token          string
	CalculatorURL  string
	UpdateTimeout  int
	AllowedUserIDs []int64 // Optional: restrict access to specific users
}

func main() {
	var token string
	var calculatorURL string
	var allowedUsers string
	var configPath string

	flag.StringVar(&token, "token", "", "Telegram bot token (required, or set TELEGRAM_BOT_TOKEN env var)")
	flag.StringVar(&calculatorURL, "calculator-url", defaultCalculatorURL, "Calculator service URL")
	flag.StringVar(&allowedUsers, "allowed-users", "", "Comma-separated list of allowed user IDs (optional)")
	flag.StringVar(&configPath, "config", "", "Path to config file (optional, for logging setup)")
	flag.Parse()

	// Initialize logging if config is provided
	if configPath != "" {
		if cfg, err := config.Load(configPath); err == nil {
			_, _ = logging.SetupLogger(&cfg.Logging, "telegram-bot")
		}
	}

	// Get token from environment if not provided via flag
	if token == "" {
		token = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if token == "" {
		slog.Error("Telegram bot token is required. Set -token flag or TELEGRAM_BOT_TOKEN env var")
		os.Exit(1)
	}

	// Get calculator URL from environment if not provided
	if calculatorURL == defaultCalculatorURL {
		if envURL := os.Getenv("CALCULATOR_URL"); envURL != "" {
			calculatorURL = envURL
		}
	}

	botConfig := BotConfig{
		Token:         token,
		CalculatorURL: calculatorURL,
		UpdateTimeout: 60,
	}

	// Parse allowed users if provided
	if allowedUsers != "" {
		userIDs := strings.Split(allowedUsers, ",")
		for _, idStr := range userIDs {
			id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
			if err == nil {
				botConfig.AllowedUserIDs = append(botConfig.AllowedUserIDs, id)
			}
		}
	}

	slog.Info("Starting Telegram bot...")
	slog.Info("Calculator URL", "url", botConfig.CalculatorURL)

	bot, err := tgbotapi.NewBotAPI(botConfig.Token)
	if err != nil {
		slog.Error("Failed to create bot", "error", err)
		os.Exit(1)
	}

	bot.Debug = false

	// Test bot connection by getting bot info
	botInfo, err := bot.GetMe()
	if err != nil {
		slog.Error("Failed to get bot info (token might be invalid)", "error", err)
		os.Exit(1)
	}

	slog.Info("Authorized on account", "username", botInfo.UserName, "id", botInfo.ID)
	slog.Info("Bot is ready to receive messages")
	slog.Debug("Bot token", "token_preview", fmt.Sprintf("%s...%s", botConfig.Token[:10], botConfig.Token[len(botConfig.Token)-4:]))

	u := tgbotapi.NewUpdate(0)
	u.Timeout = botConfig.UpdateTimeout

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, stopping bot...")
		cancel()
	}()

	// Start bot handler
	slog.Info("Starting updates channel...")
	updates := bot.GetUpdatesChan(u)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("PANIC in bot handler", "error", r)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping bot updates...")
				bot.StopReceivingUpdates()
				return
			case update := <-updates:
				// Handle each update in a separate goroutine to prevent one error from blocking others
				go func(upd tgbotapi.Update) {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("PANIC handling message", "user_id", upd.Message.From.ID, "error", r)
						}
					}()

					if upd.Message == nil {
						return
					}

					slog.Debug("Received message", "user_id", upd.Message.From.ID, "text", upd.Message.Text)

					// Check if user is allowed (if restrictions are set)
					if len(botConfig.AllowedUserIDs) > 0 {
						allowed := false
						for _, id := range botConfig.AllowedUserIDs {
							if upd.Message.From.ID == id {
								allowed = true
								break
							}
						}
						if !allowed {
							msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Access denied. You are not authorized to use this bot.")
							if _, err := bot.Send(msg); err != nil {
								slog.Error("Failed to send access denied message", "user_id", upd.Message.From.ID, "error", err)
							}
							return
						}
					}

					handleMessage(bot, upd.Message, botConfig)
				}(update)
			}
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	slog.Info("Telegram bot stopped")
}

func handleMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message, config BotConfig) {
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}

	// Handle commands
	if strings.HasPrefix(text, "/") {
		parts := strings.Fields(text)
		command := strings.ToLower(parts[0])

		switch command {
		case "/start":
			startAsyncProcessing(bot, message.Chat.ID, config)
		case "/help":
			sendHelpMessage(bot, message.Chat.ID)
		case "/top":
			limit := 5
			if len(parts) > 1 {
				if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
					limit = n
				}
			}
			fetchAndSendDiffs(bot, message.Chat.ID, config, limit, "")
		case "/live":
			limit := 5
			if len(parts) > 1 {
				if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
					limit = n
				}
			}
			fetchAndSendDiffs(bot, message.Chat.ID, config, limit, "live")
		case "/upcoming":
			limit := 5
			if len(parts) > 1 {
				if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
					limit = n
				}
			}
			fetchAndSendDiffs(bot, message.Chat.ID, config, limit, "upcoming")
		case "/overlays":
			limit := 10
			if len(parts) > 1 {
				if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
					limit = n
				}
			}
			fetchAndSendLineMovements(bot, message.Chat.ID, config, limit)
		case "/stop":
			stopAsyncProcessing(bot, message.Chat.ID, config)
		case "/stop_values":
			stopAlertType(bot, message.Chat.ID, config, "values", "Алерты по валуям отключены.")
		case "/stop_overlays":
			stopAlertType(bot, message.Chat.ID, config, "overlays", "Алерты по прогрузам отключены.")
		default:
			msg := tgbotapi.NewMessage(message.Chat.ID, "Unknown command. Use /help to see available commands.")
			if _, err := bot.Send(msg); err != nil {
				slog.Error("Failed to send unknown command message", "user_id", message.From.ID, "error", err)
			}
		}
	} else {
		// Try to parse as inline query for diffs
		// Format: "top 10" or "live 5" or "upcoming 3"
		parts := strings.Fields(strings.ToLower(text))
		if len(parts) >= 1 {
			switch parts[0] {
			case "top":
				limit := 5
				if len(parts) > 1 {
					if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
						limit = n
					}
				}
				fetchAndSendDiffs(bot, message.Chat.ID, config, limit, "")
			case "live":
				limit := 5
				if len(parts) > 1 {
					if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
						limit = n
					}
				}
				fetchAndSendDiffs(bot, message.Chat.ID, config, limit, "live")
			case "upcoming":
				limit := 5
				if len(parts) > 1 {
					if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
						limit = n
					}
				}
				fetchAndSendDiffs(bot, message.Chat.ID, config, limit, "upcoming")
			case "overlays":
				limit := 10
				if len(parts) > 1 {
					if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && n <= 50 {
						limit = n
					}
				}
				fetchAndSendLineMovements(bot, message.Chat.ID, config, limit)
			default:
				sendHelpMessage(bot, message.Chat.ID)
			}
		}
	}
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	helpText := `🤖 *Value Bet Calculator Bot*

*Available Commands:*

/start - Start/resume asynchronous diff processing

/stop - Остановить всё (и валуи, и прогрузы)

/stop\_values - Отключить только алерты по валуям (прогрузы продолжают приходить)

/stop\_overlays - Отключить только алерты по прогрузам (валуи продолжают приходить)

/top [limit] - Get top value bet differences
  Example: /top 10

/live [limit] - Get top differences for live matches
  Example: /live 5

/upcoming [limit] - Get top differences for upcoming matches
  Example: /upcoming 10

/overlays [limit] - Get top line movements (прогрузы)
  Example: /overlays 10

/help - Show this help message

*Usage:*
You can also send messages like:
• "top 10" - Get top 10 differences
• "live 5" - Get top 5 live matches
• "upcoming 3" - Get top 3 upcoming matches
• "overlays 10" - Get top 10 прогрузов

*Note:* Limit must be between 1 and 50. Default for /top, /live, /upcoming is 5; for /overlays is 10.`

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := bot.Send(msg); err != nil {
		slog.Error("Failed to send help message", "chat_id", chatID, "error", err)
	}
}

func fetchAndSendDiffs(bot *tgbotapi.BotAPI, chatID int64, config BotConfig, limit int, status string) {
	// Show "typing..." indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		slog.Debug("Failed to send typing indicator", "chat_id", chatID, "error", err)
	}

	// Build URL - use value-bets endpoint instead of diffs
	url := fmt.Sprintf("%s/value-bets/top?limit=%d", config.CalculatorURL, limit)
	if status != "" {
		url += "&status=" + status
	}

	// Fetch data from calculator
	slog.Debug("Fetching diffs", "url", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		slog.Error("Failed to fetch from calculator", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("Calculator returned non-OK status", "status", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		slog.Debug("Calculator error response body", "body", string(bodyBytes))
		var errorResp map[string]string
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		}
		return
	}

	// Read response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read calculator response body", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to read response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	previewLen := 200
	if len(bodyBytes) < previewLen {
		previewLen = len(bodyBytes)
	}
	slog.Debug("Calculator response", "length", len(bodyBytes), "preview", string(bodyBytes[:previewLen]))

	var valueBets []ValueBet
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&valueBets); err != nil {
		previewLen := 500
		if len(bodyBytes) < previewLen {
			previewLen = len(bodyBytes)
		}
		slog.Error("Failed to parse calculator response", "error", err, "body_preview", string(bodyBytes[:previewLen]))
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	slog.Info("Received value bets from calculator", "count", len(valueBets))

	// Debug: log first value bet structure if available
	if len(valueBets) > 0 {
		slog.Debug("First value bet", "match_name", valueBets[0].MatchName, "bookmaker", valueBets[0].Bookmaker, "odds", valueBets[0].AllBookmakerOdds)
	}

	if len(valueBets) == 0 {
		statusText := ""
		if status == "live" {
			statusText = " live"
		} else if status == "upcoming" {
			statusText = " upcoming"
		}
		msgText := fmt.Sprintf("📊 No%s value bets found.", statusText)
		slog.Debug("Sending empty result message", "chat_id", chatID, "message", msgText)
		msg := tgbotapi.NewMessage(chatID, msgText)
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send empty result message", "chat_id", chatID, "error", sendErr)
		} else {
			slog.Debug("Successfully sent empty result message", "chat_id", chatID)
		}
		return
	}

	// Format and send results
	// Telegram has a message length limit of 4096 characters
	// Split into multiple messages if needed
	var builder strings.Builder
	// Use limit instead of len(valueBets) for header, but show actual count
	actualCount := len(valueBets)
	if actualCount > limit {
		actualCount = limit
	}
	header := fmt.Sprintf("📊 *Top %d Value Bets", actualCount)
	if status == "live" {
		header += " (Live)"
	} else if status == "upcoming" {
		header += " (Upcoming)"
	}
	header += "*\n\n"

	builder.WriteString(header)

	for i, vb := range valueBets {
		if i >= limit {
			break
		}

		// Format event type and outcome
		eventStr := formatEventType(vb.EventType)
		outcomeStr := formatOutcomeType(vb.OutcomeType)
		betInfo := fmt.Sprintf("%s | %s", eventStr, outcomeStr)
		if vb.Parameter != "" {
			betInfo += fmt.Sprintf(" (%s)", vb.Parameter)
		}

		entry := fmt.Sprintf("*%d. %s*\n", i+1, escapeMarkdown(vb.MatchName))
		entry += fmt.Sprintf("⚽ %s\n", betInfo)
		entry += fmt.Sprintf("💰 Value: *%.2f%%*\n", vb.ValuePercent)
		entry += fmt.Sprintf("🎯 %s: *%.2f*\n", vb.Bookmaker, vb.BookmakerOdd)
		entry += fmt.Sprintf("📊 Fair odd: %.2f (prob: %.2f%%)\n", vb.FairOdd, vb.FairProbability*100)

		// Show all bookmaker odds
		if len(vb.AllBookmakerOdds) > 0 {
			entry += "📈 All odds: "
			var oddsParts []string
			for bk, odd := range vb.AllBookmakerOdds {
				oddsParts = append(oddsParts, fmt.Sprintf("%s: %.2f", bk, odd))
			}
			// Sort for consistent output
			sort.Strings(oddsParts)
			entry += strings.Join(oddsParts, " | ")
			entry += "\n"
		}

		entry += fmt.Sprintf("🕐 Start: %s\n", formatTime(vb.StartTime))
		entry += "\n"

		// Check if adding this entry would exceed message limit
		if builder.Len()+len(entry) > 4000 {
			// Send current message and start new one
			msg := tgbotapi.NewMessage(chatID, builder.String())
			msg.ParseMode = tgbotapi.ModeMarkdown
			if _, err := bot.Send(msg); err != nil {
				slog.Error("Failed to send message part", "chat_id", chatID, "error", err)
				return
			}
			builder.Reset()
			builder.WriteString(header)
		}

		builder.WriteString(entry)
	}

	// Send remaining message
	if builder.Len() > len(header) {
		msgText := builder.String()
		slog.Debug("Sending value bets message", "chat_id", chatID, "chars", len(msgText), "count", len(valueBets))
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := bot.Send(msg); err != nil {
			slog.Error("Failed to send final message", "chat_id", chatID, "error", err)
		} else {
			slog.Debug("Successfully sent value bets", "chat_id", chatID, "count", len(valueBets))
		}
	} else {
		slog.Debug("Message builder is empty or only contains header, not sending", "chat_id", chatID)
	}
}

func fetchAndSendLineMovements(bot *tgbotapi.BotAPI, chatID int64, config BotConfig, limit int) {
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		slog.Debug("Failed to send typing indicator", "chat_id", chatID, "error", err)
	}

	url := fmt.Sprintf("%s/line-movements/top?limit=%d", config.CalculatorURL, limit)
	slog.Debug("Fetching line movements", "url", url)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		slog.Error("Failed to fetch line movements from calculator", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("Calculator returned non-OK status for line movements", "status", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		var errorResp map[string]string
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		}
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read line movements response", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to read response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	var movements []LineMovement
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&movements); err != nil {
		slog.Error("Failed to parse line movements response", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	if len(movements) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📊 Нет актуальных прогрузов.")
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send empty result message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	var builder strings.Builder
	actualCount := len(movements)
	if actualCount > limit {
		actualCount = limit
	}
	header := fmt.Sprintf("📊 *Топ %d прогрузов*\n\n", actualCount)
	builder.WriteString(header)

	for i, lm := range movements {
		if i >= limit {
			break
		}
		eventStr := formatEventType(lm.EventType)
		outcomeStr := formatOutcomeType(lm.OutcomeType)
		betInfo := fmt.Sprintf("%s | %s", eventStr, outcomeStr)
		if lm.Parameter != "" {
			betInfo += fmt.Sprintf(" (%s)", lm.Parameter)
		}
		entry := fmt.Sprintf("*%d. %s*\n", i+1, escapeMarkdown(lm.MatchName))
		if lm.Tournament != "" || lm.Sport != "" {
			leagueLine := strings.TrimSpace(lm.Sport)
			if lm.Tournament != "" {
				if leagueLine != "" {
					leagueLine += " • "
				}
				leagueLine += strings.TrimSpace(lm.Tournament)
			}
			if leagueLine != "" {
				entry += fmt.Sprintf("🏆 %s\n", escapeMarkdown(leagueLine))
			}
		}
		entry += fmt.Sprintf("📌 %s\n", betInfo)
		entry += fmt.Sprintf("🏠 %s: *%.2f* → *%.2f* (%+.1f%%)\n", escapeMarkdown(lm.Bookmaker), lm.PreviousOdd, lm.CurrentOdd, lm.ChangePercent)
		entry += fmt.Sprintf("🕐 Start: %s\n\n", formatTime(lm.StartTime))

		if builder.Len()+len(entry) > 4000 {
			msg := tgbotapi.NewMessage(chatID, builder.String())
			msg.ParseMode = tgbotapi.ModeMarkdown
			if _, err := bot.Send(msg); err != nil {
				slog.Error("Failed to send line movements message part", "chat_id", chatID, "error", err)
				return
			}
			builder.Reset()
			builder.WriteString(header)
		}
		builder.WriteString(entry)
	}

	if builder.Len() > len(header) {
		msg := tgbotapi.NewMessage(chatID, builder.String())
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := bot.Send(msg); err != nil {
			slog.Error("Failed to send line movements message", "chat_id", chatID, "error", err)
		}
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04 UTC")
}

func formatEventType(eventType string) string {
	// Convert snake_case to Title Case
	parts := strings.Split(eventType, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

func formatOutcomeType(outcomeType string) string {
	// Convert snake_case to Title Case
	parts := strings.Split(outcomeType, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

func escapeMarkdown(text string) string {
	// Escape special Markdown characters
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func startAsyncProcessing(bot *tgbotapi.BotAPI, chatID int64, config BotConfig) {
	// Show "typing..." indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		slog.Debug("Failed to send typing indicator", "chat_id", chatID, "error", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/async/start", config.CalculatorURL)

	// Send POST request to start async processing
	slog.Debug("Starting async processing", "url", url)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		slog.Error("Failed to create request", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to create request: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to start async processing", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("Calculator returned non-OK status", "status", resp.StatusCode)
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		}
		return
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("Failed to parse calculator response", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	// Send success message
	statusMsg := "✅ " + result["message"]
	if result["status"] == "already_running" {
		statusMsg = "ℹ️ " + result["message"]
	}
	msg := tgbotapi.NewMessage(chatID, statusMsg)
	if _, err := bot.Send(msg); err != nil {
		slog.Error("Failed to send start confirmation", "chat_id", chatID, "error", err)
	} else {
		slog.Info("Successfully started async processing via bot")
	}
}

func stopAsyncProcessing(bot *tgbotapi.BotAPI, chatID int64, config BotConfig) {
	// Show "typing..." indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		slog.Debug("Failed to send typing indicator", "chat_id", chatID, "error", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/async/stop", config.CalculatorURL)

	// Send POST request to stop async processing
	slog.Debug("Stopping async processing", "url", url)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		slog.Error("Failed to create request", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to create request: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to stop async processing", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("Calculator returned non-OK status", "status", resp.StatusCode)
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
			}
		}
		return
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("Failed to parse calculator response", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			slog.Error("Failed to send error message", "chat_id", chatID, "error", sendErr)
		}
		return
	}

	// Send success message
	statusMsg := "✅ " + result["message"]
	if result["status"] == "already_stopped" {
		statusMsg = "ℹ️ " + result["message"]
	}
	msg := tgbotapi.NewMessage(chatID, statusMsg)
	if _, err := bot.Send(msg); err != nil {
		slog.Error("Failed to send stop confirmation", "chat_id", chatID, "error", err)
	} else {
		slog.Info("Successfully stopped async processing via bot")
	}
}

// stopAlertType disables only one type of alerts (values or overlays) via calculator API.
func stopAlertType(bot *tgbotapi.BotAPI, chatID int64, config BotConfig, alertType string, defaultMsg string) {
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		slog.Debug("Failed to send typing indicator", "chat_id", chatID, "error", err)
	}

	var path string
	switch alertType {
	case "values":
		path = "/async/stop_values"
	case "overlays":
		path = "/async/stop_overlays"
	default:
		msg := tgbotapi.NewMessage(chatID, "❌ Unknown alert type.")
		_, _ = bot.Send(msg)
		return
	}

	url := config.CalculatorURL + path
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		slog.Error("Failed to create request", "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %v", err))
		_, _ = bot.Send(msg)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to stop alert type", "type", alertType, "error", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось связаться с калькулятором: %v", err))
		_, _ = bot.Send(msg)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var errorResp map[string]string
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, "❌ "+errorResp["error"])
			_, _ = bot.Send(msg)
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Calculator вернул статус %d", resp.StatusCode))
			_, _ = bot.Send(msg)
		}
		return
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("Failed to parse response", "error", err)
		msg := tgbotapi.NewMessage(chatID, "✅ "+defaultMsg)
		_, _ = bot.Send(msg)
		return
	}

	statusMsg := "✅ " + result["message"]
	msg := tgbotapi.NewMessage(chatID, statusMsg)
	if _, err := bot.Send(msg); err != nil {
		slog.Error("Failed to send stop alert type confirmation", "chat_id", chatID, "error", err)
	} else {
		slog.Info("Stopped alert type via bot", "type", alertType)
	}
}

// LineMovement represents a line movement / прогруз (matches the calculator response)
type LineMovement struct {
	MatchGroupKey   string    `json:"match_group_key"`
	MatchName       string    `json:"match_name"`
	StartTime       time.Time `json:"start_time"`
	Sport           string    `json:"sport"`
	Tournament      string    `json:"tournament"` // league/championship for identification (e.g. when match is "Home vs Away")
	EventType       string    `json:"event_type"`
	OutcomeType     string    `json:"outcome_type"`
	Parameter       string    `json:"parameter"`
	BetKey          string    `json:"bet_key"`
	Bookmaker       string    `json:"bookmaker"`
	PreviousOdd     float64   `json:"previous_odd"`
	CurrentOdd      float64   `json:"current_odd"`
	ChangeAbs       float64   `json:"change_abs"`
	ChangePercent   float64   `json:"change_percent"`
	RecordedAt      time.Time `json:"recorded_at"`
}

// ValueBet represents a value bet (matches the calculator response)
type ValueBet struct {
	MatchGroupKey    string             `json:"match_group_key"`
	MatchName        string             `json:"match_name"`
	StartTime        time.Time          `json:"start_time"`
	Sport            string             `json:"sport"`
	EventType        string             `json:"event_type"`
	OutcomeType      string             `json:"outcome_type"`
	Parameter        string             `json:"parameter"`
	BetKey           string             `json:"bet_key"`
	AllBookmakerOdds map[string]float64 `json:"all_bookmaker_odds"`
	FairOdd          float64            `json:"fair_odd"`
	FairProbability  float64            `json:"fair_probability"`
	Bookmaker        string             `json:"bookmaker"`
	BookmakerOdd     float64            `json:"bookmaker_odd"`
	ValuePercent     float64            `json:"value_percent"`
	ExpectedValue    float64            `json:"expected_value"`
	CalculatedAt     time.Time          `json:"calculated_at"`
}
