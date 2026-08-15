// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

const (
	defaultMemoryTrackerSampleInterval = 1
)

// MemoryTrackerOption configures a MemoryTracker instance.
type MemoryTrackerOption func(*MemoryTracker)

// MemoryTrackerLimit sets a limit on the peak aggregate memory observed during tracking.
//
// The tracker does not enforce the limit itself; callers should consult ExceedsLimit after
// tracking observations and terminate evaluation as appropriate.
func MemoryTrackerLimit(limit uint32) MemoryTrackerOption {
	return func(t *MemoryTracker) {
		t.limit = &limit
	}
}

// MemoryTrackerSampleInterval configures how frequently Sample observations compute a size.
//
// An interval of N means every Nth call to Sample performs a size computation; intervening
// calls are skipped. Values less than 1 are treated as 1, meaning every sample is computed.
// Sampling bounds the tracking overhead for high-frequency observation points such as
// comprehension loops and bind initializers.
func MemoryTrackerSampleInterval(interval uint32) MemoryTrackerOption {
	return func(t *MemoryTracker) {
		if interval < 1 {
			interval = 1
		}
		t.sampleInterval = interval
	}
}

// MemoryTrackerSizeCalculator overrides the SizeCalculator used to compute value sizes.
func MemoryTrackerSizeCalculator(calc *SizeCalculator) MemoryTrackerOption {
	return func(t *MemoryTracker) {
		t.calc = calc
	}
}

// MemoryTracker records the peak aggregate memory observed during an evaluation.
//
// Memory is measured in aggregate element counts as computed by a SizeCalculator, with all
// arithmetic saturating at math.MaxUint32. The tracker is independent of any interpreter
// implementation; evaluators feed it observations at the points where values materialize
// during evaluation, such as resolved attributes, call results, constructed aggregates, and
// values built up within comprehensions or bind initializers.
//
// The peak is the largest single observation, where one Track call observes a set of
// coexistent values as a single watermark.
//
// A MemoryTracker is stateful and intended for use by a single evaluation at a time; it is
// not safe for concurrent use.
type MemoryTracker struct {
	version        int
	calc           *SizeCalculator
	limit          *uint32
	sampleInterval uint32

	sampleCount       uint32
	peak              uint32
	calcLimitExceeded bool
}

// NewMemoryTracker returns a new MemoryTracker configured with optional MemoryTrackerOption
// settings, using a default SizeCalculator when one is not provided.
func NewMemoryTracker(opts ...MemoryTrackerOption) *MemoryTracker {
	t := &MemoryTracker{
		version:        0,
		sampleInterval: defaultMemoryTrackerSampleInterval,
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.calc == nil {
		t.calc = NewSizeCalculator()
	}
	return t
}

// Version returns the tracking version.
func (t *MemoryTracker) Version() int {
	return t.version
}

// Track observes a set of coexistent values as a single watermark, returning their combined
// aggregate size with saturation at math.MaxUint32.
//
// Call sites with multiple live values, such as the input arguments to a function call,
// should be tracked in a single call so the watermark reflects their combined footprint.
func (t *MemoryTracker) Track(vals ...any) uint32 {
	total := uint32(0)
	for _, val := range vals {
		est := t.calc.EstimateAggregateSize(val)
		if est.LimitExceeded {
			t.calcLimitExceeded = true
		}
		total = safeAddUint32(total, est.Size)
	}
	if total > t.peak {
		t.peak = total
	}
	return total
}

// Sample observes a set of coexistent values subject to the tracker's sample interval,
// returning their combined aggregate size when computed, or zero when the observation
// is skipped.
//
// Sample is intended for high-frequency observation points, such as accumulator values built
// up by comprehension loops or bind initializers, where sizing every iteration would be
// prohibitively expensive.
func (t *MemoryTracker) Sample(vals ...any) uint32 {
	t.sampleCount++
	if t.sampleInterval > 1 && t.sampleCount%t.sampleInterval != 0 {
		return 0
	}
	return t.Track(vals...)
}

// Peak returns the largest single watermark observed, saturating at math.MaxUint32.
func (t *MemoryTracker) Peak() uint32 {
	return t.peak
}

// ExceedsLimit indicates whether the peak observed memory exceeds the configured limit.
// When no limit is configured, ExceedsLimit always returns false.
func (t *MemoryTracker) ExceedsLimit() bool {
	return t.limit != nil && t.peak > *t.limit
}

// CalculationLimitExceeded indicates whether any tracked value was too expensive to size,
// causing the size computation to abort at the SizeCalculator's depth or traversal limits.
//
// Such observations saturate to math.MaxUint32; this signal distinguishes values which were
// too costly to measure from values whose measured size genuinely saturated uint32.
func (t *MemoryTracker) CalculationLimitExceeded() bool {
	return t.calcLimitExceeded
}
