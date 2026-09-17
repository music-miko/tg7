package ntgcalls

//#include "ntgcalls.h"
//#include <stdlib.h>
import "C"
import (
	"fmt"
	"unsafe"
)

func parseConnectionState(state C.ntg_connection_state_enum) ConnectionState {
	switch state {
	case C.NTG_STATE_CONNECTING:
		return Connecting
	case C.NTG_STATE_CONNECTED:
		return Connected
	case C.NTG_STATE_FAILED:
		return Failed
	case C.NTG_STATE_TIMEOUT:
		return Timeout
	case C.NTG_STATE_CLOSED:
		return Closed
	}
	return Connecting
}

func parseStreamDevice(device C.ntg_stream_device_enum) StreamDevice {
	var goDevice StreamDevice
	switch device {
	case C.NTG_STREAM_MICROPHONE:
		goDevice = MicrophoneStream
	case C.NTG_STREAM_SPEAKER:
		goDevice = SpeakerStream
	case C.NTG_STREAM_CAMERA:
		goDevice = CameraStream
	case C.NTG_STREAM_SCREEN:
		goDevice = ScreenStream
	}
	return goDevice
}

func parseStringVector(data unsafe.Pointer, size C.int) []string {
	result := make([]string, size)
	for i := 0; i < int(size); i++ {
		pointer := *(**C.char)(unsafe.Pointer(uintptr(data) + uintptr(i)*unsafe.Sizeof(uintptr(0))))
		result[i] = C.GoString(pointer)
		C.free(unsafe.Pointer(pointer))
	}
	defer C.free(data)
	return result
}

func parseErrorCode(futureResult *Future) error {
	errorCode := int32(*futureResult.errCode)
	if errorCode < 0 {
		var message string
		if *futureResult.errMessage != nil {
			message = C.GoString(*futureResult.errMessage)
		}
		if len(message) == 0 {
			message = fmt.Sprintf("Error code: %d", errorCode)
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func parseStreamStatus(status C.ntg_stream_status_enum) StreamStatus {
	switch status {
	case C.NTG_ACTIVE:
		return ActiveStream
	case C.NTG_PAUSED:
		return PausedStream
	case C.NTG_IDLING:
		return IdlingStream
	}
	return ActiveStream
}

func parseDeviceInfoVector(devices unsafe.Pointer, size C.int) []DeviceInfo {
	rawDevices := make([]DeviceInfo, size)
	for i := 0; i < int(size); i++ {
		device := *(*C.ntg_device_info_struct)(unsafe.Pointer(uintptr(devices) + uintptr(i)*unsafe.Sizeof(C.ntg_device_info_struct{})))
		rawDevices[i] = DeviceInfo{
			Name:     C.GoString(device.name),
			Metadata: C.GoString(device.metadata),
		}
		C.free(unsafe.Pointer(device.name))
		C.free(unsafe.Pointer(device.metadata))
	}
	defer C.free(devices)
	return rawDevices
}

// NOTE: parseBytes, parseBool, parseSsrcGroups, parseUint32VectorC and
// parseStringVectorC used to live here. They have been replaced by the
// Future-owned equivalents in cmem.go.
//
// The originals allocated C memory (C.CString / C.CBytes / C.malloc) that
// nothing ever freed, and returned pointers into Go slice backing arrays
// (&rawGroups[0]) to be read by asynchronous native calls. Both are unsafe
// once the call outlives the Go frame that started it, which is exactly the
// case for every ntg_* call in this package.
