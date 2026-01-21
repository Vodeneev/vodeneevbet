package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

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

	flag.StringVar(&token, "token", "", "Telegram bot token (required, or set TELEGRAM_BOT_TOKEN env var)")
	flag.StringVar(&calculatorURL, "calculator-url", defaultCalculatorURL, "Calculator service URL")
	flag.StringVar(&allowedUsers, "allowed-users", "", "Comma-separated list of allowed user IDs (optional)")
	flag.Parse()

	// Get token from environment if not provided via flag
	if token == "" {
		token = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if token == "" {
		log.Fatal("Telegram bot token is required. Set -token flag or TELEGRAM_BOT_TOKEN env var")
	}

	// Get calculator URL from environment if not provided
	if calculatorURL == defaultCalculatorURL {
		if envURL := os.Getenv("CALCULATOR_URL"); envURL != "" {
			calculatorURL = envURL
		}
	}

	config := BotConfig{
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
				config.AllowedUserIDs = append(config.AllowedUserIDs, id)
			}
		}
	}

	log.Printf("Starting Telegram bot...")
	log.Printf("Calculator URL: %s", config.CalculatorURL)

	bot, err := tgbotapi.NewBotAPI(config.Token)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	bot.Debug = false
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = config.UpdateTimeout

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping bot...")
		cancel()
	}()

	updates := bot.GetUpdatesChan(u)

	// Start bot handler
	go func() {
		for {
			select {
			case <-ctx.Done():
				bot.StopReceivingUpdates()
				return
			case update := <-updates:
				if update.Message == nil {
					continue
				}

				// Check if user is allowed (if restrictions are set)
				if len(config.AllowedUserIDs) > 0 {
					allowed := false
					for _, id := range config.AllowedUserIDs {
						if update.Message.From.ID == id {
							allowed = true
							break
						}
					}
					if !allowed {
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Access denied. You are not authorized to use this bot.")
						bot.Send(msg)
						continue
					}
				}

				handleMessage(bot, update.Message, config)
			}
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("Telegram bot stopped")
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
		case "/start", "/help":
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
		default:
			msg := tgbotapi.NewMessage(message.Chat.ID, "Unknown command. Use /help to see available commands.")
			bot.Send(msg)
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
			default:
				sendHelpMessage(bot, message.Chat.ID)
			}
		}
	}
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	helpText := `🤖 *Value Bet Calculator Bot*

*Available Commands:*

/top [limit] - Get top value bet differences
  Example: /top 10

/live [limit] - Get top differences for live matches
  Example: /live 5

/upcoming [limit] - Get top differences for upcoming matches
  Example: /upcoming 10

/help - Show this help message

*Usage:*
You can also send messages like:
• "top 10" - Get top 10 differences
• "live 5" - Get top 5 live matches
• "upcoming 3" - Get top 3 upcoming matches

*Note:* Limit must be between 1 and 50. Default is 5.`

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ParseMode = tgbotapi.ModeMarkdown
	bot.Send(msg)
}

func fetchAndSendDiffs(bot *tgbotapi.BotAPI, chatID int64, config BotConfig, limit int, status string) {
	// Show "typing..." indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	bot.Send(typing)

	// Build URL
	url := fmt.Sprintf("%s/diffs/top?limit=%d", config.CalculatorURL, limit)
	if status != "" {
		url += "&status=" + status
	}

	// Fetch data from calculator
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		bot.Send(msg)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			bot.Send(msg)
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			bot.Send(msg)
		}
		return
	}

	var diffs []DiffBet
	if err := json.NewDecoder(resp.Body).Decode(&diffs); err != nil {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		bot.Send(msg)
		return
	}

	if len(diffs) == 0 {
		statusText := ""
		if status == "live" {
			statusText = " live"
		} else if status == "upcoming" {
			statusText = " upcoming"
		}
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("📊 No%s value bet differences found.", statusText))
		bot.Send(msg)
		return
	}

	// Format and send results
	// Telegram has a message length limit of 4096 characters
	// Split into multiple messages if needed
	var builder strings.Builder
	header := fmt.Sprintf("📊 *Top %d Value Bet Differences", len(diffs))
	if status == "live" {
		header += " (Live)"
	} else if status == "upcoming" {
		header += " (Upcoming)"
	}
	header += "*\n\n"

	builder.WriteString(header)

	for i, diff := range diffs {
		if i >= limit {
			break
		}

		// Format event type and outcome
		eventStr := formatEventType(diff.EventType)
		outcomeStr := formatOutcomeType(diff.OutcomeType)
		betInfo := fmt.Sprintf("%s | %s", eventStr, outcomeStr)
		if diff.Parameter != "" {
			betInfo += fmt.Sprintf(" (%s)", diff.Parameter)
		}

		entry := fmt.Sprintf("*%d. %s*\n", i+1, escapeMarkdown(diff.MatchName))
		entry += fmt.Sprintf("⚽ %s\n", betInfo)
		entry += fmt.Sprintf("📈 Difference: *%.2f%%* (%.2f)\n", diff.DiffPercent, diff.DiffAbs)
		entry += fmt.Sprintf("💰 %s: %.2f | %s: %.2f\n", diff.MinBookmaker, diff.MinOdd, diff.MaxBookmaker, diff.MaxOdd)
		entry += fmt.Sprintf("🕐 Start: %s\n", formatTime(diff.StartTime))
		entry += "\n"

		// Check if adding this entry would exceed message limit
		if builder.Len()+len(entry) > 4000 {
			// Send current message and start new one
			msg := tgbotapi.NewMessage(chatID, builder.String())
			msg.ParseMode = tgbotapi.ModeMarkdown
			bot.Send(msg)
			builder.Reset()
			builder.WriteString(header)
		}

		builder.WriteString(entry)
	}

	// Send remaining message
	if builder.Len() > len(header) {
		msg := tgbotapi.NewMessage(chatID, builder.String())
		msg.ParseMode = tgbotapi.ModeMarkdown
		bot.Send(msg)
	}

	msg := tgbotapi.NewMessage(chatID, builder.String())
	msg.ParseMode = tgbotapi.ModeMarkdown
	bot.Send(msg)
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

// DiffBet represents a value bet difference (matches the calculator response)
type DiffBet struct {
	MatchGroupKey string    `json:"match_group_key"`
	MatchName     string    `json:"match_name"`
	StartTime     time.Time `json:"start_time"`
	Sport         string    `json:"sport"`
	EventType     string    `json:"event_type"`
	OutcomeType   string    `json:"outcome_type"`
	Parameter     string    `json:"parameter"`
	BetKey        string    `json:"bet_key"`
	Bookmakers    int       `json:"bookmakers"`
	MinBookmaker  string    `json:"min_bookmaker"`
	MinOdd        float64   `json:"min_odd"`
	MaxBookmaker  string    `json:"max_bookmaker"`
	MaxOdd        float64   `json:"max_odd"`
	DiffAbs       float64   `json:"diff_abs"`
	DiffPercent   float64   `json:"diff_percent"`
	CalculatedAt  time.Time `json:"calculated_at"`
}
