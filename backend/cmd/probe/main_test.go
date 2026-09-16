package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want options
	}{
		{
			name: "defaults to one shot",
			args: nil,
			want: options{
				interval: defaultInterval,
			},
		},
		{
			name: "service duration and interval",
			args: []string{"-service=instagram", "-duration=30m", "-interval=15s"},
			want: options{
				serviceID: "instagram",
				duration:  30 * time.Minute,
				interval:  15 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.args, io.Discard)
			if err != nil {
				t.Fatalf("parseOptions() returned error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("parseOptions() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseOptionsRejectsInvalidValues(t *testing.T) {
	tests := [][]string{
		{"-duration=-1s"},
		{"-duration=30s", "-interval=0"},
		{"unexpected"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseOptions(args, io.Discard); err == nil {
				t.Fatal("parseOptions() returned nil error, want validation error")
			}
		})
	}
}

func TestSelectServices(t *testing.T) {
	catalogue := []service.Service{
		testService("github", true),
		testService("instagram", true),
		testService("disabled", false),
	}

	t.Run("all enabled by default", func(t *testing.T) {
		selected, err := selectServices(catalogue, "")
		if err != nil {
			t.Fatalf("selectServices() returned error: %v", err)
		}

		got := serviceIDs(selected)
		want := []string{"github", "instagram"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("selected IDs = %v, want %v", got, want)
		}
	})

	t.Run("specific enabled service", func(t *testing.T) {
		selected, err := selectServices(catalogue, "instagram")
		if err != nil {
			t.Fatalf("selectServices() returned error: %v", err)
		}

		if len(selected) != 1 || selected[0].ID != "instagram" {
			t.Fatalf("selected = %v, want only instagram", serviceIDs(selected))
		}
	})

	t.Run("unknown service", func(t *testing.T) {
		if _, err := selectServices(catalogue, "missing"); err == nil {
			t.Fatal("selectServices() returned nil error, want unknown service error")
		}
	})

	t.Run("disabled service", func(t *testing.T) {
		if _, err := selectServices(catalogue, "disabled"); err == nil {
			t.Fatal("selectServices() returned nil error, want disabled service error")
		}
	})

	t.Run("no enabled services", func(t *testing.T) {
		if _, err := selectServices([]service.Service{testService("disabled", false)}, ""); err == nil {
			t.Fatal("selectServices() returned nil error, want no services error")
		}
	})
}

func TestRunDiagnosticOneShot(t *testing.T) {
	services := []service.Service{
		testService("github", true),
		testService("disabled", false),
		testService("openai", true),
	}

	var calls []string
	check := func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		calls = append(calls, svc.ID)

		return monitoring.CheckResult{
			ServiceID: svc.ID,
			CheckedAt: time.Date(2026, 9, 15, 9, 20, len(calls), 0, time.UTC),
			Duration:  time.Duration(len(calls)) * 100 * time.Millisecond,
			StatusCode: map[string]int{
				"github": httpStatusOK,
				"openai": httpStatusForbidden,
			}[svc.ID],
		}
	}

	var out bytes.Buffer
	err := runDiagnostic(context.Background(), options{interval: defaultInterval}, services, check, &out)
	if err != nil {
		t.Fatalf("runDiagnostic() returned error: %v", err)
	}

	if strings.Join(calls, ",") != "github,openai" {
		t.Fatalf("calls = %v, want sequential enabled services", calls)
	}

	output := out.String()
	for _, want := range []string{
		"github",
		"openai",
		"status=200",
		"status=403",
		"Summary",
		"HTTP 200",
		"HTTP 403",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunDiagnosticRepeatedStopsAfterCancellationAndPrintsPartialSummary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	services := []service.Service{
		testService("github", true),
		testService("openai", true),
	}

	var calls []string
	check := func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		calls = append(calls, svc.ID)
		cancel()

		return monitoring.CheckResult{
			ServiceID: svc.ID,
			CheckedAt: time.Date(2026, 9, 15, 9, 20, 0, 0, time.UTC),
			Duration:  10 * time.Millisecond,
			ErrorKind: monitoring.ErrorCanceled,
		}
	}

	var out bytes.Buffer
	err := runDiagnostic(ctx, options{duration: time.Hour, interval: defaultInterval}, services, check, &out)
	if err != nil {
		t.Fatalf("runDiagnostic() returned error: %v", err)
	}

	if strings.Join(calls, ",") != "github" {
		t.Fatalf("calls = %v, want first service only after cancellation", calls)
	}

	output := out.String()
	for _, want := range []string{
		"interrupted; printing summary collected so far",
		"error=canceled",
		"Summary",
		"github",
		"openai",
		"checks:        0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunRepeatedWaitsUntilNextRoundTarget(t *testing.T) {
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	services := []service.Service{testService("github", true)}

	var starts []time.Time
	check := func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		starts = append(starts, clock.Now())
		clock.Advance(5 * time.Second)

		return monitoring.CheckResult{
			ServiceID:  svc.ID,
			CheckedAt:  clock.Now().UTC(),
			Duration:   5 * time.Second,
			StatusCode: httpStatusOK,
		}
	}

	summary := newSummary(services)
	var out bytes.Buffer
	runRepeatedWithClock(
		context.Background(),
		options{duration: 46 * time.Second, interval: 15 * time.Second},
		services,
		check,
		&out,
		summary,
		clock,
	)

	wantStarts := []time.Time{
		start,
		start.Add(15 * time.Second),
		start.Add(30 * time.Second),
		start.Add(45 * time.Second),
	}
	assertTimes(t, starts, wantStarts)
	assertTimes(t, clock.sleepTargets, wantStarts[1:])
}

func TestRunRepeatedSkipsMissedRoundTargetsAfterOverrun(t *testing.T) {
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	services := []service.Service{testService("github", true)}
	durations := []time.Duration{
		5 * time.Second,
		19 * time.Second,
		5 * time.Second,
		time.Second,
	}

	var starts []time.Time
	check := func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		starts = append(starts, clock.Now())
		duration := durations[len(starts)-1]
		clock.Advance(duration)

		return monitoring.CheckResult{
			ServiceID:  svc.ID,
			CheckedAt:  clock.Now().UTC(),
			Duration:   duration,
			StatusCode: httpStatusOK,
		}
	}

	summary := newSummary(services)
	var out bytes.Buffer
	runRepeatedWithClock(
		context.Background(),
		options{duration: 61 * time.Second, interval: 15 * time.Second},
		services,
		check,
		&out,
		summary,
		clock,
	)

	wantStarts := []time.Time{
		start,
		start.Add(15 * time.Second),
		start.Add(45 * time.Second),
		start.Add(60 * time.Second),
	}
	assertTimes(t, starts, wantStarts)

	if !strings.Contains(out.String(), "missed_rounds=1") {
		t.Fatalf("output missing missed round diagnostic:\n%s", out.String())
	}
}

func TestRunRepeatedDoesNotStartRoundAtDurationBoundary(t *testing.T) {
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	services := []service.Service{testService("github", true)}

	var starts []time.Time
	check := func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		starts = append(starts, clock.Now())
		clock.Advance(5 * time.Second)

		return monitoring.CheckResult{
			ServiceID:  svc.ID,
			CheckedAt:  clock.Now().UTC(),
			Duration:   5 * time.Second,
			StatusCode: httpStatusOK,
		}
	}

	summary := newSummary(services)
	var out bytes.Buffer
	runRepeatedWithClock(
		context.Background(),
		options{duration: 45 * time.Second, interval: 15 * time.Second},
		services,
		check,
		&out,
		summary,
		clock,
	)

	wantStarts := []time.Time{
		start,
		start.Add(15 * time.Second),
		start.Add(30 * time.Second),
	}
	assertTimes(t, starts, wantStarts)
}

func TestRunRepeatedWaitingHonorsCancellation(t *testing.T) {
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	clock.cancelOnSleep = true
	ctx, cancel := context.WithCancel(context.Background())
	clock.cancel = cancel
	defer cancel()

	services := []service.Service{testService("github", true)}
	var calls int
	check := func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		calls++
		clock.Advance(5 * time.Second)

		return monitoring.CheckResult{
			ServiceID:  svc.ID,
			CheckedAt:  clock.Now().UTC(),
			Duration:   5 * time.Second,
			StatusCode: httpStatusOK,
		}
	}

	summary := newSummary(services)
	var out bytes.Buffer
	runRepeatedWithClock(
		ctx,
		options{duration: time.Minute, interval: 15 * time.Second},
		services,
		check,
		&out,
		summary,
		clock,
	)

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	if !strings.Contains(out.String(), "interrupted; printing summary collected so far") {
		t.Fatalf("output missing interruption diagnostic:\n%s", out.String())
	}
}

func TestSummaryAggregation(t *testing.T) {
	summary := newSummary([]service.Service{testService("github", true)})
	summary.record(monitoring.CheckResult{
		ServiceID:  "github",
		Duration:   100 * time.Millisecond,
		StatusCode: httpStatusOK,
	})
	summary.record(monitoring.CheckResult{
		ServiceID:  "github",
		Duration:   200 * time.Millisecond,
		StatusCode: httpStatusServiceUnavailable,
	})
	summary.record(monitoring.CheckResult{
		ServiceID: "github",
		Duration:  300 * time.Millisecond,
		ErrorKind: monitoring.ErrorTimeout,
	})

	var out bytes.Buffer
	summary.print(&out)

	output := out.String()
	for _, want := range []string{
		"checks:        3",
		"HTTP 200",
		"HTTP 503",
		"errors:        1",
		"error timeout:",
		"avg duration:  200ms",
		"min duration:  100ms",
		"max duration:  300ms",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary missing %q:\n%s", want, output)
		}
	}
}

func TestNextRoundTarget(t *testing.T) {
	start := time.Date(2026, 9, 15, 9, 20, 0, 0, time.UTC)
	interval := 15 * time.Second

	tests := []struct {
		name          string
		currentTarget time.Time
		finishedAt    time.Time
		wantTarget    time.Time
		wantSkipped   int
	}{
		{
			name:          "round finishes before next target",
			currentTarget: start,
			finishedAt:    start.Add(6 * time.Second),
			wantTarget:    start.Add(interval),
			wantSkipped:   0,
		},
		{
			name:          "round finishes exactly at next target",
			currentTarget: start,
			finishedAt:    start.Add(interval),
			wantTarget:    start.Add(interval),
			wantSkipped:   0,
		},
		{
			name:          "round finishes just after next target",
			currentTarget: start,
			finishedAt:    start.Add(interval + time.Nanosecond),
			wantTarget:    start.Add(2 * interval),
			wantSkipped:   1,
		},
		{
			name:          "round finishes after multiple missed targets",
			currentTarget: start,
			finishedAt:    start.Add(45*time.Second + time.Nanosecond),
			wantTarget:    start.Add(60 * time.Second),
			wantSkipped:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotSkipped := nextRoundTarget(tt.currentTarget, tt.finishedAt, interval)
			if !gotTarget.Equal(tt.wantTarget) {
				t.Fatalf("target = %s, want %s", gotTarget, tt.wantTarget)
			}

			if gotSkipped != tt.wantSkipped {
				t.Fatalf("skipped = %d, want %d", gotSkipped, tt.wantSkipped)
			}
		})
	}
}

func assertTimes(t *testing.T, got []time.Time, want []time.Time) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("times = %v, want %v", got, want)
	}

	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("time[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func serviceIDs(services []service.Service) []string {
	ids := make([]string, 0, len(services))
	for _, svc := range services {
		ids = append(ids, svc.ID)
	}

	return ids
}

type fakeClock struct {
	now           time.Time
	sleepTargets  []time.Time
	cancelOnSleep bool
	cancel        context.CancelFunc
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (clock *fakeClock) Now() time.Time {
	return clock.now
}

func (clock *fakeClock) SleepUntil(ctx context.Context, wakeAt time.Time) bool {
	clock.sleepTargets = append(clock.sleepTargets, wakeAt)
	if clock.cancelOnSleep {
		if clock.cancel != nil {
			clock.cancel()
		}
		return false
	}

	if err := ctx.Err(); err != nil {
		return false
	}

	if wakeAt.After(clock.now) {
		clock.now = wakeAt
	}

	return true
}

func (clock *fakeClock) Advance(duration time.Duration) {
	clock.now = clock.now.Add(duration)
}

func testService(id string, enabled bool) service.Service {
	return service.Service{
		ID:         id,
		Name:       "Test " + id,
		Category:   service.CategoryDeveloperCloud,
		WebsiteURL: "https://example.com",
		CheckURL:   "https://example.com",
		Weight:     1,
		Enabled:    enabled,
	}
}

const (
	httpStatusOK                 = 200
	httpStatusForbidden          = 403
	httpStatusServiceUnavailable = 503
)
