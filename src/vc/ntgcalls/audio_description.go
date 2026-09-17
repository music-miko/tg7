package ntgcalls

type AudioDescription struct {
	MediaSource  MediaSource
	Input        string
	SampleRate   uint32
	ChannelCount uint8
	KeepOpen     bool
}

// NOTE: the old exported ParseToC() on this type was removed. It called
// C.CString(ctx.Input) and nothing ever freed the result, so every
// SetStreamSources leaked the whole ffmpeg command line. Use parseToC(f) in
// cmem.go instead, which ties the allocation to the Future and frees it when
// the native call resolves.
