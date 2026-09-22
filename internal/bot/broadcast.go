/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"ashokshau/tgmusic/internal/broadcast"
	"ashokshau/tgmusic/internal/db"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	td "github.com/AshokShau/gotdbot"
)

const (
	// defaultBroadcastRate is deliberately far below Telegram's ~30 msg/s
	// per-bot limit: it keeps most of that budget free for the music bot's own
	// traffic (now-playing messages, button callbacks, queue edits) while a
	// broadcast is running. 5.6/s finishes ~81k targets in about 4 hours.
	defaultBroadcastRate = 5.6

	// bcCheckpointEvery is how often progress is saved to the database (so a
	// restart can resume) and known-dead targets are flushed in bulk.
	bcCheckpointEvery = 15 * time.Second
	// bcProgressEvery is how often the status message is edited. Kept
	// infrequent so the edits don't eat into the flood budget themselves.
	bcProgressEvery = 30 * time.Second
	// bcMaxErrorLines caps the error file so a mass failure can't build a
	// giant in-memory string.
	bcMaxErrorLines = 5000
)

var (
	bcMu sync.Mutex
	// bcCancel is non-nil exactly while a broadcast is running.
	bcCancel context.CancelFunc
	bcEngine *broadcast.Engine
)

func bcBegin() (context.Context, bool) {
	bcMu.Lock()
	defer bcMu.Unlock()
	if bcCancel != nil {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	bcCancel = cancel
	return ctx, true
}

func bcEnd() {
	bcMu.Lock()
	defer bcMu.Unlock()
	if bcCancel != nil {
		bcCancel()
	}
	bcCancel = nil
	bcEngine = nil
}

func bcRunning() bool {
	bcMu.Lock()
	defer bcMu.Unlock()
	return bcCancel != nil
}

// isUserGoneError reports whether err indicates the target user has blocked
// the bot (isBlocked=true) or their account no longer exists (isBlocked=false,
// meaning deleted). Matching is done on the error text (case-insensitively,
// since TDLib's own human-readable descriptions don't have consistent
// casing), same convention used elsewhere in this codebase (see
// vc/userbot.go, vc/leave_all.go) since the underlying td/MTProto error type
// doesn't expose a stable error code here.
func isUserGoneError(err error) (isBlocked, isDeleted bool) {
	if err == nil {
		return false, false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "user_is_blocked"), strings.Contains(msg, "bot was blocked by the user"):
		return true, false
	case strings.Contains(msg, "user_is_deleted"), strings.Contains(msg, "user_deactivated"), strings.Contains(msg, "user is deactivated"):
		return false, true
	// These three are TDLib's own descriptions (not MTProto error codes)
	// seen specifically on user (DM) broadcast targets: the bot has no
	// existing chat with the user (never started it / it was cleared),
	// so there's no way to deliver a message. Treated as "blocked" for
	// broadcast-skip purposes even though the account itself may be fine -
	// what matters is that resending to this ID will keep failing.
	case strings.Contains(msg, "chat not found"),
		strings.Contains(msg, "have no write access to the chat"),
		strings.Contains(msg, "can't initiate conversation with a user"):
		return true, false
	default:
		return false, false
	}
}

// isChatGoneError reports whether err indicates the target chat is no longer
// reachable (bot kicked/left, chat deleted, or otherwise inaccessible).
// Matching is case-insensitive for the same reason as isUserGoneError above.
func isChatGoneError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "chat_write_forbidden"),
		strings.Contains(msg, "channel_private"),
		strings.Contains(msg, "user_not_participant"),
		strings.Contains(msg, "peer_id_invalid"),
		strings.Contains(msg, "chat_id_invalid"),
		strings.Contains(msg, "bot was kicked"),
		strings.Contains(msg, "chat not found"),
		strings.Contains(msg, "have no write access to the chat"),
		strings.Contains(msg, "group chat was deleted"):
		return true
	default:
		return false
	}
}

func getFloodWait(err error) int {
	if err == nil {
		return 0
	}

	type retryError interface {
		GetRetryAfter() int
	}

	if re, ok := err.(retryError); ok {
		return re.GetRetryAfter()
	}

	if tdErr, ok := err.(*td.Error); ok {
		return tdErr.GetRetryAfter()
	}

	if tdErr, ok := err.(td.Error); ok {
		return tdErr.GetRetryAfter()
	}

	return 0
}

func cancelBroadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	bcMu.Lock()
	cancel := bcCancel
	bcMu.Unlock()

	if cancel == nil {
		_, _ = m.ReplyText(c, "No broadcast in progress.", nil)
		return td.EndGroups
	}

	cancel()
	_, _ = m.ReplyText(c, "Stopping the broadcast. Progress is saved; use /broadcast_resume to continue from where it stopped.", nil)
	return td.EndGroups
}

// bcOptions are the flags accepted by /broadcast (and -rate by /broadcast_resume).
type bcOptions struct {
	mode    string // "chat", "user" or "both"
	copy    bool
	rate    float64
	hasRate bool
	fresh   bool // -new: discard an unfinished broadcast and start over
	err     string
}

func parseBroadcastArgs(fields []string) bcOptions {
	o := bcOptions{mode: "both", rate: defaultBroadcastRate}

	setRate := func(v string) {
		r, err := strconv.ParseFloat(v, 64)
		if err != nil || r < broadcast.MinRate || r > broadcast.MaxRate {
			o.err = fmt.Sprintf("Invalid rate %q. Use a number between %.1f and %.0f messages per second.", v, broadcast.MinRate, broadcast.MaxRate)
			return
		}
		o.rate, o.hasRate = r, true
	}

	for i := 0; i < len(fields); i++ {
		a := fields[i]
		switch {
		case a == "-copy":
			o.copy = true
		case a == "-chat":
			o.mode = "chat"
		case a == "-user":
			o.mode = "user"
		case a == "-both":
			o.mode = "both"
		case a == "-new":
			o.fresh = true
		case a == "-rate":
			if i+1 >= len(fields) {
				o.err = "-rate needs a number, e.g. -rate 5.6"
				continue
			}
			i++
			setRate(fields[i])
		case strings.HasPrefix(a, "-rate="):
			setRate(strings.TrimPrefix(a, "-rate="))
		}
	}
	return o
}

// bcCollectTargets loads the reachable targets for mode in one go. Chats and
// users already flagged invalid/blocked/deleted by earlier broadcasts are
// filtered out by the database query itself, so they cost nothing here.
func bcCollectTargets(mode string) ([]int64, error) {
	var all []int64
	if mode == "chat" || mode == "both" {
		chats, err := db.Instance.GetActiveChats()
		if err != nil {
			return nil, fmt.Errorf("loading chats: %w", err)
		}
		all = append(all, chats...)
	}
	if mode == "user" || mode == "both" {
		users, err := db.Instance.GetActiveUsers()
		if err != nil {
			return nil, fmt.Errorf("loading users: %w", err)
		}
		all = append(all, users...)
	}
	return all, nil
}

// bcKnownDeadText reports how many targets were skipped up front because
// earlier runs already flagged them as unreachable.
func bcKnownDeadText(mode string) string {
	var chats, users int64
	if mode != "user" {
		if cc, err := db.Instance.GetChatCounts(); err == nil {
			chats = cc.Invalid
		}
	}
	if mode != "chat" {
		if uc, err := db.Instance.GetUserCounts(); err == nil {
			users = uc.Blocked + uc.Deleted
		}
	}
	if chats+users == 0 {
		return ""
	}
	return fmt.Sprintf("Skipped up front: %s known-dead targets (%s chats, %s users) flagged in earlier runs.",
		bcNum(chats+users), bcNum(chats), bcNum(users))
}

func broadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	if bcRunning() {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return td.EndGroups
	}

	reply, err := m.GetRepliedMessage(c)
	if err != nil {
		usage := `Please reply to a message to broadcast.

Usage:
-chat      : groups only
-user      : users only
-both      : groups + users (default)
-copy      : send as copy
-rate N    : messages per second (default 5.6, max 25)
-new       : discard an unfinished broadcast and start over

Examples:
/broadcast
/broadcast -chat
/broadcast -user -copy -rate 6

Related: /broadcast_resume, /broadcast_rate N, /stop_broadcast
`
		_, _ = m.ReplyText(c, usage, nil)
		return td.EndGroups
	}

	opts := parseBroadcastArgs(strings.Fields(Args(m)))
	if opts.err != "" {
		_, _ = m.ReplyText(c, opts.err, nil)
		return td.EndGroups
	}

	if prev, _ := db.Instance.GetBroadcastState(); prev != nil && !prev.Finished && !opts.fresh {
		pct := 0.0
		if prev.Total > 0 {
			pct = float64(prev.Processed) / float64(prev.Total) * 100
		}
		_, _ = m.ReplyText(c, fmt.Sprintf(
			"An unfinished broadcast exists (%.1f%% done). Use /broadcast_resume to continue it, or /broadcast -new to discard it and start over.", pct), nil)
		return td.EndGroups
	}

	all, err := bcCollectTargets(opts.mode)
	if err != nil {
		_, _ = m.ReplyText(c, "Could not load targets: "+err.Error(), nil)
		return td.EndGroups
	}
	targets := broadcast.NormalizeTargets(all, nil)
	if len(targets) == 0 {
		_, _ = m.ReplyText(c, "No targets found.", nil)
		return td.EndGroups
	}

	st := &db.BroadcastState{
		SourceChatID:    reply.ChatId,
		SourceMessageID: reply.Id,
		Mode:            opts.mode,
		Copy:            opts.copy,
		Rate:            opts.rate,
		Total:           len(targets),
		StartedAt:       time.Now(),
	}
	startBroadcast(c, m, reply, st, targets, bcKnownDeadText(opts.mode))
	return td.EndGroups
}

// broadcastResumeHandler continues the last unfinished broadcast from its
// saved watermark. Reply to the original message to broadcast if the bot can
// no longer fetch it itself.
func broadcastResumeHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	if bcRunning() {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return td.EndGroups
	}

	st, err := db.Instance.GetBroadcastState()
	if err != nil {
		_, _ = m.ReplyText(c, "Could not read the saved broadcast: "+err.Error(), nil)
		return td.EndGroups
	}
	if st == nil || st.Finished {
		_, _ = m.ReplyText(c, "There is no unfinished broadcast to resume.", nil)
		return td.EndGroups
	}

	opts := parseBroadcastArgs(strings.Fields(Args(m)))
	if opts.err != "" {
		_, _ = m.ReplyText(c, opts.err, nil)
		return td.EndGroups
	}
	if opts.hasRate {
		st.Rate = opts.rate
	}
	if st.Rate <= 0 {
		st.Rate = defaultBroadcastRate
	}

	src, err := m.GetRepliedMessage(c)
	if err != nil || src == nil {
		src, err = c.GetMessage(st.SourceChatID, st.SourceMessageID)
		if err != nil || src == nil {
			_, _ = m.ReplyText(c, "Could not re-fetch the original message (it may be deleted or too old). Reply to it with /broadcast_resume to continue with that message.", nil)
			return td.EndGroups
		}
	}
	st.SourceChatID, st.SourceMessageID = src.ChatId, src.Id

	all, err := bcCollectTargets(st.Mode)
	if err != nil {
		_, _ = m.ReplyText(c, "Could not load targets: "+err.Error(), nil)
		return td.EndGroups
	}
	var after *int64
	if st.HasWatermark {
		w := st.Watermark
		after = &w
	}
	targets := broadcast.NormalizeTargets(all, after)
	if len(targets) == 0 {
		st.Finished = true
		_ = db.Instance.SaveBroadcastState(st)
		_, _ = m.ReplyText(c, "Nothing left to send: the previous broadcast had already reached everyone.", nil)
		return td.EndGroups
	}

	startBroadcast(c, m, src, st, targets, "Resuming from the saved position.")
	return td.EndGroups
}

// broadcastRateHandler changes the speed of the running broadcast.
func broadcastRateHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	bcMu.Lock()
	e := bcEngine
	bcMu.Unlock()
	if e == nil {
		_, _ = m.ReplyText(c, "No broadcast in progress.", nil)
		return td.EndGroups
	}

	v, err := strconv.ParseFloat(strings.TrimSpace(Args(m)), 64)
	if err != nil || v <= 0 {
		_, _ = m.ReplyText(c, fmt.Sprintf("Usage: /broadcast_rate N (%.1f to %.0f messages per second)", broadcast.MinRate, broadcast.MaxRate), nil)
		return td.EndGroups
	}

	applied := e.SetRate(v)
	_, _ = m.ReplyText(c, fmt.Sprintf("Broadcast rate set to %.1f messages per second.", applied), nil)
	return td.EndGroups
}

// startBroadcast announces the run and launches it in the background.
func startBroadcast(c *td.Client, m *td.Message, src *td.Message, st *db.BroadcastState, targets []int64, note string) {
	ctx, ok := bcBegin()
	if !ok {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return
	}

	var nChats, nUsers int64
	for _, id := range targets {
		if id < 0 {
			nChats++
		} else {
			nUsers++
		}
	}
	est := time.Duration(float64(len(targets)) / st.Rate * float64(time.Second))
	kind := "forward"
	if st.Copy {
		kind = "copy"
	}

	var sb strings.Builder
	sb.WriteString("Broadcast started.\n")
	fmt.Fprintf(&sb, "Targets: %s (%s groups, %s users)\n", bcNum(int64(len(targets))), bcNum(nChats), bcNum(nUsers))
	if note != "" {
		sb.WriteString(note + "\n")
	}
	fmt.Fprintf(&sb, "Mode: %s | Rate: %.1f/s | Estimated time: %s", kind, st.Rate, bcDuration(est))

	status, _ := m.ReplyText(c, sb.String(), nil)
	if err := db.Instance.SaveBroadcastState(st); err != nil {
		slog.Warn("[Broadcast] could not save initial state", "error", err)
	}

	go runBroadcast(ctx, c, m, status, src, st, targets)
}

// bcDeadBuf collects targets found unreachable during a run so they can be
// flagged in the database in bulk instead of one write per failure.
type bcDeadBuf struct {
	mu      sync.Mutex
	chats   []int64
	blocked []int64
	deleted []int64
}

func (b *bcDeadBuf) add(id int64, tag string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case tag == "blocked":
		b.blocked = append(b.blocked, id)
	case tag == "deleted":
		b.deleted = append(b.deleted, id)
	default:
		b.chats = append(b.chats, id)
	}
}

// flush writes the buffered IDs; anything that fails to save is kept for the
// next flush.
func (b *bcDeadBuf) flush() {
	b.mu.Lock()
	chats, blocked, deleted := b.chats, b.blocked, b.deleted
	b.chats, b.blocked, b.deleted = nil, nil, nil
	b.mu.Unlock()

	if err := db.Instance.MarkChatsInvalid(chats); err != nil {
		slog.Warn("[Broadcast] could not flag dead chats", "error", err)
		b.mu.Lock()
		b.chats = append(b.chats, chats...)
		b.mu.Unlock()
	}
	if err := db.Instance.MarkUsersBlocked(blocked); err != nil {
		slog.Warn("[Broadcast] could not flag blocked users", "error", err)
		b.mu.Lock()
		b.blocked = append(b.blocked, blocked...)
		b.mu.Unlock()
	}
	if err := db.Instance.MarkUsersDeleted(deleted); err != nil {
		slog.Warn("[Broadcast] could not flag deleted users", "error", err)
		b.mu.Lock()
		b.deleted = append(b.deleted, deleted...)
		b.mu.Unlock()
	}
}

// bcClassify turns a send error into what the engine should do about it.
func bcClassify(id int64, err error) broadcast.Outcome {
	if wait := getFloodWait(err); wait > 0 {
		return broadcast.Outcome{Kind: broadcast.Flood, Wait: time.Duration(wait) * time.Second}
	}
	if id < 0 {
		if isChatGoneError(err) {
			return broadcast.Outcome{Kind: broadcast.Dead, Tag: "chat"}
		}
		return broadcast.Outcome{Kind: broadcast.Failed}
	}
	blocked, deleted := isUserGoneError(err)
	switch {
	case blocked:
		return broadcast.Outcome{Kind: broadcast.Dead, Tag: "blocked"}
	case deleted:
		return broadcast.Outcome{Kind: broadcast.Dead, Tag: "deleted"}
	}
	return broadcast.Outcome{Kind: broadcast.Failed}
}

// runBroadcast drives one broadcast to completion (or until it is stopped).
// It owns st: it is the only goroutine that touches it after startBroadcast.
func runBroadcast(ctx context.Context, c *td.Client, m *td.Message, status *td.Message, src *td.Message, st *db.BroadcastState, targets []int64) {
	defer bcEnd()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("[Broadcast] crashed", "panic", r)
			bcEdit(c, status, fmt.Sprintf("Broadcast crashed: %v\nProgress up to the last checkpoint is saved; use /broadcast_resume to continue.", r))
		}
	}()

	base := *st // cumulative counters from before this run (non-zero when resuming)

	dead := &bcDeadBuf{}
	var (
		failMu      sync.Mutex
		failed      strings.Builder
		failedLines int
		failedExtra int
	)

	engine := broadcast.New(broadcast.Config{
		Targets: targets,
		Rate:    st.Rate,
		Send: func(_ context.Context, id int64) error {
			var err error
			if st.Copy {
				_, err = src.Copy(c, id, &td.SendCopyOpts{ReplyMarkup: src.ReplyMarkup})
			} else {
				_, err = src.Forward(c, id, &td.ForwardMessageOpts{})
			}
			return err
		},
		Classify: bcClassify,
		OnDead:   dead.add,
		OnFailed: func(id int64, err error) {
			failMu.Lock()
			defer failMu.Unlock()
			if failedLines >= bcMaxErrorLines {
				failedExtra++
				return
			}
			failedLines++
			fmt.Fprintf(&failed, "%d - %v\n", id, err)
		},
		Logf: func(format string, args ...any) { slog.Warn(fmt.Sprintf("[Broadcast] "+format, args...)) },
	})

	bcMu.Lock()
	bcEngine = engine
	bcMu.Unlock()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		engine.Run(ctx)
	}()

	started := time.Now()
	checkpoint := time.NewTicker(bcCheckpointEvery)
	defer checkpoint.Stop()
	progress := time.NewTicker(bcProgressEvery)
	defer progress.Stop()

loop:
	for {
		select {
		case <-finished:
			break loop
		case <-checkpoint.C:
			bcCheckpoint(st, base, engine, dead, false)
		case <-progress.C:
			bcEdit(c, status, bcProgressText(st, base, engine.Stats(), started, len(targets)))
		}
	}

	stats := engine.Stats()
	complete := stats.Processed >= len(targets)
	bcCheckpoint(st, base, engine, dead, complete)

	summary := fmt.Sprintf("Groups: %s\nUsers: %s\nSkipped (blocked/deleted/invalid): %s\nFailed: %s",
		bcNum(st.SentChats), bcNum(st.SentUsers), bcNum(st.Dead), bcNum(st.Failed))

	if !complete {
		pct := 0.0
		if st.Total > 0 {
			pct = float64(st.Processed) / float64(st.Total) * 100
		}
		bcEdit(c, status, fmt.Sprintf("Broadcast paused at %.1f%%.\n%s\n\nUse /broadcast_resume to continue.", pct, summary))
		return
	}

	text := fmt.Sprintf("Broadcast ended in %s.\n%s", bcDuration(time.Since(started)), summary)

	failMu.Lock()
	failedStr := failed.String()
	if failedExtra > 0 {
		failedStr += fmt.Sprintf("... and %d more failures not listed.\n", failedExtra)
	}
	failMu.Unlock()

	if failedStr == "" {
		bcEdit(c, status, text)
		return
	}

	errFile := filepath.Join(os.TempDir(), fmt.Sprintf("errors_%d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(errFile, []byte(failedStr), 0644); err != nil {
		bcEdit(c, status, text)
		return
	}
	defer os.Remove(errFile)

	if _, err := m.ReplyDocument(c, td.InputFileLocal{Path: errFile}, &td.SendDocumentOpts{Caption: text}); err != nil {
		bcEdit(c, status, text)
	}
}

// bcCheckpoint saves progress and flushes buffered dead targets.
func bcCheckpoint(st *db.BroadcastState, base db.BroadcastState, e *broadcast.Engine, dead *bcDeadBuf, finished bool) {
	s := e.Stats()
	if wm, ok := e.Watermark(); ok {
		st.Watermark, st.HasWatermark = wm, true
	}
	st.Processed = base.Processed + s.Processed
	st.SentChats = base.SentChats + s.SentChats
	st.SentUsers = base.SentUsers + s.SentUsers
	st.Dead = base.Dead + s.Dead
	st.Failed = base.Failed + s.Failed
	st.Rate = s.Rate
	st.Finished = finished

	dead.flush()
	if err := db.Instance.SaveBroadcastState(st); err != nil {
		slog.Warn("[Broadcast] could not save progress", "error", err)
	}
}

func bcProgressText(st *db.BroadcastState, base db.BroadcastState, s broadcast.Stats, started time.Time, runTotal int) string {
	done := int64(base.Processed + s.Processed)
	pct := 0.0
	if st.Total > 0 {
		pct = float64(done) / float64(st.Total) * 100
	}

	remaining := runTotal - s.Processed
	var eta time.Duration
	if elapsed := time.Since(started).Seconds(); s.Processed >= 20 && elapsed > 0 {
		eta = time.Duration(float64(remaining) / (float64(s.Processed) / elapsed) * float64(time.Second))
	} else if s.Rate > 0 {
		eta = time.Duration(float64(remaining) / s.Rate * float64(time.Second))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Broadcast in progress: %.1f%%\n", pct)
	fmt.Fprintf(&sb, "Processed: %s / %s\n", bcNum(done), bcNum(int64(st.Total)))
	fmt.Fprintf(&sb, "Delivered: %s groups, %s users\n", bcNum(base.SentChats+s.SentChats), bcNum(base.SentUsers+s.SentUsers))
	fmt.Fprintf(&sb, "Skipped (dead): %s | Failed: %s\n", bcNum(base.Dead+s.Dead), bcNum(base.Failed+s.Failed))
	fmt.Fprintf(&sb, "Rate: %.1f/s | ETA: %s", s.Rate, bcDuration(eta))
	if s.Paused {
		sb.WriteString("\nPaused by a Telegram flood wait, resuming automatically.")
	}
	sb.WriteString("\n\n/broadcast_rate N to change speed | /stop_broadcast to pause")
	return sb.String()
}

func bcEdit(c *td.Client, msg *td.Message, text string) {
	if msg == nil {
		return
	}
	if _, err := msg.EditText(c, text, nil); err != nil {
		slog.Debug("[Broadcast] status edit failed", "error", err)
	}
}

// bcNum formats n with thousands separators.
func bcNum(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// bcDuration formats d as "45s", "12m" or "2h 05m".
func bcDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
