package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/kahoon/machine"
)

type reasonParams struct {
	Reason string `yaml:"reason"`
}

type messageParams struct {
	Message string `yaml:"message"`
}

type labelParams struct {
	Label string `yaml:"label"`
}

func main() {
	lines, err := runDemo()
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}

type policy struct {
	name string
	file string
}

func runDemo() ([]string, error) {
	reg := machine.NewRegistry()
	lines := make([]string, 0, 32)
	events := make(chan string, 32)

	machine.MustRegisterAction(reg, "prompt_mfa", func(_ context.Context, req machine.ActionRequest[reasonParams]) error {
		events <- fmt.Sprintf("%s prompt_mfa: %s", req.InstanceID, req.Params.Reason)
		return nil
	})
	machine.MustRegisterAction(reg, "alert_security", func(_ context.Context, req machine.ActionRequest[messageParams]) error {
		events <- fmt.Sprintf("%s alert_security: %s", req.InstanceID, req.Params.Message)
		return nil
	})
	machine.MustRegisterAction(reg, "issue_session", func(_ context.Context, req machine.ActionRequest[labelParams]) error {
		events <- fmt.Sprintf("%s issue_session: %s", req.InstanceID, req.Params.Label)
		return nil
	})
	machine.MustRegisterAction(reg, "revoke_session", func(_ context.Context, req machine.ActionRequest[reasonParams]) error {
		events <- fmt.Sprintf("%s revoke_session: %s", req.InstanceID, req.Params.Reason)
		return nil
	})
	machine.MustRegisterAction(reg, "lock_account", func(_ context.Context, req machine.ActionRequest[reasonParams]) error {
		events <- fmt.Sprintf("%s lock_account: %s", req.InstanceID, req.Params.Reason)
		return nil
	})
	machine.MustRegisterAction(reg, "clear_lockout", func(_ context.Context, req machine.ActionRequest[machine.NoParams]) error {
		events <- fmt.Sprintf("%s clear_lockout", req.InstanceID)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	policies := []policy{
		{name: "baseline", file: "baseline.yaml"},
		{name: "strict", file: "strict.yaml"},
	}
	for idx, current := range policies {
		if idx > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "policy "+current.name)

		policyLines, err := runTrustedFlow(ctx, reg, events, current)
		if err != nil {
			return nil, err
		}
		lines = append(lines, policyLines...)

		policyLines, err = runRiskyAdminFlow(ctx, reg, events, current)
		if err != nil {
			return nil, err
		}
		lines = append(lines, policyLines...)
	}

	return lines, nil
}

func runTrustedFlow(ctx context.Context, reg *machine.Registry, events <-chan string, current policy) ([]string, error) {
	engine, err := newEngine(current.file, "trusted-user", reg)
	if err != nil {
		return nil, err
	}
	defer shutdownEngine(engine)

	lines := []string{}
	if err := engine.Set(ctx, "trusted_device"); err != nil {
		return nil, err
	}
	if err := engine.Send(ctx, "login"); err != nil {
		return nil, err
	}
	lines = append(lines, current.name+" trusted after login: "+engine.State())

	switch current.name {
	case "baseline":
		lines = append(lines, current.name+" "+mustEvent(events))
		if err := engine.Send(ctx, "suspicious_activity"); err != nil {
			return nil, err
		}
		lines = append(lines, current.name+" trusted after suspicious activity: "+engine.State())
		lines = append(lines, prefixEvents(current.name, mustEvents(events, 2))...)
		if err := engine.Send(ctx, "mfa_passed"); err != nil {
			return nil, err
		}
		lines = append(lines, current.name+" trusted after MFA pass: "+engine.State())
		lines = append(lines, current.name+" "+mustEvent(events))
		if err := waitForState(engine, "signed_out"); err != nil {
			return nil, err
		}
		lines = append(lines, current.name+" trusted final state: "+engine.State())
		lines = append(lines, current.name+" "+mustEvent(events))
	case "strict":
		lines = append(lines, prefixEvents(current.name, mustEvents(events, 2))...)
		if err := engine.Send(ctx, "mfa_passed"); err != nil {
			return nil, err
		}
		lines = append(lines, current.name+" trusted after MFA pass: "+engine.State())
		lines = append(lines, current.name+" "+mustEvent(events))
		if err := engine.Send(ctx, "suspicious_activity"); err != nil {
			return nil, err
		}
		lines = append(lines, current.name+" trusted after suspicious activity: "+engine.State())
		lines = append(lines, prefixEvents(current.name, mustEvents(events, 2))...)
		if err := waitForState(engine, "signed_out"); err != nil {
			return nil, err
		}
		lines = append(lines, current.name+" trusted final state: "+engine.State())
		lines = append(lines, current.name+" "+mustEvent(events))
	default:
		return nil, fmt.Errorf("unknown policy %q", current.name)
	}

	return lines, nil
}

func runRiskyAdminFlow(ctx context.Context, reg *machine.Registry, events <-chan string, current policy) ([]string, error) {
	engine, err := newEngine(current.file, "risky-admin", reg)
	if err != nil {
		return nil, err
	}
	defer shutdownEngine(engine)

	lines := []string{}
	if err := engine.Set(ctx, "admin_user", "geo_risk"); err != nil {
		return nil, err
	}
	if err := engine.Send(ctx, "login"); err != nil {
		return nil, err
	}
	lines = append(lines, current.name+" risky admin after login: "+engine.State())
	lines = append(lines, prefixEvents(current.name, mustEvents(events, 2))...)
	if err := engine.Send(ctx, "mfa_failed"); err != nil {
		return nil, err
	}
	lines = append(lines, current.name+" risky admin after MFA failure: "+engine.State())
	lines = append(lines, current.name+" "+mustEvent(events))
	if err := waitForState(engine, "signed_out"); err != nil {
		return nil, err
	}
	lines = append(lines, current.name+" risky admin final state: "+engine.State())
	lines = append(lines, current.name+" "+mustEvent(events))

	return lines, nil
}

func newEngine(file, id string, reg *machine.Registry) (*machine.Engine, error) {
	return machine.New(
		machine.FromFile(machinePath(file)),
		machine.WithID(id),
		machine.WithRegistry(reg),
		machine.WithActionLimit(1, machine.StrategyBlock),
	)
}

func shutdownEngine(engine *machine.Engine) {
	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	_ = engine.Shutdown(closeCtx)
}

func mustEvent(events <-chan string) string {
	select {
	case event := <-events:
		return event
	case <-time.After(250 * time.Millisecond):
		return "missing event"
	}
}

func mustEvents(events <-chan string, count int) []string {
	items := make([]string, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, mustEvent(events))
	}
	sort.Strings(items)
	return items
}

func prefixEvents(prefix string, events []string) []string {
	lines := make([]string, 0, len(events))
	for _, event := range events {
		lines = append(lines, prefix+" "+event)
	}
	return lines
}

func waitForState(engine *machine.Engine, want string) error {
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if engine.State() == want {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("state %q did not reach %q", engine.State(), want)
}

func machinePath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), name)
}
