package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
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
	
	// Test bot connection by getting bot info
	botInfo, err := bot.GetMe()
	if err != nil {
		log.Fatalf("Failed to get bot info (token might be invalid): %v", err)
	}
	
	log.Printf("Authorized on account %s (ID: %d)", botInfo.UserName, botInfo.ID)
	log.Printf("Bot is ready to receive messages")
	log.Printf("Bot token: %s...%s (first 10, last 4 chars)", config.Token[:10], config.Token[len(config.Token)-4:])

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

	// Start bot handler
	log.Println("Starting updates channel...")
	updates := bot.GetUpdatesChan(u)
	
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in bot handler: %v", r)
			}
		}()
		
		for {
			select {
			case <-ctx.Done():
				log.Println("Stopping bot updates...")
				bot.StopReceivingUpdates()
				return
			case update := <-updates:
				// Handle each update in a separate goroutine to prevent one error from blocking others
				go func(upd tgbotapi.Update) {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("PANIC handling message from user %d: %v", upd.Message.From.ID, r)
						}
					}()
					
					if upd.Message == nil {
						return
					}

					log.Printf("Received message from user %d: %s", upd.Message.From.ID, upd.Message.Text)

					// Check if user is allowed (if restrictions are set)
					if len(config.AllowedUserIDs) > 0 {
						allowed := false
						for _, id := range config.AllowedUserIDs {
							if upd.Message.From.ID == id {
								allowed = true
								break
							}
						}
						if !allowed {
							msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Access denied. You are not authorized to use this bot.")
							if _, err := bot.Send(msg); err != nil {
								log.Printf("Failed to send access denied message to user %d: %v", upd.Message.From.ID, err)
							}
							return
						}
					}

					handleMessage(bot, upd.Message, config)
				}(update)
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
		case "/stop":
			stopAsyncProcessing(bot, message.Chat.ID, config)
		default:
			msg := tgbotapi.NewMessage(message.Chat.ID, "Unknown command. Use /help to see available commands.")
			if _, err := bot.Send(msg); err != nil {
				log.Printf("Failed to send unknown command message to user %d: %v", message.From.ID, err)
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

/stop - Stop asynchronous diff processing

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
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Failed to send help message to chat %d: %v", chatID, err)
	}
}

func fetchAndSendDiffs(bot *tgbotapi.BotAPI, chatID int64, config BotConfig, limit int, status string) {
	// Show "typing..." indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		log.Printf("Failed to send typing indicator to chat %d: %v", chatID, err)
	}

	// Build URL - use value-bets endpoint instead of diffs
	url := fmt.Sprintf("%s/value-bets/top?limit=%d", config.CalculatorURL, limit)
	if status != "" {
		url += "&status=" + status
	}

	// Fetch data from calculator
	log.Printf("Fetching diffs from %s", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Failed to fetch from calculator: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Calculator returned status %d", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Calculator error response body: %s", string(bodyBytes))
		var errorResp map[string]string
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
			}
		}
		return
	}

	// Read response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read calculator response body: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to read response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}

	previewLen := 200
	if len(bodyBytes) < previewLen {
		previewLen = len(bodyBytes)
	}
	log.Printf("Calculator response body length: %d bytes, preview: %s", len(bodyBytes), string(bodyBytes[:previewLen]))

	var valueBets []ValueBet
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&valueBets); err != nil {
		previewLen := 500
		if len(bodyBytes) < previewLen {
			previewLen = len(bodyBytes)
		}
		log.Printf("Failed to parse calculator response: %v, body: %s", err, string(bodyBytes[:previewLen]))
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}

	log.Printf("Received %d value bets from calculator", len(valueBets))
	
	// Debug: log first value bet structure if available
	if len(valueBets) > 0 {
		log.Printf("First value bet: MatchName=%s, Bookmaker=%s, AllBookmakerOdds=%v", 
			valueBets[0].MatchName, valueBets[0].Bookmaker, valueBets[0].AllBookmakerOdds)
	}

	if len(valueBets) == 0 {
		statusText := ""
		if status == "live" {
			statusText = " live"
		} else if status == "upcoming" {
			statusText = " upcoming"
		}
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("📊 No%s value bets found.", statusText))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send empty result message to chat %d: %v", chatID, sendErr)
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
		entry += fmt.Sprintf("💰 Value: *%.2f%%* | Expected: %.4f\n", vb.ValuePercent, vb.ExpectedValue)
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
				log.Printf("Failed to send message part to chat %d: %v", chatID, err)
				return
			}
			builder.Reset()
			builder.WriteString(header)
		}

		builder.WriteString(entry)
	}

	// Send remaining message
	if builder.Len() > len(header) {
		msg := tgbotapi.NewMessage(chatID, builder.String())
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := bot.Send(msg); err != nil {
			log.Printf("Failed to send final message to chat %d: %v", chatID, err)
		} else {
			log.Printf("Successfully sent value bets to chat %d", chatID)
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
		log.Printf("Failed to send typing indicator to chat %d: %v", chatID, err)
	}

	// Build URL
	url := fmt.Sprintf("%s/async/start", config.CalculatorURL)

	// Send POST request to start async processing
	log.Printf("Starting async processing via %s", url)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to create request: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to start async processing: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Calculator returned status %d", resp.StatusCode)
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
			}
		}
		return
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Failed to parse calculator response: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
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
		log.Printf("Failed to send start confirmation to chat %d: %v", chatID, err)
	} else {
		log.Printf("Successfully started async processing via bot")
	}
}

func stopAsyncProcessing(bot *tgbotapi.BotAPI, chatID int64, config BotConfig) {
	// Show "typing..." indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := bot.Request(typing); err != nil {
		log.Printf("Failed to send typing indicator to chat %d: %v", chatID, err)
	}

	// Build URL
	url := fmt.Sprintf("%s/async/stop", config.CalculatorURL)

	// Send POST request to stop async processing
	log.Printf("Stopping async processing via %s", url)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to create request: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to stop async processing: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to connect to calculator service: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Calculator returned status %d", resp.StatusCode)
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: %s", errorResp["error"]))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Calculator service returned status %d", resp.StatusCode))
			if _, sendErr := bot.Send(msg); sendErr != nil {
				log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
			}
		}
		return
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Failed to parse calculator response: %v", err)
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Error: Failed to parse response: %v", err))
		if _, sendErr := bot.Send(msg); sendErr != nil {
			log.Printf("Failed to send error message to chat %d: %v", chatID, sendErr)
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
		log.Printf("Failed to send stop confirmation to chat %d: %v", chatID, err)
	} else {
		log.Printf("Successfully stopped async processing via bot")
	}
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
