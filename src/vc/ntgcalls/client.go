package ntgcalls

// Client wraps one native ntgcalls engine instance.
//
// health was added alongside the Future rewrite: a single *Client is shared by
// every chat assigned to an assistant, so when its engine wedges the failure
// is engine-wide, not per-chat. Tracking that here lets callers route around
// a dead engine instead of rediscovering it on every call. See health.go.
type Client struct {
	ptr                         uintptr
	connectionChangeCallbacks   []ConnectionChangeCallback
	streamEndCallbacks          []StreamEndCallback
	upgradeCallbacks            []UpgradeCallback
	frameCallbacks              []FrameCallback
	remoteSourceCallbacks       []RemoteSourceCallback
	broadcastTimestampCallbacks []BroadcastTimestampCallback
	broadcastPartCallbacks      []BroadcastPartCallback

	health engineHealth
}
