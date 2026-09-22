package ntgcalls

import "sync/atomic"

type Client struct {
	ptr                         uintptr
	unhealthy                   atomic.Bool
	connectionChangeCallbacks   []ConnectionChangeCallback
	streamEndCallbacks          []StreamEndCallback
	upgradeCallbacks            []UpgradeCallback
	frameCallbacks              []FrameCallback
	remoteSourceCallbacks       []RemoteSourceCallback
	broadcastTimestampCallbacks []BroadcastTimestampCallback
	broadcastPartCallbacks      []BroadcastPartCallback
}

// MarkUnhealthy marks the client engine as unresponsive.
func (ctx *Client) MarkUnhealthy() {
	ctx.unhealthy.Store(true)
}

// MarkHealthy clears the unresponsive flag on the client engine.
func (ctx *Client) MarkHealthy() {
	ctx.unhealthy.Store(false)
}

// IsUnhealthy returns true if the native engine has timed out or become unresponsive.
func (ctx *Client) IsUnhealthy() bool {
	return ctx.unhealthy.Load()
}
