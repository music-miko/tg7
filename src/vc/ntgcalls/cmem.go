package ntgcalls

/*
#include "ntgcalls.h"
#include <stdlib.h>
*/
import "C"

import "unsafe"

// This file holds the Future-owned replacements for the old conversion
// helpers in utils.go.
//
// The originals had two problems on the async call path:
//
//   - They leaked. C.CString(ctx.Input) in AudioDescription/VideoDescription
//     allocated the full ffmpeg command line - several hundred bytes - on
//     every SetStreamSources, and nothing ever freed it. Across dozens of
//     chats changing tracks continuously that is a steady, unbounded malloc
//     leak, which shows up as RSS growth and, eventually, an engine that
//     stops responding promptly.
//
//   - They handed Go memory to C for asynchronous use. MediaDescription
//     pointed its C struct fields at Go locals (&microphone), and
//     parseSsrcGroups returned &rawGroups[0], the backing array of a Go
//     slice. Those stay valid for the duration of a synchronous cgo call, but
//     these calls are not synchronous - the engine may read them after the Go
//     frame is gone.
//
// Everything below allocates in C memory tied to the Future, so the lifetime
// matches what the native side actually needs, and Future.Release() reclaims
// it in one place.

// parseToC converts an AudioDescription using Future-owned C memory.
func (ctx *AudioDescription) parseToC(f *Future) C.ntg_audio_description_struct {
	var x C.ntg_audio_description_struct
	x.mediaSource = ctx.MediaSource.ParseToC()
	x.input = f.cString(ctx.Input)
	x.sampleRate = C.uint32_t(ctx.SampleRate)
	x.channelCount = C.uint8_t(ctx.ChannelCount)
	x.keepOpen = C.bool(ctx.KeepOpen)
	return x
}

// parseToC converts a VideoDescription using Future-owned C memory.
func (ctx *VideoDescription) parseToC(f *Future) C.ntg_video_description_struct {
	var x C.ntg_video_description_struct
	x.mediaSource = ctx.MediaSource.ParseToC()
	x.input = f.cString(ctx.Input)
	x.width = C.int16_t(ctx.Width)
	x.height = C.int16_t(ctx.Height)
	x.fps = C.uint8_t(ctx.Fps)
	x.keepOpen = C.bool(ctx.KeepOpen)
	return x
}

// parseToC converts a MediaDescription, placing each sub-struct in C memory
// rather than pointing at Go locals.
func (ctx *MediaDescription) parseToC(f *Future) C.ntg_media_description_struct {
	var x C.ntg_media_description_struct

	if ctx.Microphone != nil {
		p := (*C.ntg_audio_description_struct)(f.allocC(C.size_t(unsafe.Sizeof(C.ntg_audio_description_struct{}))))
		*p = ctx.Microphone.parseToC(f)
		x.microphone = p
	}
	if ctx.Speaker != nil {
		p := (*C.ntg_audio_description_struct)(f.allocC(C.size_t(unsafe.Sizeof(C.ntg_audio_description_struct{}))))
		*p = ctx.Speaker.parseToC(f)
		x.speaker = p
	}
	if ctx.Camera != nil {
		p := (*C.ntg_video_description_struct)(f.allocC(C.size_t(unsafe.Sizeof(C.ntg_video_description_struct{}))))
		*p = ctx.Camera.parseToC(f)
		x.camera = p
	}
	if ctx.Screen != nil {
		p := (*C.ntg_video_description_struct)(f.allocC(C.size_t(unsafe.Sizeof(C.ntg_video_description_struct{}))))
		*p = ctx.Screen.parseToC(f)
		x.screen = p
	}

	return x
}

// parseUint32VectorF is parseUint32VectorC with Future-owned memory.
func parseUint32VectorF(f *Future, data []uint32) (*C.uint32_t, C.int) {
	if len(data) == 0 {
		return nil, 0
	}
	elem := unsafe.Sizeof(C.uint32_t(0))
	p := f.allocC(C.size_t(uintptr(len(data)) * elem))
	if p == nil {
		return nil, 0
	}
	base := uintptr(p)
	for i, v := range data {
		*(*C.uint32_t)(unsafe.Pointer(base + uintptr(i)*elem)) = C.uint32_t(v)
	}
	return (*C.uint32_t)(p), C.int(len(data))
}

// parseSsrcGroupsF is parseSsrcGroups with Future-owned memory: the group
// array itself lives in C memory instead of a Go slice, and the semantics
// strings are freed with the rest of the call.
func parseSsrcGroupsF(f *Future, ssrcGroups []SsrcGroup) *C.ntg_ssrc_group_struct {
	if len(ssrcGroups) == 0 {
		return nil
	}

	elem := unsafe.Sizeof(C.ntg_ssrc_group_struct{})
	p := f.allocC(C.size_t(uintptr(len(ssrcGroups)) * elem))
	if p == nil {
		return nil
	}

	base := uintptr(p)
	for i, group := range ssrcGroups {
		ssrcsC, sizeSsrcs := parseUint32VectorF(f, group.Ssrcs)
		*(*C.ntg_ssrc_group_struct)(unsafe.Pointer(base + uintptr(i)*elem)) = C.ntg_ssrc_group_struct{
			semantics: f.cString(group.Semantics),
			ssrcs:     ssrcsC,
			sizeSsrcs: sizeSsrcs,
		}
	}

	return (*C.ntg_ssrc_group_struct)(p)
}
