package calculator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// Min interval between any two Telegram messages to the same chat to avoid 429 Too Many Requests (~30/min limit).
const telegramSendInterval = 2 * time.Second

// TelegramNotifier sends Telegram notifications for high-value diffs
type TelegramNotifier struct {
	bot      *tgbotapi.BotAPI
	chatID   int64
	mu       sync.Mutex
	lastSend time.Time
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

// waitSendInterval waits until at least telegramSendInterval has passed since lastSend. Holds n.mu for the whole wait so sends are serialized. Call with n.mu held.
func (n *TelegramNotifier) waitSendInterval(ctx context.Context) error {
	for {
		elapsed := time.Since(n.lastSend)
		if elapsed >= telegramSendInterval {
			return nil
		}
		wait := telegramSendInterval - elapsed
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		n.mu.Unlock()
		select {
		case <-ctx.Done():
			n.mu.Lock()
			return ctx.Err()
		case <-time.After(wait):
			n.mu.Lock()
		}
	}
}

// SendDiffAlert sends an alert for a high-value diff
func (n *TelegramNotifier) SendDiffAlert(ctx context.Context, diff *DiffBet, threshold int) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	message := n.formatDiffAlert(diff, threshold)
	msg := tgbotapi.NewMessage(n.chatID, message)
	msg.ParseMode = tgbotapi.ModeMarkdown

	n.mu.Lock()
	if err := n.waitSendInterval(ctx); err != nil {
		n.mu.Unlock()
		return err
	}
	n.lastSend = time.Now()
	_, err := n.bot.Send(msg)
	n.mu.Unlock()
	return err
}

// SendLineMovementAlert sends an alert for a significant odds change in the same bookmaker.
// history is used to show timeline (e.g. "6.70 (12 min ago) → 7.10 (now)").
// thresholdPercent is the min change in % that triggered the alert (e.g. 5.0 for 5%).
func (n *TelegramNotifier) SendLineMovementAlert(ctx context.Context, lm *LineMovement, thresholdPercent float64, now time.Time, history []storage.OddsHistoryPoint) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	message := n.formatLineMovementAlert(lm, thresholdPercent, now, history)
	msg := tgbotapi.NewMessage(n.chatID, message)
	msg.ParseMode = tgbotapi.ModeMarkdown

	n.mu.Lock()
	if err := n.waitSendInterval(ctx); err != nil {
		n.mu.Unlock()
		return err
	}
	n.lastSend = time.Now()
	_, err := n.bot.Send(msg)
	n.mu.Unlock()
	return err
}

func (n *TelegramNotifier) formatLineMovementAlert(lm *LineMovement, thresholdPercent float64, now time.Time, history []storage.OddsHistoryPoint) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("📊 *Line movement (≥%.1f%%)*\n\n", thresholdPercent))
	builder.WriteString(fmt.Sprintf("*%s*\n", escapeMarkdown(lm.MatchName)))
	builder.WriteString(fmt.Sprintf("📌 %s | %s", formatEventType(lm.EventType), formatOutcomeType(lm.OutcomeType)))
	if lm.Parameter != "" {
		builder.WriteString(fmt.Sprintf(" (%s)", lm.Parameter))
	}
	builder.WriteString("\n\n")
	bookmakerLabel := strings.TrimSpace(lm.Bookmaker)
	if bookmakerLabel == "" {
		bookmakerLabel = "—"
	}
	builder.WriteString(fmt.Sprintf("🏠 *%s*\n", escapeMarkdown(bookmakerLabel)))
	changeStr := fmt.Sprintf("%+.1f%%", lm.ChangePercent)
	builder.WriteString(fmt.Sprintf("Was: *%.2f* → now: *%.2f* (%s)\n", lm.PreviousOdd, lm.CurrentOdd, changeStr))
	// Timeline: collapse consecutive same odds, e.g. "6.70 (12 min ago) → 6.85 (5 min ago) → 7.10 (now)"
	if len(history) > 0 {
		timeline := collapseConsecutiveOdds(history)
		builder.WriteString("Timeline: ")
		for i, p := range timeline {
			if i > 0 {
				builder.WriteString(" → ")
			}
			mins := int(now.Sub(p.RecordedAt).Minutes())
			if mins <= 0 {
				builder.WriteString(fmt.Sprintf("*%.2f* (now)", p.Odd))
			} else {
				builder.WriteString(fmt.Sprintf("*%.2f* (%d min ago)", p.Odd, mins))
			}
		}
		builder.WriteString("\n")
	}
	if !lm.StartTime.IsZero() {
		builder.WriteString(fmt.Sprintf("🕐 Kick-off: %s\n", formatTime(lm.StartTime)))
	}
	if lm.Sport != "" {
		builder.WriteString(fmt.Sprintf("🏆 %s\n", lm.Sport))
	}
	return builder.String()
}

// formatDiffAlert formats a diff bet as a Telegram message (English).
func (n *TelegramNotifier) formatDiffAlert(diff *DiffBet, threshold int) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("🚨 *Value Bet Alert (%d%%+)*\n\n", threshold))
	builder.WriteString(fmt.Sprintf("*%s*\n", escapeMarkdown(diff.MatchName)))
	builder.WriteString(fmt.Sprintf("⚽ %s | %s", formatEventType(diff.EventType), formatOutcomeType(diff.OutcomeType)))
	if diff.Parameter != "" {
		builder.WriteString(fmt.Sprintf(" (%s)", diff.Parameter))
	}
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("📈 *Difference: %.2f%%*\n", diff.DiffPercent))
	builder.WriteString(fmt.Sprintf("💰 %s: %.2f | %s: %.2f\n", diff.MinBookmaker, diff.MinOdd, diff.MaxBookmaker, diff.MaxOdd))
	if !diff.StartTime.IsZero() {
		builder.WriteString(fmt.Sprintf("🕐 Kick-off: %s\n", formatTime(diff.StartTime)))
	}
	if diff.Sport != "" {
		builder.WriteString(fmt.Sprintf("🏆 %s\n", diff.Sport))
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

// collapseConsecutiveOdds keeps first, last, and points where odd changed (shorter timeline).
func collapseConsecutiveOdds(history []storage.OddsHistoryPoint) []storage.OddsHistoryPoint {
	if len(history) <= 2 {
		return history
	}
	var out []storage.OddsHistoryPoint
	out = append(out, history[0])
	for i := 1; i < len(history)-1; i++ {
		if history[i].Odd != history[i-1].Odd || history[i].Odd != history[i+1].Odd {
			out = append(out, history[i])
		}
	}
	out = append(out, history[len(history)-1])
	return out
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
