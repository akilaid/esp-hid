package capture

import "errors"

// ErrPermissionDenied reports that the OS refused to install the input hook
// because the process lacks the required input-capture permission. On macOS
// that is Accessibility and/or Input Monitoring; Windows has no equivalent,
// so only the darwin implementation returns it.
//
// It is declared here, untagged, so the bridge and UI layers can test for it
// with errors.Is on any platform without a build tag of their own.
var ErrPermissionDenied = errors.New("capture: input permission not granted")
