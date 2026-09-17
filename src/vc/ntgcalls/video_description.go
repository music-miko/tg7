package ntgcalls

type VideoDescription struct {
	MediaSource   MediaSource
	Input         string
	Width, Height int16
	Fps           uint8
	KeepOpen      bool
}

// NOTE: the old exported ParseToC() on this type was removed for the same
// reason as AudioDescription's - see cmem.go.
