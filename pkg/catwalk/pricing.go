package catwalk

import (
	"fmt"
	"strings"
	"time"
)

// Weekday is a day of the week used in time-based pricing conditions.
type Weekday string

// All the days of the week.
const (
	Monday    Weekday = "mon"
	Tuesday   Weekday = "tue"
	Wednesday Weekday = "wed"
	Thursday  Weekday = "thu"
	Friday    Weekday = "fri"
	Saturday  Weekday = "sat"
	Sunday    Weekday = "sun"
)

// PricingValues stores optional prices in US dollars per 1M tokens. Only
// the fields that are set replace the corresponding base pricing fields
// when a pricing override matches.
type PricingValues struct {
	Input       *float64 `json:"input,omitempty"`
	Output      *float64 `json:"output,omitempty"`
	CacheCreate *float64 `json:"cache_create,omitempty"`
	CacheHit    *float64 `json:"cache_hit,omitempty"`
}

// TokenRange is a half-open range of token counts, [gte, lt). A missing
// bound is unbounded.
type TokenRange struct {
	GTE *int64 `json:"gte,omitempty"`
	LT  *int64 `json:"lt,omitempty"`
}

// Contains reports whether n falls within the range.
func (r TokenRange) Contains(n int64) bool {
	if r.GTE != nil && n < *r.GTE {
		return false
	}
	if r.LT != nil && n >= *r.LT {
		return false
	}
	return true
}

// TimeWindow describes a weekly recurring time window. Days is empty for
// every day. Start and End use 24-hour "HH:MM" notation; when Start is
// later than End the window wraps around midnight. Timezone is an IANA
// time zone name and defaults to UTC when empty.
type TimeWindow struct {
	Days     []Weekday `json:"days,omitempty"`
	Start    string    `json:"start"`
	End      string    `json:"end"`
	Timezone string    `json:"timezone,omitempty"`
}

// Contains reports whether t falls within the window. It returns false
// when t is the zero time or the window is malformed.
func (w TimeWindow) Contains(t time.Time) bool {
	if t.IsZero() {
		return false
	}
	loc := time.UTC
	if w.Timezone != "" {
		loaded, err := time.LoadLocation(w.Timezone)
		if err != nil {
			return false
		}
		loc = loaded
	}
	local := t.In(loc)
	if len(w.Days) > 0 && !containsDay(w.Days, local.Weekday()) {
		return false
	}
	start, ok := parseClock(w.Start)
	if !ok {
		return false
	}
	end, ok := parseClock(w.End)
	if !ok {
		return false
	}
	minutes := local.Hour()*60 + local.Minute()
	if start <= end {
		return minutes >= start && minutes < end
	}
	return minutes >= start || minutes < end
}

// PricingCondition describes when a pricing override applies. All the
// populated fields must match (AND semantics). A zero time in the pricing
// context never matches a time window condition.
type PricingCondition struct {
	// InputTokens matches on the number of input tokens of a request,
	// e.g. for providers that charge more once the context grows past a
	// certain size.
	InputTokens *TokenRange `json:"input_tokens,omitempty"`
	// Time matches requests made inside a recurring time window, e.g. for
	// providers with peak vs. off-peak pricing.
	Time *TimeWindow `json:"time,omitempty"`
}

// Matches reports whether the condition applies to the given context.
func (c PricingCondition) Matches(ctx PricingContext) bool {
	if c.InputTokens != nil && !c.InputTokens.Contains(ctx.InputTokens) {
		return false
	}
	if c.Time != nil && !c.Time.Contains(ctx.Time) {
		return false
	}
	return true
}

// PricingOverride replaces parts of the base pricing of a model when its
// condition matches.
type PricingOverride struct {
	Condition PricingCondition `json:"condition"`
	Pricing   PricingValues    `json:"pricing"`
}

// PricingContext carries the request information used to resolve pricing
// overrides.
type PricingContext struct {
	// InputTokens is the number of input tokens of the request.
	InputTokens int64
	// Time is when the request is made. A zero time never matches
	// time-based overrides.
	Time time.Time
}

// ResolvePricing returns the effective pricing of the model for the given
// context. Overrides are applied in declaration order on top of the base
// pricing, so later matching overrides win over earlier ones.
func (m Model) ResolvePricing(ctx PricingContext) Pricing {
	resolved := m.Pricing
	for _, override := range m.PricingOverrides {
		if !override.Condition.Matches(ctx) {
			continue
		}
		if override.Pricing.Input != nil {
			resolved.Input = *override.Pricing.Input
		}
		if override.Pricing.Output != nil {
			resolved.Output = *override.Pricing.Output
		}
		if override.Pricing.CacheCreate != nil {
			resolved.CacheCreate = *override.Pricing.CacheCreate
		}
		if override.Pricing.CacheHit != nil {
			resolved.CacheHit = *override.Pricing.CacheHit
		}
	}
	return resolved
}

// Validate reports whether the override is well-formed.
func (o PricingOverride) Validate() error {
	if err := o.Condition.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]*float64{
		"input":        o.Pricing.Input,
		"output":       o.Pricing.Output,
		"cache_create": o.Pricing.CacheCreate,
		"cache_hit":    o.Pricing.CacheHit,
	} {
		if value != nil && *value < 0 {
			return fmt.Errorf("pricing override: %s must not be negative", name)
		}
	}
	return nil
}

// Validate reports whether the condition is well-formed.
func (c PricingCondition) Validate() error {
	if c.InputTokens != nil {
		r := c.InputTokens
		if r.GTE != nil && *r.GTE < 0 {
			return fmt.Errorf("pricing condition: input_tokens.gte must not be negative")
		}
		if r.LT != nil && *r.LT <= 0 {
			return fmt.Errorf("pricing condition: input_tokens.lt must be positive")
		}
		if r.GTE != nil && r.LT != nil && *r.GTE >= *r.LT {
			return fmt.Errorf("pricing condition: input_tokens.gte must be less than input_tokens.lt")
		}
	}
	if c.Time != nil {
		if err := c.Time.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate reports whether the time window is well-formed.
func (w TimeWindow) Validate() error {
	for _, day := range w.Days {
		if !validDay(day) {
			return fmt.Errorf("pricing condition: unknown day %q", day)
		}
	}
	if _, ok := parseClock(w.Start); !ok {
		return fmt.Errorf("pricing condition: invalid start time %q, want HH:MM", w.Start)
	}
	if _, ok := parseClock(w.End); !ok {
		return fmt.Errorf("pricing condition: invalid end time %q, want HH:MM", w.End)
	}
	if w.Timezone != "" {
		if _, err := time.LoadLocation(w.Timezone); err != nil {
			return fmt.Errorf("pricing condition: invalid timezone %q: %w", w.Timezone, err)
		}
	}
	return nil
}

func parseClock(s string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func containsDay(days []Weekday, target time.Weekday) bool {
	abbrev := weekdayAbbrev(target)
	for _, day := range days {
		if day == abbrev {
			return true
		}
	}
	return false
}

func validDay(day Weekday) bool {
	switch day {
	case Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday:
		return true
	default:
		return false
	}
}

func weekdayAbbrev(day time.Weekday) Weekday {
	switch day {
	case time.Monday:
		return Monday
	case time.Tuesday:
		return Tuesday
	case time.Wednesday:
		return Wednesday
	case time.Thursday:
		return Thursday
	case time.Friday:
		return Friday
	case time.Saturday:
		return Saturday
	case time.Sunday:
		return Sunday
	default:
		return ""
	}
}
