package ntgcalls

type MediaDescription struct {
	Microphone *AudioDescription
	Speaker    *AudioDescription
	Camera     *VideoDescription
	Screen     *VideoDescription
}

// NOTE: the old exported ParseToC() on this type was removed. Besides leaking
// the C strings underneath it, it pointed the C struct's fields at Go locals
// (x.microphone = &microphone), which are only guaranteed to stay put for the
// duration of a synchronous cgo call - and ntg_set_stream_sources is
// asynchronous. parseToC(f) in cmem.go places those sub-structs in C memory
// owned by the Future instead.
