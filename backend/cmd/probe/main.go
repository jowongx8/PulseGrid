package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
)

const defaultInterval = 15 * time.Second

type options struct {
	serviceID string
	duration  time.Duration
	interval  time.Duration
}

type checkFunc func(context.Context, service.Service) monitoring.CheckResult

type probeClock interface {
	Now() time.Time
	SleepUntil(context.Context, time.Time) bool
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) SleepUntil(ctx context.Context, wakeAt time.Time) bool {
	timer := time.NewTimer(time.Until(wakeAt))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	checker := monitoring.NewHTTPChecker()
	if err := runDiagnostic(ctx, opts, service.Catalogue(), checker.Check, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "probe: %v\n", err)
		os.Exit(1)
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{
		interval: defaultInterval,
	}

	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.serviceID, "service", "", "stable service ID to probe")
	flags.DurationVar(&opts.duration, "duration", 0, "total repeated probe duration; 0 runs once")
	flags.DurationVar(&opts.interval, "interval", defaultInterval, "target interval between repeated round starts")

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}

	if flags.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}

	if err := validateOptions(opts); err != nil {
		return options{}, err
	}

	return opts, nil
}

func validateOptions(opts options) error {
	if opts.duration < 0 {
		return fmt.Errorf("duration must not be negative")
	}

	if opts.duration > 0 && opts.interval <= 0 {
		return fmt.Errorf("interval must be positive when duration is set")
	}

	return nil
}

func runDiagnostic(ctx context.Context, opts options, catalogue []service.Service, check checkFunc, out io.Writer) error {
	services, err := selectServices(catalogue, opts.serviceID)
	if err != nil {
		return err
	}

	summary := newSummary(services)
	if opts.duration == 0 {
		runRound(ctx, services, check, out, summary)
		summary.print(out)
		return nil
	}

	runRepeated(ctx, opts, services, check, out, summary)
	return nil
}

func selectServices(catalogue []service.Service, serviceID string) ([]service.Service, error) {
	if serviceID != "" {
		for _, svc := range catalogue {
			if svc.ID != serviceID {
				continue
			}

			if !svc.Enabled {
				return nil, fmt.Errorf("service %q is disabled", serviceID)
			}

			return []service.Service{svc}, nil
		}

		return nil, fmt.Errorf("service %q was not found in the catalogue", serviceID)
	}

	var selected []service.Service
	for _, svc := range catalogue {
		if svc.Enabled {
			selected = append(selected, svc)
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no enabled services selected")
	}

	return selected, nil
}

func runRepeated(ctx context.Context, opts options, services []service.Service, check checkFunc, out io.Writer, summary *runSummary) {
	runRepeatedWithClock(ctx, opts, services, check, out, summary, realClock{})
}

func runRepeatedWithClock(ctx context.Context, opts options, services []service.Service, check checkFunc, out io.Writer, summary *runSummary, clock probeClock) {
	start := clock.Now()
	end := start.Add(opts.duration)
	targetStart := start
	round := 1

	for targetStart.Before(end) {
		if clock.Now().Before(targetStart) {
			if !clock.SleepUntil(ctx, targetStart) {
				fmt.Fprintln(out, "interrupted; printing summary collected so far")
				break
			}
		}

		if !clock.Now().Before(end) {
			break
		}

		now := clock.Now()
		if delay := now.Sub(targetStart); delay > 0 && round > 1 {
			fmt.Fprintf(out, "round=%d target=%s delayed_by=%s\n", round, targetStart.UTC().Format(time.RFC3339), formatDuration(delay))
		}

		runRound(ctx, services, check, out, summary)
		if ctx.Err() != nil {
			fmt.Fprintln(out, "interrupted; printing summary collected so far")
			break
		}

		nextTarget, skippedRounds := nextRoundTarget(targetStart, clock.Now(), opts.interval)
		if skippedRounds > 0 {
			fmt.Fprintf(out, "missed_rounds=%d next_target=%s\n", skippedRounds, nextTarget.UTC().Format(time.RFC3339))
		}

		targetStart = nextTarget
		round++
	}

	summary.print(out)
}

func nextRoundTarget(currentTarget time.Time, finishedAt time.Time, interval time.Duration) (time.Time, int) {
	nextTarget := currentTarget.Add(interval)
	skippedRounds := 0

	for nextTarget.Before(finishedAt) {
		nextTarget = nextTarget.Add(interval)
		skippedRounds++
	}

	return nextTarget, skippedRounds
}

func runRound(ctx context.Context, services []service.Service, check checkFunc, out io.Writer, summary *runSummary) {
	for _, svc := range services {
		if ctx.Err() != nil {
			return
		}

		result := check(ctx, svc)
		printResult(out, result)
		summary.record(result)

		if ctx.Err() != nil {
			return
		}
	}
}

func printResult(out io.Writer, result monitoring.CheckResult) {
	errorKind := "-"
	if result.ErrorKind != monitoring.ErrorNone {
		errorKind = string(result.ErrorKind)
	}

	fmt.Fprintf(
		out,
		"%s %-24s status=%-3d duration=%s error=%s",
		result.CheckedAt.UTC().Format(time.RFC3339),
		result.ServiceID,
		result.StatusCode,
		formatDuration(result.Duration),
		errorKind,
	)

	if result.ErrorMessage != "" {
		fmt.Fprintf(out, " message=%q", result.ErrorMessage)
	}

	fmt.Fprintln(out)
}

type runSummary struct {
	order     []string
	byService map[string]*serviceSummary
}

type serviceSummary struct {
	checks       int
	statusCounts map[int]int
	errorCounts  map[monitoring.ErrorKind]int
	total        time.Duration
	min          time.Duration
	max          time.Duration
}

func newSummary(services []service.Service) *runSummary {
	summary := &runSummary{
		order:     make([]string, 0, len(services)),
		byService: make(map[string]*serviceSummary, len(services)),
	}

	for _, svc := range services {
		if _, exists := summary.byService[svc.ID]; exists {
			continue
		}

		summary.order = append(summary.order, svc.ID)
		summary.byService[svc.ID] = newServiceSummary()
	}

	return summary
}

func newServiceSummary() *serviceSummary {
	return &serviceSummary{
		statusCounts: make(map[int]int),
		errorCounts:  make(map[monitoring.ErrorKind]int),
	}
}

func (summary *runSummary) record(result monitoring.CheckResult) {
	serviceStats, exists := summary.byService[result.ServiceID]
	if !exists {
		summary.order = append(summary.order, result.ServiceID)
		serviceStats = newServiceSummary()
		summary.byService[result.ServiceID] = serviceStats
	}

	serviceStats.record(result)
}

func (summary *serviceSummary) record(result monitoring.CheckResult) {
	summary.checks++
	summary.total += result.Duration

	if summary.checks == 1 || result.Duration < summary.min {
		summary.min = result.Duration
	}

	if result.Duration > summary.max {
		summary.max = result.Duration
	}

	if result.StatusCode != 0 {
		summary.statusCounts[result.StatusCode]++
	}

	if result.ErrorKind != monitoring.ErrorNone {
		summary.errorCounts[result.ErrorKind]++
	}
}

func (summary *runSummary) print(out io.Writer) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Summary")

	for _, serviceID := range summary.order {
		serviceStats := summary.byService[serviceID]
		fmt.Fprintln(out)
		fmt.Fprintln(out, serviceID)
		fmt.Fprintf(out, "  checks:        %d\n", serviceStats.checks)

		for _, statusCode := range sortedStatusCodes(serviceStats.statusCounts) {
			fmt.Fprintf(out, "  HTTP %-3d:      %d\n", statusCode, serviceStats.statusCounts[statusCode])
		}

		errorTotal := serviceStats.errorTotal()
		fmt.Fprintf(out, "  errors:        %d\n", errorTotal)
		for _, errorKind := range sortedErrorKinds(serviceStats.errorCounts) {
			fmt.Fprintf(out, "  error %-10s %d\n", errorKind+":", serviceStats.errorCounts[errorKind])
		}

		if serviceStats.checks == 0 {
			fmt.Fprintln(out, "  avg duration:  -")
			fmt.Fprintln(out, "  min duration:  -")
			fmt.Fprintln(out, "  max duration:  -")
			continue
		}

		fmt.Fprintf(out, "  avg duration:  %s\n", formatDuration(serviceStats.total/time.Duration(serviceStats.checks)))
		fmt.Fprintf(out, "  min duration:  %s\n", formatDuration(serviceStats.min))
		fmt.Fprintf(out, "  max duration:  %s\n", formatDuration(serviceStats.max))
	}
}

func (summary *serviceSummary) errorTotal() int {
	total := 0
	for _, count := range summary.errorCounts {
		total += count
	}

	return total
}

func sortedStatusCodes(counts map[int]int) []int {
	statusCodes := make([]int, 0, len(counts))
	for statusCode := range counts {
		statusCodes = append(statusCodes, statusCode)
	}
	sort.Ints(statusCodes)

	return statusCodes
}

func sortedErrorKinds(counts map[monitoring.ErrorKind]int) []monitoring.ErrorKind {
	errorKinds := make([]monitoring.ErrorKind, 0, len(counts))
	for errorKind := range counts {
		errorKinds = append(errorKinds, errorKind)
	}

	sort.Slice(errorKinds, func(i, j int) bool {
		return errorKinds[i] < errorKinds[j]
	})

	return errorKinds
}

func formatDuration(duration time.Duration) string {
	if duration >= time.Millisecond {
		return duration.Round(time.Millisecond).String()
	}

	return duration.String()
}
