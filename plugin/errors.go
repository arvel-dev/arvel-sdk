// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/redact"
)

// toRPCError converts a provider or router error into the error sent to the
// host (SPEC §5.4 item 8). A *protocol.RPCError keeps its code and data, with
// secrets scrubbed; invalid Arvel error data becomes "internal"; any other error
// becomes "provider_error" with redacted text.
func toRPCError(rd *redact.Redactor, log *slog.Logger, err error) *protocol.RPCError {
	if err == nil {
		return nil
	}
	var re *protocol.RPCError
	if errors.As(err, &re) && re != nil {
		out := *re
		out.Message = rd.String(re.Message)
		if len(re.Data) > 0 {
			if data := rd.String(string(re.Data)); json.Valid([]byte(data)) {
				out.Data = json.RawMessage(data)
			} else {
				out.Data = nil
			}
		}
		if d, ok := out.Arvel(); ok {
			if verr := d.Validate(); verr != nil {
				log.Error("plugin returned an invalid error", "err", verr.Error())
				return protocol.NewError(protocol.ErrInternal, "plugin returned an invalid error", nil)
			}
		}
		return &out
	}
	return protocol.NewError(protocol.ErrProviderError, rd.String(err.Error()),
		map[string]any{"vendorCode": "unclassified"})
}
