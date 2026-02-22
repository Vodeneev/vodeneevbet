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

// messageType represents the type of message to send
type messageType int

const (
	messageTypeDiff messageType = iota
	messageTypeLineMovement
	messageTypeTest
)

// queuedMessage represents a message queued for sending
type queuedMessage struct {
	msgType         messageType
	text            string
	diff            *DiffBet
	threshold       int
	lineMovement    *LineMovement
	thresholdPercent float64
	now             time.Time
	history         []storage.OddsHistoryPoint
	testMessage     string // For test alerts
}

// TelegramNotifier sends Telegram notifications for high-value diffs
type TelegramNotifier struct {
	bot      *tgbotapi.BotAPI
	chatID   int64
	mu       sync.Mutex
	lastSend time.Time

	// Async queue for sending messages
	queue     chan queuedMessage
	queueDone chan struct{}
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc

	// clearCh: send a channel here; messageSender drains queue then sends dropped count and closes
	clearCh chan chan int
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

	ctx, cancel := context.WithCancel(context.Background())
	
	notifier := &TelegramNotifier{
		bot:       bot,
		chatID:    chatID,
		queue:     make(chan queuedMessage, 100), // Buffer up to 100 messages
		queueDone: make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
		clearCh:   make(chan chan int),
	}

	// Start background worker for sending messages
	notifier.wg.Add(1)
	go notifier.messageSender()

	slog.Info("Telegram notifier initialized", "chat_id", chatID)

	return notifier
}

// QueueLen returns current number of messages in the send queue (for logging).
func (n *TelegramNotifier) QueueLen() int {
	if n == nil || n.queue == nil {
		return 0
	}
	return len(n.queue)
}

// ClearQueue drains the notification queue without sending. Pending alerts are dropped.
// Returns the number of messages that were dropped. Safe to call if notifier is nil.
func (n *TelegramNotifier) ClearQueue() int {
	if n == nil || n.clearCh == nil {
		return 0
	}
	select {
	case <-n.ctx.Done():
		return 0
	default:
	}
	respCh := make(chan int)
	select {
	case n.clearCh <- respCh:
		return <-respCh
	default:
		return 0
	}
}

// messageSender runs in background and sends queued messages with proper intervals
func (n *TelegramNotifier) messageSender() {
	defer n.wg.Done()

outer:
	for {
		select {
		case <-n.ctx.Done():
			// Drain remaining messages before exit
			for {
				select {
				case msg := <-n.queue:
					n.sendQueuedMessage(msg)
				default:
					close(n.queueDone)
					return
				}
			}
		case respCh := <-n.clearCh:
			// Clear queue: drain without sending
			drained := 0
			for {
				select {
				case <-n.queue:
					drained++
				default:
					if drained > 0 {
						slog.Info("Telegram notifier: queue cleared", "dropped_messages", drained)
					}
					respCh <- drained
					close(respCh)
					continue outer
				}
			}
		case msg := <-n.queue:
			n.sendQueuedMessage(msg)
		}
	}
}

// sendQueuedMessage sends a queued message with proper rate limiting
func (n *TelegramNotifier) sendQueuedMessage(msg queuedMessage) {
	var messageText string
	
	switch msg.msgType {
	case messageTypeDiff:
		messageText = n.formatDiffAlert(msg.diff, msg.threshold)
	case messageTypeLineMovement:
		messageText = n.formatLineMovementAlert(msg.lineMovement, msg.thresholdPercent, msg.now, msg.history)
	case messageTypeTest:
		messageText = msg.testMessage
	default:
		slog.Error("Unknown message type", "type", msg.msgType)
		return
	}
	
	tgMsg := tgbotapi.NewMessage(n.chatID, messageText)
	tgMsg.ParseMode = tgbotapi.ModeMarkdown
	
	// Log before waiting for interval
	queueTime := time.Now()
	prepLogArgs := []interface{}{"type", msg.msgType, "queue_time", queueTime.UTC().Format(time.RFC3339), "message_preview", truncateString(messageText, 50)}
	switch msg.msgType {
	case messageTypeDiff:
		if msg.diff != nil {
			prepLogArgs = append(prepLogArgs, "match", msg.diff.MatchName, "calculated_at", msg.diff.CalculatedAt.UTC().Format(time.RFC3339), "diff_percent", msg.diff.DiffPercent)
		}
	case messageTypeLineMovement:
		if msg.lineMovement != nil {
			prepLogArgs = append(prepLogArgs, "match", msg.lineMovement.MatchName, "detected_at", msg.now.UTC().Format(time.RFC3339), "change_percent", msg.lineMovement.ChangePercent)
		}
	}
	slog.Info("Telegram send: preparing to send message", prepLogArgs...)
	
	// Wait for proper interval
	n.mu.Lock()
	elapsed := time.Since(n.lastSend)
	waitStart := time.Now()
	if elapsed < telegramSendInterval {
		waitTime := telegramSendInterval - elapsed
		slog.Info("Telegram send: waiting for rate limit", 
			"elapsed_since_last", elapsed,
			"wait_time", waitTime,
			"type", msg.msgType)
		n.mu.Unlock()
		select {
		case <-n.ctx.Done():
			slog.Warn("Telegram send: cancelled during wait", "type", msg.msgType)
			return
		case <-time.After(waitTime):
		}
		n.mu.Lock()
	}
	actualWait := time.Since(waitStart)
	
	sendStart := time.Now()
	timeBeforeSend := n.lastSend
	n.lastSend = time.Now()
	_, err := n.bot.Send(tgMsg)
	sendDuration := time.Since(sendStart)
	totalDuration := time.Since(queueTime)
	timeSinceLast := time.Since(timeBeforeSend)
	n.mu.Unlock()
	
	sentAt := time.Now()
	extra := n.logSentExtraFields(msg, sentAt)
	if err != nil {
		args := append([]interface{}{
			"error", err,
			"type", msg.msgType,
			"sent_at", sentAt.UTC().Format(time.RFC3339),
			"total_duration", totalDuration,
			"wait_duration", actualWait,
			"send_duration", sendDuration,
			"time_since_last_send", timeSinceLast,
		}, extra...)
		slog.Error("Telegram send: failed", args...)
	} else {
		args := append([]interface{}{
			"type", msg.msgType,
			"sent_at", sentAt.UTC().Format(time.RFC3339),
			"total_duration", totalDuration,
			"wait_duration", actualWait,
			"send_duration", sendDuration,
			"time_since_last_send", timeSinceLast,
			"queue_length", len(n.queue),
		}, extra...)
		slog.Info("Telegram send: success", args...)
	}
}

// logSentExtraFields returns extra log fields for value/line-movement alerts (when the message was calculated/detected vs sent).
func (n *TelegramNotifier) logSentExtraFields(msg queuedMessage, sentAt time.Time) []interface{} {
	switch msg.msgType {
	case messageTypeDiff:
		if msg.diff != nil {
			delay := sentAt.Sub(msg.diff.CalculatedAt)
			return []interface{}{
				"match", msg.diff.MatchName,
				"calculated_at", msg.diff.CalculatedAt.UTC().Format(time.RFC3339),
				"delay_since_calculation_sec", delay.Seconds(),
			}
		}
	case messageTypeLineMovement:
		if msg.lineMovement != nil {
			delay := sentAt.Sub(msg.now)
			return []interface{}{
				"match", msg.lineMovement.MatchName,
				"detected_at", msg.now.UTC().Format(time.RFC3339),
				"delay_since_detection_sec", delay.Seconds(),
			}
		}
	}
	return nil
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// SendTestAlert sends a test alert message (non-blocking)
func (n *TelegramNotifier) SendTestAlert(ctx context.Context, message string) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	testMsg := fmt.Sprintf("🧪 *Test Alert*\n\n%s\n\n_Time: %s_", message, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	select {
	case <-n.ctx.Done():
		return fmt.Errorf("notifier stopped")
	case <-ctx.Done():
		return ctx.Err()
	case n.queue <- queuedMessage{
		msgType:     messageTypeTest,
		testMessage: testMsg,
	}:
		slog.Info("Telegram test alert: queued", "message", message, "queue_len", len(n.queue))
		return nil
	default:
		slog.Warn("Telegram test alert: queue full, dropping", "message", message)
		return fmt.Errorf("message queue is full")
	}
}

// Stop stops the notifier and waits for all queued messages to be sent
func (n *TelegramNotifier) Stop() {
	if n == nil {
		return
	}
	n.cancel()
	<-n.queueDone
	n.wg.Wait()
}

// SendDiffAlert queues an alert for a high-value diff (non-blocking)
func (n *TelegramNotifier) SendDiffAlert(ctx context.Context, diff *DiffBet, threshold int) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	select {
	case <-n.ctx.Done():
		return fmt.Errorf("notifier stopped")
	case <-ctx.Done():
		return ctx.Err()
	case n.queue <- queuedMessage{
		msgType:   messageTypeDiff,
		diff:      diff,
		threshold: threshold,
	}:
		return nil
	default:
		// Queue is full, log warning but don't block
		slog.Warn("Telegram message queue is full, dropping message", "match", diff.MatchName)
		return fmt.Errorf("message queue is full")
	}
}

// SendLineMovementAlert queues an alert for a significant odds change in the same bookmaker (non-blocking).
// history is used to show timeline (e.g. "6.70 (12 min ago) → 7.10 (now)").
// thresholdPercent is the min change in % that triggered the alert (e.g. 5.0 for 5%).
func (n *TelegramNotifier) SendLineMovementAlert(ctx context.Context, lm *LineMovement, thresholdPercent float64, now time.Time, history []storage.OddsHistoryPoint) error {
	if n == nil || n.bot == nil {
		return fmt.Errorf("telegram notifier not initialized")
	}

	// Copy history slice to avoid race conditions
	historyCopy := make([]storage.OddsHistoryPoint, len(history))
	copy(historyCopy, history)

	select {
	case <-n.ctx.Done():
		return fmt.Errorf("notifier stopped")
	case <-ctx.Done():
		return ctx.Err()
	case n.queue <- queuedMessage{
		msgType:         messageTypeLineMovement,
		lineMovement:    lm,
		thresholdPercent: thresholdPercent,
		now:             now,
		history:         historyCopy,
	}:
		return nil
	default:
		// Queue is full, log warning but don't block
		slog.Warn("Telegram message queue is full, dropping line movement message", "match", lm.MatchName)
		return fmt.Errorf("message queue is full")
	}
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
		// Show when this movement was first recorded (last history point) so user sees data age
		lastRecorded := history[len(history)-1].RecordedAt
		dataMins := int(now.Sub(lastRecorded).Minutes())
		if dataMins > 0 {
			builder.WriteString(fmt.Sprintf("📅 _Movement first seen %d min ago_\n", dataMins))
		}
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
