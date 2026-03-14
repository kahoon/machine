package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kahoon/machine"
	"github.com/kahoon/machine/actions"
	machineyaml "github.com/kahoon/machine/yaml"
	"github.com/kahoon/pending"
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
	reg := machine.NewRegistry().
		MustInput("start", 0).
		MustInput("stop", 1).
		MustInput("timeout", 2).
		MustInput("door_closed", 3)

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

	def, err := machineyaml.CompileFile(machinePath(), reg)
	if err != nil {
		return nil, err
	}

	manager := pending.NewManager()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	}()

	exec := actions.NewPendingExecutor(manager)
	lines := make([]string, 0, 5)

	stopCase, err := machine.New(def, exec, machine.WithInstanceID("door-1"))
	if err != nil {
		return nil, err
	}
	if _, err := stopCase.Apply(context.Background(), machine.MustInputs(reg, "start", "door_closed")); err != nil {
		return nil, err
	}
	lines = append(lines, "stop scenario after start: "+stopCase.State())
	if _, err := stopCase.Apply(context.Background(), machine.MustInputs(reg, "stop")); err != nil {
		return nil, err
	}
	time.Sleep(75 * time.Millisecond)
	lines = append(lines, "stop scenario final: "+stopCase.State())

	timeoutCase, err := machine.New(def, exec, machine.WithInstanceID("door-2"))
	if err != nil {
		return nil, err
	}
	if _, err := timeoutCase.Apply(context.Background(), machine.MustInputs(reg, "start", "door_closed")); err != nil {
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
