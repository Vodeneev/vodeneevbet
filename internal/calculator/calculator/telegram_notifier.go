package calculator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramNotifier sends Telegram notifications for high-value diffs
type TelegramNotifier struct {
	bot    *tgbotapi.BotAPI
	chatID int64
}

// NewTelegramNotifier creates a new Telegram notifier
func NewTelegramNotifier(token string, chatID int64) *TelegramNotifier {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		slog.Error("Failed to create telegram bot", "error", err)
		return nil
	}

	bot.Debug = false

	// Test bot connection
	_, err = bot.GetMe()
	if err != nil {
		slog.Error("Failed to get bot info", "error", err)
		return nil
	}

	slog.Info("Telegram notifier initialized", "chat_id", chatID)

	return &TelegramNotifier{
		bot:    bot,
		chatID: chatID,
	}
}

// SendDiffAlert sends an alert for a high-value diff
func (n *TelegramNotifier) SendDiffAlert(ctx context.Context, diff *DiffBet, threshold int) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	// Format the alert message
	message := n.formatDiffAlert(diff, threshold)

	msg := tgbotapi.NewMessage(n.chatID, message)
	msg.ParseMode = tgbotapi.ModeMarkdown

	// Send with context timeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		_, err := n.bot.Send(msg)
		return err
	}
}

// SendLineMovementAlert sends an alert for a significant odds change in the same bookmaker
func (n *TelegramNotifier) SendLineMovementAlert(ctx context.Context, lm *LineMovement, thresholdAbs float64) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	message := n.formatLineMovementAlert(lm, thresholdAbs)
	msg := tgbotapi.NewMessage(n.chatID, message)
	msg.ParseMode = tgbotapi.ModeMarkdown

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		_, err := n.bot.Send(msg)
		return err
	}
}

func (n *TelegramNotifier) formatLineMovementAlert(lm *LineMovement, thresholdAbs float64) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("📊 *Изменение линии (≥%.2f)*\n\n", thresholdAbs))
	builder.WriteString(fmt.Sprintf("*%s*\n", escapeMarkdown(lm.MatchName)))
	builder.WriteString(fmt.Sprintf("📌 %s | %s", formatEventType(lm.EventType), formatOutcomeType(lm.OutcomeType)))
	if lm.Parameter != "" {
		builder.WriteString(fmt.Sprintf(" (%s)", lm.Parameter))
	}
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("🏠 *%s*\n", escapeMarkdown(lm.Bookmaker)))
	changeStr := fmt.Sprintf("%+.2f", lm.ChangeAbs)
	builder.WriteString(fmt.Sprintf("Было: *%.2f* → стало: *%.2f* (%s)\n", lm.PreviousOdd, lm.CurrentOdd, changeStr))
	if !lm.StartTime.IsZero() {
		builder.WriteString(fmt.Sprintf("🕐 Начало: %s\n", formatTime(lm.StartTime)))
	}
	if lm.Sport != "" {
		builder.WriteString(fmt.Sprintf("🏆 %s\n", lm.Sport))
	}
	return builder.String()
}

// formatDiffAlert formats a diff bet as a Telegram message
func (n *TelegramNotifier) formatDiffAlert(diff *DiffBet, threshold int) string {
	var builder strings.Builder

	// Header with threshold
	builder.WriteString(fmt.Sprintf("🚨 *Value Bet Alert (%d%%+)*\n\n", threshold))

	// Match info
	builder.WriteString(fmt.Sprintf("*%s*\n", escapeMarkdown(diff.MatchName)))
	builder.WriteString(fmt.Sprintf("⚽ %s | %s", formatEventType(diff.EventType), formatOutcomeType(diff.OutcomeType)))
	if diff.Parameter != "" {
		builder.WriteString(fmt.Sprintf(" (%s)", diff.Parameter))
	}
	builder.WriteString("\n\n")

	// Difference info
	builder.WriteString(fmt.Sprintf("📈 *Difference: %.2f%%*\n", diff.DiffPercent))
	builder.WriteString(fmt.Sprintf("💰 %s: %.2f | %s: %.2f\n", diff.MinBookmaker, diff.MinOdd, diff.MaxBookmaker, diff.MaxOdd))

	// Time info
	if !diff.StartTime.IsZero() {
		builder.WriteString(fmt.Sprintf("🕐 Start: %s\n", formatTime(diff.StartTime)))
	}

	// Sport
	if diff.Sport != "" {
		builder.WriteString(fmt.Sprintf("🏆 Sport: %s\n", diff.Sport))
	}

	return builder.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04 UTC")
}

func formatEventType(eventType string) string {
	parts := strings.Split(eventType, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

func formatOutcomeType(outcomeType string) string {
	parts := strings.Split(outcomeType, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

func escapeMarkdown(text string) string {
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
