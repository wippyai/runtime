// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
)

const MaxResultValueBytes = 32 * 1024
const MaxResultErrorBytes = 4096
const MaxResultWireBytes = 72 * 1024

var ErrInvalidResult = errors.New("remote topology: invalid completion result")

// CompletionResult separates observed execution failure from unavailable result
// representation. Unavailable values never become a fabricated empty success.
// Data is copied at each boundary. Go error identity is not transferable.
type CompletionResult struct {
	ValueState string `json:"value_state"`
	Format     string `json:"format"`
	Reason     string `json:"reason"`
	ErrorText  string `json:"error_text"`
	ErrorState string `json:"error_state"`
	Data       []byte `json:"data"`
}

// CaptureResult accepts already-owned local completion. It deliberately avoids
// arbitrary marshaler/transcoder calls. Call outside admission gates: obtaining
// an error message or payload invokes methods supplied by the local task.
// Unsupported native values retain an explicit unavailable marker.
func CaptureResult(result *runtime.Result) (CompletionResult, error) {
	if result == nil {
		return CompletionResult{}, ErrInvalidResult
	}
	out := CompletionResult{ValueState: "absent", ErrorState: "none"}
	if result.Error != nil {
		out.ErrorState = "text"
		out.ErrorText = result.Error.Error()
		if len(out.ErrorText) > MaxResultErrorBytes || !utf8.ValidString(out.ErrorText) {
			out.ErrorState = "unavailable"
			out.ErrorText = ""
		}
	}
	if result.Value == nil {
		return out, nil
	}
	out.ValueState = "unavailable"
	out.Reason = "unsupported_format"
	format := result.Value.Format()
	switch format {
	case payload.String, payload.Bytes, payload.JSON:
	default:
		return out, nil
	}
	var data []byte
	switch v := result.Value.Data().(type) {
	case string:
		if format != payload.String && format != payload.JSON {
			out.Reason = "invalid_value"
			return out, nil
		}
		if len(v) > MaxResultValueBytes {
			out.Reason = "value_too_large"
			return out, nil
		}
		data = []byte(v)
	case []byte:
		if format == payload.String {
			out.Reason = "invalid_value"
			return out, nil
		}
		if len(v) > MaxResultValueBytes {
			out.Reason = "value_too_large"
			return out, nil
		}
		data = append([]byte{}, v...)
	default:
		out.Reason = "invalid_value"
		return out, nil
	}
	if (format == payload.String && !utf8.Valid(data)) || (format == payload.JSON && (!utf8.Valid(data) || !json.Valid(data))) {
		out.Reason = "invalid_value"
		return out, nil
	}
	out.ValueState, out.Format, out.Reason, out.Data = "present", format, "", data
	return out, nil
}

func (r CompletionResult) valid() bool {
	if len(r.Data) > MaxResultValueBytes || len(r.ErrorText) > MaxResultErrorBytes || !utf8.ValidString(r.ErrorText) {
		return false
	}
	switch r.ErrorState {
	case "text":
	case "none", "unavailable":
		if r.ErrorText != "" {
			return false
		}
	default:
		return false
	}
	switch r.ValueState {
	case "absent":
		return r.Data == nil && r.Format == "" && r.Reason == ""
	case "unavailable":
		if r.Data != nil || r.Format != "" {
			return false
		}
		return r.Reason == "unsupported_format" || r.Reason == "invalid_value" || r.Reason == "value_too_large"
	case "present":
		if r.Reason != "" || r.Data == nil {
			return false
		}
		switch r.Format {
		case payload.Bytes:
			return true
		case payload.String:
			return utf8.Valid(r.Data)
		case payload.JSON:
			return utf8.Valid(r.Data) && json.Valid(r.Data)
		}
	}
	return false
}

func EncodeResult(result CompletionResult) ([]byte, error) {
	if !result.valid() {
		return nil, ErrInvalidResult
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > MaxResultWireBytes {
		return nil, ErrInvalidResult
	}
	return data, nil
}

// DecodeResult rejects missing, unknown, duplicate and trailing fields. It only
// decodes representation; the enclosing EXIT must verify grant, request, actors
// and exact live connection before any watcher can receive this result.
func DecodeResult(data []byte) (CompletionResult, error) {
	var out CompletionResult
	if len(data) == 0 || len(data) > MaxResultWireBytes || !utf8.Valid(data) {
		return out, ErrInvalidResult
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return out, ErrInvalidResult
	}
	seen := make(map[string]bool, 6)
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return CompletionResult{}, ErrInvalidResult
		}
		seen[key] = true
		// Decode scalars through pointers so JSON null cannot stand in for text.
		var value *string
		switch key {
		case "data":
			err = d.Decode(&value)
			if err == nil && value != nil {
				out.Data, err = base64.StdEncoding.Strict().DecodeString(*value)
				if err == nil && base64.StdEncoding.EncodeToString(out.Data) != *value {
					return CompletionResult{}, ErrInvalidResult
				}
			}
		case "value_state", "format", "reason", "error_text", "error_state":
			err = d.Decode(&value)
			if err == nil && value == nil {
				return CompletionResult{}, ErrInvalidResult
			}
			if err == nil {
				switch key {
				case "value_state":
					out.ValueState = *value
				case "format":
					out.Format = *value
				case "reason":
					out.Reason = *value
				case "error_text":
					out.ErrorText = *value
				case "error_state":
					out.ErrorState = *value
				}
			}
		default:
			return CompletionResult{}, ErrInvalidResult
		}
		if err != nil {
			return CompletionResult{}, ErrInvalidResult
		}
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') || len(seen) != 6 || !out.valid() {
		return CompletionResult{}, ErrInvalidResult
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return CompletionResult{}, ErrInvalidResult
	}
	return out, nil
}
