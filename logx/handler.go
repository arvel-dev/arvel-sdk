// SPDX-License-Identifier: Apache-2.0

package logx

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/arvel-dev/arvel-sdk/redact"
)

// replaceAttr renames the built-in keys to the names PROTOCOL §10 uses.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		return slog.String("ts", a.Value.Time().UTC().Format(time.RFC3339Nano))
	case slog.LevelKey:
		if l, ok := a.Value.Any().(slog.Level); ok {
			return slog.String("level", strings.ToLower(l.String()))
		}
	}
	return a
}

// redactHandler runs the message and every attribute through the redactor.
type redactHandler struct {
	inner slog.Handler
	r     *redact.Redactor
}

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	nr := slog.NewRecord(rec.Time, rec.Level, h.r.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(h.attr(a))
		return true
	})
	return h.inner.Handle(ctx, nr)
}

func (h *redactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(as))
	for i, a := range as {
		red[i] = h.attr(a)
	}
	return &redactHandler{inner: h.inner.WithAttrs(red), r: h.r}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name), r: h.r}
}

func (h *redactHandler) attr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.r.String(v.String()))
	case slog.KindGroup:
		members := v.Group()
		out := make([]slog.Attr, len(members))
		for i, m := range members {
			out[i] = h.attr(m)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		if v.Any() == nil {
			return slog.Attr{Key: a.Key, Value: v}
		}
		// %+v uses Error() and String() and recovers from panics in them.
		return slog.String(a.Key, h.r.String(fmt.Sprintf("%+v", v.Any())))
	}
	return slog.Attr{Key: a.Key, Value: v}
}
