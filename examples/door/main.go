package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kahoon/machine"
)

type notifyTimeoutParams struct {
	Message string `yaml:"message"`
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

func runDemo() ([]string, error) {
	reg := machine.NewRegistry()

	timeoutEvents := make(chan string, 1)

	machine.MustRegisterAction(reg, "begin_cycle", func(_ context.Context, _ machine.ActionRequest[machine.NoParams]) error {
		return nil
	})
	machine.MustRegisterAction(reg, "stop_cycle", func(_ context.Context, _ machine.ActionRequest[machine.NoParams]) error {
		return nil
	})
	machine.MustRegisterAction(reg, "clear_fault", func(_ context.Context, _ machine.ActionRequest[machine.NoParams]) error {
		return nil
	})
	machine.MustRegisterAction(reg, "notify_timeout", func(_ context.Context, req machine.ActionRequest[notifyTimeoutParams]) error {
		timeoutEvents <- fmt.Sprintf("%s: %s", req.InstanceID, req.Params.Message)
		return nil
	})

	lines := make([]string, 0, 5)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stopCase, err := machine.New(
		machine.FromFile(machinePath()),
		machine.WithID("door-1"),
		machine.WithRegistry(reg),
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = stopCase.Shutdown(closeCtx)
	}()

	if err := stopCase.Set(ctx, "door_closed"); err != nil {
		return nil, err
	}
	if err := stopCase.Send(ctx, "start"); err != nil {
		return nil, err
	}
	lines = append(lines, "stop scenario after start: "+stopCase.State())
	if err := stopCase.Send(ctx, "stop"); err != nil {
		return nil, err
	}
	time.Sleep(75 * time.Millisecond)
	lines = append(lines, "stop scenario final: "+stopCase.State())

	timeoutCase, err := machine.New(
		machine.FromFile(machinePath()),
		machine.WithID("door-2"),
		machine.WithRegistry(reg),
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = timeoutCase.Shutdown(closeCtx)
	}()

	if err := timeoutCase.Set(ctx, "door_closed"); err != nil {
		return nil, err
	}
	if err := timeoutCase.Send(ctx, "start"); err != nil {
		return nil, err
	}
	lines = append(lines, "timeout scenario after start: "+timeoutCase.State())

	select {
	case msg := <-timeoutEvents:
		lines = append(lines, msg)
	case <-time.After(250 * time.Millisecond):
		return nil, fmt.Errorf("timed out waiting for timeout transition")
	}

	lines = append(lines, "timeout scenario final: "+timeoutCase.State())
	return lines, nil
}

func machinePath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "machine.yaml")
}
