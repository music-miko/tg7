/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/config"
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	td "github.com/AshokShau/gotdbot"
)

func runShellCommand(cmd string, timeout time.Duration) (string, string, int) {
	var shell string
	var args []string

	if runtime.GOOS == "windows" {
		shell = "cmd"
		args = []string{"/C", cmd}
	} else {
		shell = "bash"
		args = []string{"-c", cmd}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, shell, args...)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Sprintf("Command timed out after %v seconds", timeout.Seconds()), -1
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), exitCode
}

// shellRunner runs one or more shell commands and reports the results back
// as an ephemeral message (Bot API 10.3 - see richtext.go), visible only
// to the dev who ran /sh, since shell output (paths, env-derived values,
// stack traces, ...) shouldn't sit around readable by everyone else in
// whatever chat the command happened to be run from.
func shellRunner(c *td.Client, m *td.Message) error {
	args := strings.TrimSpace(Args(m))
	if args == "" {
		_, _ = m.ReplyText(c, "Usage: /sh cmd", nil)
		return td.EndGroups
	}

	receiverUserId := m.SenderID()

	msg, err := m.ReplyText(c, "Running...", &td.SendTextMessageOpts{ReceiverUserID: receiverUserId})
	if err != nil {
		return td.EndGroups
	}

	commands := strings.Split(args, "\n")
	var outputParts []string

	for _, cmd := range commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}

		stdout, stderr, code := runShellCommand(cmd, 300*time.Second)
		part := fmt.Sprintf("<b>Command:</b> <code>%s</code>\n", html.EscapeString(cmd))
		if stdout != "" {
			part += fmt.Sprintf("<b>Output:</b>\n<pre>%s</pre>\n", html.EscapeString(stdout))
		}

		if stderr != "" {
			part += fmt.Sprintf("<b>Error:</b>\n<pre>%s</pre>\n", html.EscapeString(stderr))
		}
		part += fmt.Sprintf("<b>Exit Code:</b> <code>%d</code>\n", code)
		outputParts = append(outputParts, part)
	}

	finalOutput := strings.Join(outputParts, "\n")
	if strings.TrimSpace(finalOutput) == "" {
		finalOutput = "<b>📭 No output was returned</b>"
	}

	if len(finalOutput) <= 3500 {
		if err := c.EditEphemeralMessageText(m.ChatId, msg.EphemeralMessageId, receiverUserId, finalOutput, &td.EditEphemeralMessageTextOpts{ParseMode: "HTML"}); err != nil {
			c.Logger.Warn("Failed to edit ephemeral shell output", "error", err)
		}
		return td.EndGroups
	}

	file := filepath.Join(config.DownloadsDir, fmt.Sprintf("%d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(file, []byte(finalOutput), 0644); err != nil {
		_ = c.EditEphemeralMessageText(m.ChatId, msg.EphemeralMessageId, receiverUserId, fmt.Sprintf("Failed to write output: %v", err), nil)
		return td.EndGroups
	}
	defer os.Remove(file)

	if err := c.EditEphemeralMessageMedia(m.ChatId, msg.EphemeralMessageId, receiverUserId, td.InputMessageDocument{
		Document: &td.InputDocument{Document: td.InputFileLocal{Path: file}},
	}, nil); err != nil {
		_ = c.EditEphemeralMessageText(m.ChatId, msg.EphemeralMessageId, receiverUserId, "Error: "+err.Error(), nil)
		return td.EndGroups
	}

	return td.EndGroups
}

// shellCommand handles /sh commands
func shellCommand(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	return shellRunner(c, m)
}
