//go:build !darwin && !windows && !linux

package sysproxy

import "errors"

func set(string, int) error { return errors.New("system proxy is not supported on this system") }

func clearOurs(string, int) error { return nil }

func snapshot() (State, error) { return State{}, nil }

func restore(State) error { return nil }

func readEffective() (Effective, error) { return Effective{}, ErrUnknown }
