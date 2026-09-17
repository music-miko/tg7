package ntgcalls

//#include "ntgcalls.h"
//#include <stdlib.h>
//extern void handleStreamEnd(uintptr_t ptr, int64_t chatID, ntg_stream_type_enum streamType, ntg_stream_device_enum streamDevice, void*);
//extern void handleUpgrade(uintptr_t ptr, int64_t chatID, ntg_media_state_struct state, void*);
//extern void handleConnectionChange(uintptr_t ptr, int64_t chatID, ntg_network_info_struct networkInfo, void*);
//extern void handleSignal(uintptr_t ptr, int64_t chatID, uint8_t*, int, void*);
//extern void handleFrames(uintptr_t ptr, int64_t chatID, ntg_stream_mode_enum streamMode, ntg_stream_device_enum streamDevice, ntg_frame_struct* frames, uint64_t size, void*);
//extern void handleRemoteSourceChange(uintptr_t ptr, int64_t chatID, ntg_remote_source_struct remoteSource, void*);
//extern void handleRequestBroadcastTimestamp(uintptr_t ptr, int64_t chatID, void*);
//extern void handleRequestBroadcastPart(uintptr_t ptr, int64_t chatID, ntg_segment_part_request_struct segmentPartRequest, void*);
//extern void handleLogs(ntg_log_message_struct logMessage);
import "C"
import (
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"
)

// nativeCallTimeout bounds how long Go will wait for the native ntgcalls
// engine to complete a single async call. Without it, a wedged native worker
// thread blocks the caller forever.
//
// 15s is generous for these operations - they are engine-local bookkeeping,
// not network round-trips - while still surfacing a stuck assistant as a
// user-visible error in well under a minute.
//
// Unlike the original implementation, a timeout here no longer strands a
// goroutine (see future.go) and no longer abandons C memory that the engine
// may still write to. It also feeds the per-engine circuit breaker
// (health.go), so a dead engine stops costing 15s per call almost immediately.
const nativeCallTimeout = 15 * time.Second

// DefaultCallTimeout returns the timeout used to bound native calls, for
// callers that want to match it (e.g. when calling CallsTimeout directly).
func DefaultCallTimeout() time.Duration {
	return nativeCallTimeout
}

func init() {
	C.ntg_register_logger((C.ntg_log_message_callback)(unsafe.Pointer(C.handleLogs)))
}

// wait bounds f by nativeCallTimeout and keeps the engine's health record up
// to date. op/chatId are only used for the log line.
func (ctx *Client) wait(f *Future, op string, chatId int64) error {
	if !f.WaitTimeout(nativeCallTimeout) {
		ctx.health.timeout()
		loggerNTGCalls.Error(fmt.Sprintf(
			"%s timed out after %s for chat_id=%d - native engine unresponsive (pending native calls: %d)",
			op, nativeCallTimeout, chatId, PendingFutures(),
		))
		return ErrNativeTimeout
	}
	ctx.health.success()
	return parseErrorCode(f)
}

var (
	loggerNTGCalls = NewLogger("ntgcalls", LevelInfo)
	loggerWebRTC   = NewLogger("webrtc", LevelFatal)
)

// ---------------------------------------------------------------------------
// Bounded callback dispatch
// ---------------------------------------------------------------------------
//
// The native engine invokes Go callbacks from its own worker threads. The
// original code answered every one with a bare `go x0(...)`, including for
// the high-frequency callbacks: handleFrames fires per frame, and
// handleRequestBroadcastPart fires per requested segment. At 60-70 live chats
// that is thousands of goroutine spawns a second if those callbacks are ever
// registered, with nothing bounding them.
//
// High-frequency callbacks now go through a bounded pool and are dropped when
// it saturates. Dropping is the correct behaviour here: the engine re-requests
// segments it does not receive, and a dropped frame notification is by
// definition already stale. Low-frequency, semantically important callbacks
// (stream end, connection change) still get a goroutine each.
const callbackPoolSize = 256

var (
	callbackSem     = make(chan struct{}, callbackPoolSize)
	droppedCallback atomic.Uint64
)

// dispatchBounded runs fn on a pooled goroutine, dropping it if the pool is
// saturated. It never blocks the calling native thread, because blocking
// there is what stalls the whole engine.
func dispatchBounded(fn func()) {
	select {
	case callbackSem <- struct{}{}:
		go func() {
			defer func() { <-callbackSem }()
			fn()
		}()
	default:
		if n := droppedCallback.Add(1); n%1000 == 1 {
			loggerNTGCalls.Warn(fmt.Sprintf(
				"callback pool saturated (%d slots), dropped %d high-frequency callback(s) so far",
				callbackPoolSize, n,
			))
		}
	}
}

// DroppedCallbacks reports how many high-frequency callbacks have been shed
// because the pool was saturated. Useful for /stats.
func DroppedCallbacks() uint64 {
	return droppedCallback.Load()
}

func NTgCalls() *Client {
	instance := &Client{
		ptr: uintptr(C.ntg_init()),
	}
	selfPointer := unsafe.Pointer(instance)
	C.ntg_on_stream_end(C.uintptr_t(instance.ptr), (C.ntg_stream_callback)(unsafe.Pointer(C.handleStreamEnd)), selfPointer)
	C.ntg_on_upgrade(C.uintptr_t(instance.ptr), (C.ntg_upgrade_callback)(unsafe.Pointer(C.handleUpgrade)), selfPointer)
	C.ntg_on_connection_change(C.uintptr_t(instance.ptr), (C.ntg_connection_callback)(unsafe.Pointer(C.handleConnectionChange)), selfPointer)
	C.ntg_on_frames(C.uintptr_t(instance.ptr), (C.ntg_frame_callback)(unsafe.Pointer(C.handleFrames)), selfPointer)
	C.ntg_on_remote_source_change(C.uintptr_t(instance.ptr), (C.ntg_remote_source_callback)(unsafe.Pointer(C.handleRemoteSourceChange)), selfPointer)
	C.ntg_on_request_broadcast_timestamp(C.uintptr_t(instance.ptr), (C.ntg_broadcast_timestamp_callback)(unsafe.Pointer(C.handleRequestBroadcastTimestamp)), selfPointer)
	C.ntg_on_request_broadcast_part(C.uintptr_t(instance.ptr), (C.ntg_broadcast_part_callback)(unsafe.Pointer(C.handleRequestBroadcastPart)), selfPointer)
	return instance
}

//export handleLogs
func handleLogs(logMessage C.ntg_log_message_struct) {
	message := fmt.Sprintf(
		"(%v:%v) %v",
		C.GoString(logMessage.file),
		uint32(logMessage.line),
		C.GoString(logMessage.message),
	)

	var lg *Logger
	if logMessage.source == C.NTG_LOG_WEBRTC {
		lg = loggerWebRTC
	} else {
		lg = loggerNTGCalls
	}

	switch logMessage.level {
	case C.NTG_LOG_DEBUG:
		lg.Debug(message)
	case C.NTG_LOG_INFO:
		lg.Info(message)
	case C.NTG_LOG_WARNING:
		lg.Warn(message)
	case C.NTG_LOG_ERROR:
		lg.Error(message)
	}
}

//export handleStreamEnd
func handleStreamEnd(_ C.uintptr_t, chatID C.int64_t, streamType C.ntg_stream_type_enum, streamDevice C.ntg_stream_device_enum, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	goChatID := int64(chatID)
	var goStreamType StreamType
	if streamType == C.NTG_STREAM_AUDIO {
		goStreamType = AudioStream
	} else {
		goStreamType = VideoStream
	}
	device := parseStreamDevice(streamDevice)
	for _, x0 := range self.streamEndCallbacks {
		cb := x0
		go cb(goChatID, goStreamType, device)
	}
}

//export handleUpgrade
func handleUpgrade(_ C.uintptr_t, chatID C.int64_t, state C.ntg_media_state_struct, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	goChatID := int64(chatID)
	goState := MediaState{
		Muted:              bool(state.muted),
		VideoPaused:        bool(state.videoPaused),
		VideoStopped:       bool(state.videoStopped),
		PresentationPaused: bool(state.presentationPaused),
	}
	for _, x0 := range self.upgradeCallbacks {
		cb := x0
		go cb(goChatID, goState)
	}
}

//export handleConnectionChange
func handleConnectionChange(_ C.uintptr_t, chatID C.int64_t, networkInfo C.ntg_network_info_struct, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	goChatID := int64(chatID)
	var goCallState NetworkInfo
	switch networkInfo.kind {
	case C.NTG_KIND_NORMAL:
		goCallState.Kind = NormalConnection
	case C.NTG_KIND_PRESENTATION:
		goCallState.Kind = PresentationConnection
	}
	goCallState.State = parseConnectionState(networkInfo.state)
	for _, x0 := range self.connectionChangeCallbacks {
		cb := x0
		go cb(goChatID, goCallState)
	}
}

//export handleFrames
func handleFrames(_ C.uintptr_t, chatID C.int64_t, streamMode C.ntg_stream_mode_enum, streamDevice C.ntg_stream_device_enum, frames *C.ntg_frame_struct, size C.uint64_t, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	if len(self.frameCallbacks) == 0 {
		return
	}

	goChatID := int64(chatID)
	var goStreamMode StreamMode
	switch streamMode {
	case C.NTG_STREAM_CAPTURE:
		goStreamMode = CaptureStream
	case C.NTG_STREAM_PLAYBACK:
		goStreamMode = PlaybackStream
	}
	rawFrames := make([]Frame, size)
	for i := uint64(0); i < uint64(size); i++ {
		rawFrame := *(*C.ntg_frame_struct)(unsafe.Pointer(uintptr(unsafe.Pointer(frames)) + uintptr(i)*unsafe.Sizeof(C.ntg_frame_struct{})))
		rawFrames[i] = Frame{
			Ssrc: uint32(rawFrame.ssrc),
			Data: C.GoBytes(unsafe.Pointer(rawFrame.data), rawFrame.sizeData),
			FrameData: FrameData{
				AbsoluteCaptureTimestampMs: int64(frames.frameData.absoluteCaptureTimestampMs),
				Width:                      uint16(frames.frameData.width),
				Height:                     uint16(frames.frameData.height),
				Rotation:                   uint16(frames.frameData.rotation),
			},
		}
	}
	device := parseStreamDevice(streamDevice)
	for _, x0 := range self.frameCallbacks {
		cb := x0
		dispatchBounded(func() { cb(goChatID, goStreamMode, device, rawFrames) })
	}
}

//export handleRemoteSourceChange
func handleRemoteSourceChange(_ C.uintptr_t, chatID C.int64_t, remoteSource C.ntg_remote_source_struct, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	goChatID := int64(chatID)
	goRemoteSource := RemoteSource{
		Ssrc:   uint32(remoteSource.ssrc),
		State:  parseStreamStatus(remoteSource.state),
		Device: parseStreamDevice(remoteSource.device),
	}
	for _, x0 := range self.remoteSourceCallbacks {
		cb := x0
		dispatchBounded(func() { cb(goChatID, goRemoteSource) })
	}
}

//export handleRequestBroadcastTimestamp
func handleRequestBroadcastTimestamp(_ C.uintptr_t, chatID C.int64_t, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	goChatID := int64(chatID)
	for _, x0 := range self.broadcastTimestampCallbacks {
		cb := x0
		dispatchBounded(func() { cb(goChatID) })
	}
}

//export handleRequestBroadcastPart
func handleRequestBroadcastPart(_ C.uintptr_t, chatID C.int64_t, segmentPartRequest C.ntg_segment_part_request_struct, ptr unsafe.Pointer) {
	self := (*Client)(ptr)
	goChatID := int64(chatID)
	var goSegmentQuality MediaSegmentQuality
	switch segmentPartRequest.quality {
	case C.NTG_MEDIA_SEGMENT_QUALITY_NONE:
		goSegmentQuality = SegmentQualityNone
	case C.NTG_MEDIA_SEGMENT_QUALITY_THUMBNAIL:
		goSegmentQuality = SegmentQualityThumbnail
	case C.NTG_MEDIA_SEGMENT_QUALITY_MEDIUM:
		goSegmentQuality = SegmentQualityMedium
	case C.NTG_MEDIA_SEGMENT_QUALITY_FULL:
		goSegmentQuality = SegmentQualityFull
	}
	goSegmentPartRequest := SegmentPartRequest{
		SegmentID:     int64(segmentPartRequest.segmentId),
		PartID:        int32(segmentPartRequest.partId),
		Limit:         int32(segmentPartRequest.limit),
		Timestamp:     int64(segmentPartRequest.timestamp),
		QualityUpdate: bool(segmentPartRequest.qualityUpdate),
		ChannelID:     int32(segmentPartRequest.channelId),
		Quality:       goSegmentQuality,
	}
	for _, x0 := range self.broadcastPartCallbacks {
		cb := x0
		dispatchBounded(func() { cb(goChatID, goSegmentPartRequest) })
	}
}

func (ctx *Client) OnStreamEnd(callback StreamEndCallback) {
	ctx.streamEndCallbacks = append(ctx.streamEndCallbacks, callback)
}

func (ctx *Client) OnUpgrade(callback UpgradeCallback) {
	ctx.upgradeCallbacks = append(ctx.upgradeCallbacks, callback)
}

func (ctx *Client) OnConnectionChange(callback ConnectionChangeCallback) {
	ctx.connectionChangeCallbacks = append(ctx.connectionChangeCallbacks, callback)
}

func (ctx *Client) OnFrame(callback FrameCallback) {
	ctx.frameCallbacks = append(ctx.frameCallbacks, callback)
}

func (ctx *Client) OnRemoteSourceChange(callback RemoteSourceCallback) {
	ctx.remoteSourceCallbacks = append(ctx.remoteSourceCallbacks, callback)
}

func (ctx *Client) OnRequestBroadcastTimestamp(callback BroadcastTimestampCallback) {
	ctx.broadcastTimestampCallbacks = append(ctx.broadcastTimestampCallbacks, callback)
}

func (ctx *Client) OnRequestBroadcastPart(callback BroadcastPartCallback) {
	ctx.broadcastPartCallbacks = append(ctx.broadcastPartCallbacks, callback)
}

// ---------------------------------------------------------------------------
// Native calls
// ---------------------------------------------------------------------------
//
// Every method below follows the same shape:
//
//	if err := ctx.health.begin(); err != nil { return err }   // fail fast
//	f := CreateFuture()
//	defer f.Release()                                          // frees C memory
//	<out-params allocated via f.allocC, inputs via f.cString>
//	C.ntg_xxx(..., f.ParseToC())
//	if err := ctx.wait(f, "ntg_xxx", chatId); err != nil { ... }
//
// The out-parameter allocation is the important detail: these calls complete
// asynchronously, so passing &someGoLocal means the engine writes into a
// stack frame that has already been reused by the time it gets there.

func (ctx *Client) GetState(chatId int64) (MediaState, error) {
	if err := ctx.health.begin(); err != nil {
		return MediaState{}, err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (*C.ntg_media_state_struct)(f.allocC(C.size_t(unsafe.Sizeof(C.ntg_media_state_struct{}))))
	C.ntg_get_state(C.uintptr_t(ctx.ptr), C.int64_t(chatId), buffer, f.ParseToC())
	if err := ctx.wait(f, "ntg_get_state", chatId); err != nil {
		return MediaState{}, err
	}

	return MediaState{
		Muted:              bool(buffer.muted),
		VideoPaused:        bool(buffer.videoPaused),
		VideoStopped:       bool(buffer.videoStopped),
		PresentationPaused: bool(buffer.presentationPaused),
	}, nil
}

func (ctx *Client) GetConnectionMode(chatId int64) (ConnectionMode, error) {
	if err := ctx.health.begin(); err != nil {
		return ConnectionMode(0), err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (*C.ntg_connection_mode_enum)(f.allocC(C.size_t(unsafe.Sizeof(C.ntg_connection_mode_enum(0)))))
	C.ntg_get_connection_mode(C.uintptr_t(ctx.ptr), C.int64_t(chatId), buffer, f.ParseToC())
	if err := ctx.wait(f, "ntg_get_connection_mode", chatId); err != nil {
		return ConnectionMode(0), err
	}

	switch *buffer {
	case C.NTG_CONNECTION_MODE_RTC:
		return RtcConnection, nil
	case C.NTG_CONNECTION_MODE_STREAM:
		return StreamConnection, nil
	case C.NTG_CONNECTION_MODE_RTMP:
		return RTMPConnection, nil
	default:
		return ConnectionMode(0), fmt.Errorf("unknown connection mode")
	}
}

func (ctx *Client) CreateCall(chatId int64) (string, error) {
	if err := ctx.health.begin(); err != nil {
		return "", err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (**C.char)(f.allocC(C.size_t(unsafe.Sizeof((*C.char)(nil)))))
	C.ntg_create(C.uintptr_t(ctx.ptr), C.int64_t(chatId), buffer, f.ParseToC())
	if err := ctx.wait(f, "ntg_create", chatId); err != nil {
		return "", err
	}

	// The engine allocates this string; we own it from here.
	result := C.GoString(*buffer)
	if *buffer != nil {
		C.free(unsafe.Pointer(*buffer))
		*buffer = nil
	}
	return result, nil
}

func (ctx *Client) InitPresentation(chatId int64) (string, error) {
	if err := ctx.health.begin(); err != nil {
		return "", err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (**C.char)(f.allocC(C.size_t(unsafe.Sizeof((*C.char)(nil)))))
	C.ntg_init_presentation(C.uintptr_t(ctx.ptr), C.int64_t(chatId), buffer, f.ParseToC())
	if err := ctx.wait(f, "ntg_init_presentation", chatId); err != nil {
		return "", err
	}

	result := C.GoString(*buffer)
	if *buffer != nil {
		C.free(unsafe.Pointer(*buffer))
		*buffer = nil
	}
	return result, nil
}

func (ctx *Client) StopPresentation(chatId int64) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_stop_presentation(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.ParseToC())
	return ctx.wait(f, "ntg_stop_presentation", chatId)
}

func (ctx *Client) AddIncomingVideo(chatId int64, endpoint string, ssrcGroups []SsrcGroup) (uint32, error) {
	if err := ctx.health.begin(); err != nil {
		return 0, err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (*C.uint32_t)(f.allocC(C.size_t(unsafe.Sizeof(C.uint32_t(0)))))
	C.ntg_add_incoming_video(
		C.uintptr_t(ctx.ptr),
		C.int64_t(chatId),
		f.cString(endpoint),
		parseSsrcGroupsF(f, ssrcGroups),
		C.int(len(ssrcGroups)),
		buffer,
		f.ParseToC(),
	)
	if err := ctx.wait(f, "ntg_add_incoming_video", chatId); err != nil {
		return 0, err
	}
	return uint32(*buffer), nil
}

func (ctx *Client) RemoveIncomingVideo(chatId int64, endpoint string) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_remove_incoming_video(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.cString(endpoint), f.ParseToC())
	return ctx.wait(f, "ntg_remove_incoming_video", chatId)
}

//goland:noinspection GoUnusedExportedFunction
func GetProtocol() Protocol {
	var buffer C.ntg_protocol_struct
	C.ntg_get_protocol(&buffer)
	return Protocol{
		MinLayer:     int32(buffer.minLayer),
		MaxLayer:     int32(buffer.maxLayer),
		UdpReflector: bool(buffer.udpReflector),
		Versions:     parseStringVector(unsafe.Pointer(buffer.libraryVersions), buffer.libraryVersionsSize),
	}
}

func (ctx *Client) Connect(chatId int64, params string, isPresentation bool) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_connect(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.cString(params), C.bool(isPresentation), f.ParseToC())
	return ctx.wait(f, "ntg_connect", chatId)
}

func (ctx *Client) SetStreamSources(chatId int64, streamMode StreamMode, desc MediaDescription) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_set_stream_sources(C.uintptr_t(ctx.ptr), C.int64_t(chatId), streamMode.ParseToC(), desc.parseToC(f), f.ParseToC())
	return ctx.wait(f, "ntg_set_stream_sources", chatId)
}

func (ctx *Client) SendExternalFrame(chatId int64, streamDevice StreamDevice, data []byte, frameData FrameData) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	dataC, dataSize := f.cBytes(data)
	C.ntg_send_external_frame(C.uintptr_t(ctx.ptr), C.int64_t(chatId), streamDevice.ParseToC(), dataC, dataSize, frameData.ParseToC(), f.ParseToC())
	return ctx.wait(f, "ntg_send_external_frame", chatId)
}

func (ctx *Client) SendBroadcastTimestamp(chatId int64, timestamp int64) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_send_broadcast_timestamp(C.uintptr_t(ctx.ptr), C.int64_t(chatId), C.int64_t(timestamp), f.ParseToC())
	return ctx.wait(f, "ntg_send_broadcast_timestamp", chatId)
}

func (ctx *Client) SendBroadcastPart(chatId int64, segmentID int64, partID int32, status MediaSegmentStatus, qualityUpdate bool, data []byte) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	dataC, dataSize := f.cBytes(data)
	C.ntg_send_broadcast_part(C.uintptr_t(ctx.ptr), C.int64_t(chatId), C.int64_t(segmentID), C.int32_t(partID), status.ParseToC(), C.bool(qualityUpdate), dataC, dataSize, f.ParseToC())
	return ctx.wait(f, "ntg_send_broadcast_part", chatId)
}

func (ctx *Client) Pause(chatId int64) (bool, error) {
	if err := ctx.health.begin(); err != nil {
		return false, err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_pause(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.ParseToC())
	if err := ctx.wait(f, "ntg_pause", chatId); err != nil {
		return false, err
	}
	return true, nil
}

func (ctx *Client) Resume(chatId int64) (bool, error) {
	if err := ctx.health.begin(); err != nil {
		return false, err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_resume(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.ParseToC())
	if err := ctx.wait(f, "ntg_resume", chatId); err != nil {
		return false, err
	}
	return true, nil
}

func (ctx *Client) Mute(chatId int64) (bool, error) {
	if err := ctx.health.begin(); err != nil {
		return false, err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_mute(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.ParseToC())
	if err := ctx.wait(f, "ntg_mute", chatId); err != nil {
		return false, err
	}
	return true, nil
}

func (ctx *Client) UnMute(chatId int64) (bool, error) {
	if err := ctx.health.begin(); err != nil {
		return false, err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_unmute(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.ParseToC())
	if err := ctx.wait(f, "ntg_unmute", chatId); err != nil {
		return false, err
	}
	return true, nil
}

func (ctx *Client) Stop(chatId int64) error {
	if err := ctx.health.begin(); err != nil {
		return err
	}

	f := CreateFuture()
	defer f.Release()

	C.ntg_stop(C.uintptr_t(ctx.ptr), C.int64_t(chatId), f.ParseToC())
	return ctx.wait(f, "ntg_stop", chatId)
}

func (ctx *Client) Time(chatId int64, streamMode StreamMode) (uint64, error) {
	if err := ctx.health.begin(); err != nil {
		return 0, err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (*C.int64_t)(f.allocC(C.size_t(unsafe.Sizeof(C.int64_t(0)))))
	C.ntg_time(C.uintptr_t(ctx.ptr), C.int64_t(chatId), streamMode.ParseToC(), buffer, f.ParseToC())
	if err := ctx.wait(f, "ntg_time", chatId); err != nil {
		return 0, err
	}
	return uint64(*buffer), nil
}

//goland:noinspection GoUnusedExportedFunction
func GetMediaDevices() MediaDevices {
	var buffer C.ntg_media_devices_struct
	C.ntg_get_media_devices(&buffer)
	return MediaDevices{
		Microphone: parseDeviceInfoVector(unsafe.Pointer(buffer.microphone), buffer.sizeMicrophone),
		Speaker:    parseDeviceInfoVector(unsafe.Pointer(buffer.speaker), buffer.sizeSpeaker),
		Camera:     parseDeviceInfoVector(unsafe.Pointer(buffer.camera), buffer.sizeCamera),
		Screen:     parseDeviceInfoVector(unsafe.Pointer(buffer.screen), buffer.sizeScreen),
	}
}

func (ctx *Client) CpuUsage() (float64, error) {
	if err := ctx.health.begin(); err != nil {
		return 0, err
	}

	f := CreateFuture()
	defer f.Release()

	buffer := (*C.double)(f.allocC(C.size_t(unsafe.Sizeof(C.double(0)))))
	C.ntg_cpu_usage(C.uintptr_t(ctx.ptr), buffer, f.ParseToC())
	if err := ctx.wait(f, "ntg_cpu_usage", 0); err != nil {
		return 0, err
	}
	return float64(*buffer), nil
}

func (ctx *Client) EnableGLibLoop(enable bool) {
	C.ntg_enable_g_lib_loop(C.bool(enable))
}

// Calls returns the native engine's bookkeeping for all active chats.
//
// This is an engine-wide call: it takes an internal lock and marshals every
// active chat, so its cost grows with the number of live voice chats. It was
// previously on the hot path of every single play (Assistant.Play called it
// just to ask "is this one chat already connected?"), which is why
// "ntg_calls timed out after 15s" was the first symptom to appear once ~60-70
// chats were live. The vc layer now keeps its own per-chat activity set and
// only falls back here when that set is not authoritative.
func (ctx *Client) Calls() map[int64]*CallInfo {
	m, _ := ctx.CallsTimeout(nativeCallTimeout)
	return m
}

// CallsTimeout is like Calls but lets the caller pick the timeout and learn
// whether the native call actually completed.
func (ctx *Client) CallsTimeout(d time.Duration) (map[int64]*CallInfo, error) {
	if err := ctx.health.begin(); err != nil {
		return nil, err
	}

	f := CreateFuture()
	defer f.Release()

	bufferPtr := (**C.ntg_call_info_struct)(f.allocC(C.size_t(unsafe.Sizeof((*C.ntg_call_info_struct)(nil)))))
	sizePtr := (*C.int)(f.allocC(C.size_t(unsafe.Sizeof(C.int(0)))))

	C.ntg_calls(C.uintptr_t(ctx.ptr), bufferPtr, sizePtr, f.ParseToC())

	if !f.WaitTimeout(d) {
		ctx.health.timeout()
		loggerNTGCalls.Error(fmt.Sprintf(
			"ntg_calls timed out after %s - native engine unresponsive (pending native calls: %d)",
			d, PendingFutures(),
		))
		return nil, ErrNativeTimeout
	}
	ctx.health.success()

	if err := parseErrorCode(f); err != nil {
		return nil, err
	}

	buffer := *bufferPtr
	size := int(*sizePtr)

	mapReturn := make(map[int64]*CallInfo, size)
	for i := 0; i < size; i++ {
		rawCall := *(*C.ntg_call_info_struct)(unsafe.Pointer(uintptr(unsafe.Pointer(buffer)) + uintptr(i)*unsafe.Sizeof(C.ntg_call_info_struct{})))
		mapReturn[int64(rawCall.chatId)] = &CallInfo{
			Playback: parseStreamStatus(rawCall.playback),
			Capture:  parseStreamStatus(rawCall.capture),
		}
	}

	if buffer != nil {
		C.free(unsafe.Pointer(buffer))
		*bufferPtr = nil
	}
	return mapReturn, nil
}

//goland:noinspection GoUnusedExportedFunction
func Version() string {
	var buffer *C.char
	C.ntg_get_version(&buffer)
	defer C.free(unsafe.Pointer(buffer))
	return C.GoString(buffer)
}

func (ctx *Client) Free() {
	C.ntg_destroy(C.uintptr_t(ctx.ptr))
}
