// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"errors"
	"fmt"
	"time"
)

// Usage is the result of _arvel/usage and the body of _arvel/usage_updated.
type Usage struct {
	Source    UsageSource   `json:"source"`
	Status    UsageStatus   `json:"status"`
	Windows   []UsageWindow `json:"windows,omitempty"`
	Spend     *Spend        `json:"spend,omitempty"`
	UpdatedAt time.Time     `json:"updatedAt,omitzero"`
}

// UsageWindow is one quota window.
type UsageWindow struct {
	ID              string    `json:"id"`
	Label           string    `json:"label,omitempty"`
	UsedPercent     float64   `json:"usedPercent"`
	ResetsAt        time.Time `json:"resetsAt,omitzero"`
	DurationMinutes int       `json:"durationMinutes,omitempty"`
}

// Spend is money spent since the process started.
type Spend struct {
	Currency   string      `json:"currency"`
	SinceStart float64     `json:"sinceStart"`
	Source     UsageSource `json:"source,omitempty"`
}

// TurnUsage is "_meta.arvel.usage" of a session/prompt result (PROTOCOL §7.2).
type TurnUsage struct {
	InputTokens     int64       `json:"inputTokens"`
	OutputTokens    int64       `json:"outputTokens"`
	CacheReadTokens int64       `json:"cacheReadTokens,omitempty"`
	CostUSD         float64     `json:"costUsd,omitempty"`
	CostSource      UsageSource `json:"costSource,omitempty"`
}

// Validate checks enums, percentages and spend.
func (u Usage) Validate() error {
	var errs []error
	if !u.Source.Valid() {
		errs = append(errs, fmt.Errorf("protocol: usage.source %q is not valid", u.Source))
	}
	if !u.Status.Valid() {
		errs = append(errs, fmt.Errorf("protocol: usage.status %q is not valid", u.Status))
	}
	for i, w := range u.Windows {
		if w.ID == "" {
			errs = append(errs, fmt.Errorf("protocol: usage.windows[%d].id is empty", i))
		}
		if !(w.UsedPercent >= 0 && w.UsedPercent <= 100) {
			errs = append(errs, fmt.Errorf("protocol: usage.windows[%d].usedPercent is outside [0,100]", i))
		}
	}
	if u.Spend != nil {
		if u.Spend.Currency == "" {
			errs = append(errs, errors.New("protocol: usage.spend.currency is empty"))
		}
		if !(u.Spend.SinceStart >= 0) {
			errs = append(errs, errors.New("protocol: usage.spend.sinceStart is negative"))
		}
	}
	return errors.Join(errs...)
}
