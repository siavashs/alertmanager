// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inhibit

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/alertmanager/config"
	amcommoncfg "github.com/prometheus/alertmanager/config/common"
	"github.com/prometheus/alertmanager/eventrecorder"
	"github.com/prometheus/alertmanager/pkg/labels"
	"github.com/prometheus/alertmanager/provider/mem"
	"github.com/prometheus/alertmanager/types"
)

// BenchmarkMutes benchmarks the Mutes method for the Muter interface
// for different numbers of inhibition rules.
func BenchmarkMutes(b *testing.B) {
	b.Run("1 inhibition rule, 1 inhibiting alert", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 1, 1))
	})
	b.Run("10 inhibition rules, 1 inhibiting alert", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 10, 1))
	})
	b.Run("100 inhibition rules, 1 inhibiting alert", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 100, 1))
	})
	b.Run("1000 inhibition rules, 1 inhibiting alert", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 1000, 1))
	})
	b.Run("10000 inhibition rules, 1 inhibiting alert", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 10000, 1))
	})
	b.Run("1 inhibition rule, 10 inhibiting alerts", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 1, 10))
	})
	b.Run("1 inhibition rule, 100 inhibiting alerts", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 1, 100))
	})
	b.Run("1 inhibition rule, 1000 inhibiting alerts", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 1, 1000))
	})
	b.Run("1 inhibition rule, 10000 inhibiting alerts", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 1, 10000))
	})
	b.Run("100 inhibition rules, 1000 inhibiting alerts", func(b *testing.B) {
		benchmarkMutes(b, allRulesMatchBenchmark(b, 100, 1000))
	})
	b.Run("10 inhibition rules, last rule matches", func(b *testing.B) {
		benchmarkMutes(b, lastRuleMatchesBenchmark(b, 10))
	})
	b.Run("100 inhibition rules, last rule matches", func(b *testing.B) {
		benchmarkMutes(b, lastRuleMatchesBenchmark(b, 100))
	})
	b.Run("1000 inhibition rules, last rule matches", func(b *testing.B) {
		benchmarkMutes(b, lastRuleMatchesBenchmark(b, 1000))
	})
	b.Run("10000 inhibition rules, last rule matches", func(b *testing.B) {
		benchmarkMutes(b, lastRuleMatchesBenchmark(b, 10000))
	})
}

// benchmarkOptions allows the declaration of a wide range of benchmarks.
type benchmarkOptions struct {
	// n is the total number of inhibition rules.
	n int
	// newRuleFunc creates the next inhibition rule. It is called n times.
	newRuleFunc func(idx int) amcommoncfg.InhibitRule
	// newAlertsFunc creates the inhibiting alerts for each inhibition rule.
	// It is called n times.
	newAlertsFunc func(idx int, r amcommoncfg.InhibitRule) []types.Alert
	// benchFunc runs the benchmark.
	benchFunc func(mutesFunc func(context.Context, model.LabelSet) bool) error
}

// allRulesMatchBenchmark returns a new benchmark where all inhibition rules
// inhibit the label dst=0. It supports a number of variations, including
// customization of the number of inhibition rules, and the number of
// inhibiting alerts per inhibition rule.
//
// The source matchers are suffixed with the position of the inhibition rule
// in the list (e.g. src=1, src=2, etc...). The target matchers are the same
// across all inhibition rules (dst=0).
//
// Each inhibition rule can have zero or more alerts that match the source
// matchers, and is determined with numInhibitingAlerts.
//
// It expects dst=0 to be muted and will fail if not.
func allRulesMatchBenchmark(b *testing.B, numInhibitionRules, numInhibitingAlerts int) benchmarkOptions {
	return benchmarkOptions{
		n: numInhibitionRules,
		newRuleFunc: func(idx int) amcommoncfg.InhibitRule {
			return amcommoncfg.InhibitRule{
				SourceMatchers: amcommoncfg.Matchers{
					mustNewMatcher(b, labels.MatchEqual, "src", strconv.Itoa(idx)),
				},
				TargetMatchers: amcommoncfg.Matchers{
					mustNewMatcher(b, labels.MatchEqual, "dst", "0"),
				},
			}
		},
		newAlertsFunc: func(idx int, _ amcommoncfg.InhibitRule) []types.Alert {
			var alerts []types.Alert
			for i := range numInhibitingAlerts {
				alerts = append(alerts, types.Alert{
					Alert: model.Alert{
						Labels: model.LabelSet{
							"src": model.LabelValue(strconv.Itoa(idx)),
							"idx": model.LabelValue(strconv.Itoa(i)),
						},
					},
				})
			}
			return alerts
		}, benchFunc: func(mutesFunc func(context.Context, model.LabelSet) bool) error {
			if ok := mutesFunc(context.Background(), model.LabelSet{"dst": "0"}); !ok {
				return errors.New("expected dst=0 to be muted")
			}
			return nil
		},
	}
}

// lastRuleMatchesBenchmark returns a new benchmark where the last inhibition
// rule inhibits the label dst=0. All other inhibition rules are no-ops.
//
// The source matchers are suffixed with the position of the inhibition rule
// in the list (e.g. src=1, src=2, etc...). The target matchers are the same
// across all inhibition rules (dst=0).
//
// It expects dst=0 to be muted and will fail if not.
func lastRuleMatchesBenchmark(b *testing.B, n int) benchmarkOptions {
	return benchmarkOptions{
		n: n,
		newRuleFunc: func(idx int) amcommoncfg.InhibitRule {
			return amcommoncfg.InhibitRule{
				SourceMatchers: amcommoncfg.Matchers{
					mustNewMatcher(b, labels.MatchEqual, "src", strconv.Itoa(idx)),
				},
				TargetMatchers: amcommoncfg.Matchers{
					mustNewMatcher(b, labels.MatchEqual, "dst", "0"),
				},
			}
		},
		newAlertsFunc: func(idx int, _ amcommoncfg.InhibitRule) []types.Alert {
			// Do not create an alert unless it is the last inhibition rule.
			if idx < n-1 {
				return nil
			}
			return []types.Alert{{
				Alert: model.Alert{
					Labels: model.LabelSet{
						"src": model.LabelValue(strconv.Itoa(idx)),
					},
				},
			}}
		}, benchFunc: func(mutesFunc func(context.Context, model.LabelSet) bool) error {
			if ok := mutesFunc(context.Background(), model.LabelSet{"dst": "0"}); !ok {
				return errors.New("expected dst=0 to be muted")
			}
			return nil
		},
	}
}

func benchmarkMutes(b *testing.B, opts benchmarkOptions) {
	r := prometheus.NewRegistry()
	m := types.NewMarker(r)
	s, err := mem.NewAlerts(context.TODO(), m, time.Minute, 0, nil, promslog.NewNopLogger(), eventrecorder.NopRecorder(), r, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	alerts, rules := benchmarkFromOptions(opts)
	for _, a := range alerts {
		tmp := a
		if err = s.Put(context.Background(), &tmp); err != nil {
			b.Fatal(err)
		}
	}

	ih := NewInhibitor(s, rules, m, promslog.NewNopLogger(), eventrecorder.NopRecorder())
	defer ih.Stop()
	go ih.Run()

	// Wait some time for the inhibitor to seed its cache.
	<-time.After(time.Second)

	for b.Loop() {
		require.NoError(b, opts.benchFunc(ih.Mutes))
	}
}

func benchmarkFromOptions(opts benchmarkOptions) ([]types.Alert, []amcommoncfg.InhibitRule) {
	var (
		alerts = make([]types.Alert, 0, opts.n)
		rules  = make([]amcommoncfg.InhibitRule, 0, opts.n)
	)
	for i := 0; i < opts.n; i++ {
		r := opts.newRuleFunc(i)
		alerts = append(alerts, opts.newAlertsFunc(i, r)...)
		rules = append(rules, r)
	}
	return alerts, rules
}

func mustNewMatcher(b *testing.B, op labels.MatchType, name, value string) *labels.Matcher {
	m, err := labels.NewMatcher(op, name, value)
	require.NoError(b, err)
	return m
}

// BenchmarkMutesEventRecorderOverhead measures the per-call overhead that the
// event recorder adds to Inhibitor.Mutes.  Three recorder configurations are
// compared for each scenario:
//
//   - nop_recorder:                          NopRecorder (baseline, event proto is
//     still constructed as a function argument).
//   - active_recorder/recording_disabled:    Real recorder, but the context does
//     not carry the recording flag so RecordEvent returns after the context check.
//   - active_recorder/recording_enabled:     Full path including extractEventType,
//     protojson.Marshal, and channel send.
func BenchmarkMutesEventRecorderOverhead(b *testing.B) {
	cases := []struct {
		name    string
		nRules  int
		nAlerts int
	}{
		{"10_rules_1_alert", 10, 1},
		{"100_rules_1000_alerts", 100, 1000},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.Run("nop_recorder", func(b *testing.B) {
				benchmarkMutesRecorderOverhead(b, tc.nRules, tc.nAlerts, eventrecorder.NopRecorder(), false)
			})
			b.Run("active_recorder/recording_disabled", func(b *testing.B) {
				rec := newInhibitBenchRecorder(b)
				defer rec.Close()
				benchmarkMutesRecorderOverhead(b, tc.nRules, tc.nAlerts, rec, false)
			})
			b.Run("active_recorder/recording_enabled", func(b *testing.B) {
				rec := newInhibitBenchRecorder(b)
				defer rec.Close()
				benchmarkMutesRecorderOverhead(b, tc.nRules, tc.nAlerts, rec, true)
			})
		})
	}
}

// newInhibitBenchRecorder creates an active Recorder backed by a JSONL file in
// a temporary directory.  The background write-loop drains the event queue so
// the channel never fills up during the benchmark.
func newInhibitBenchRecorder(b *testing.B) eventrecorder.Recorder {
	b.Helper()
	cfg := config.EventRecorderConfig{
		Outputs: []config.EventRecorderOutput{{
			Type: "file",
			Path: filepath.Join(b.TempDir(), "events.jsonl"),
		}},
	}
	return eventrecorder.NewRecorderFromConfig(cfg, "bench-inhibit", promslog.NewNopLogger(), prometheus.NewRegistry())
}

func benchmarkMutesRecorderOverhead(b *testing.B, numRules, numAlertsPerRule int, recorder eventrecorder.Recorder, recordingEnabled bool) {
	b.Helper()
	b.ReportAllocs()

	r := prometheus.NewRegistry()
	m := types.NewMarker(r)
	// The mem alert store always uses NopRecorder — we only measure recorder
	// overhead inside the Inhibitor.Mutes path.
	s, err := mem.NewAlerts(context.TODO(), m, time.Minute, 0, nil, promslog.NewNopLogger(), eventrecorder.NopRecorder(), r, nil)
	require.NoError(b, err)
	defer s.Close()

	opts := allRulesMatchBenchmark(b, numRules, numAlertsPerRule)
	alerts, rules := benchmarkFromOptions(opts)
	for _, a := range alerts {
		tmp := a
		require.NoError(b, s.Put(context.Background(), &tmp))
	}

	ih := NewInhibitor(s, rules, m, promslog.NewNopLogger(), recorder)
	defer ih.Stop()
	go ih.Run()

	// Wait for the inhibitor to seed its cache.
	<-time.After(time.Second)

	ctx := context.Background()
	if recordingEnabled {
		ctx = eventrecorder.WithEventRecording(ctx)
	}

	b.ResetTimer()
	for b.Loop() {
		if ok := ih.Mutes(ctx, model.LabelSet{"dst": "0"}); !ok {
			b.Fatal("expected dst=0 to be muted")
		}
	}
}
