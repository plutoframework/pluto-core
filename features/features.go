// Package features evaluates immutable local boolean feature snapshots.
// Flags control availability, never identity, authorization or tenant isolation.
package features

import (
	"fmt"
	"strconv"
	"strings"
)

type Key = string

type Definition struct {
	Key           Key
	Name          string
	Description   string
	Default       bool
	ClientVisible bool
}

type Evaluator interface{ Enabled(Key) bool }

type Snapshot struct {
	definitions []Definition
	values      map[Key]bool
}

// NewSnapshot copies declarations and layers. Rightmost present overrides win;
// an explicit false differs from an absent key. Undeclared overrides are ignored.
func NewSnapshot(
	definitions []Definition,
	layers ...map[Key]bool,
) (*Snapshot, error) {
	values := make(map[Key]bool, len(definitions))
	environmentKeys := map[string]bool{}

	for _, definition := range definitions {
		key := EnvironmentKey(definition.Key)
		if definition.Key == "" || environmentKeys[key] {
			return nil, fmt.Errorf(
				"feature declarations require unique nonempty keys and environment names",
			)
		}

		environmentKeys[key] = true

		value := definition.Default
		for _, layer := range layers {
			if override, present := layer[definition.Key]; present {
				value = override
			}
		}

		values[definition.Key] = value
	}

	return &Snapshot{
		definitions: append([]Definition(nil), definitions...),
		values:      values,
	}, nil
}

// MustSnapshot is for application-owned declarations validated during startup.
// It panics only for invalid declarations, never for unknown override keys.
func MustSnapshot(definitions []Definition, layers ...map[Key]bool) *Snapshot {
	snapshot, err := NewSnapshot(definitions, layers...)
	if err != nil {
		panic(err)
	}

	return snapshot
}

func (s *Snapshot) Enabled(key Key) bool { return s != nil && s.values[key] }

func (s *Snapshot) Visible() map[string]bool {
	if s == nil {
		return map[string]bool{}
	}

	return Visible(s.definitions, s)
}

// Visible returns fresh client-visible results from an application evaluator.
// Applications must establish verified identity/scope before exposing this map.
func Visible(definitions []Definition, evaluator Evaluator) map[string]bool {
	result := map[string]bool{}
	if evaluator == nil {
		return result
	}

	for _, definition := range definitions {
		if definition.ClientVisible {
			result[definition.Key] = evaluator.Enabled(definition.Key)
		}
	}

	return result
}

type EvaluateFunc func(Key) bool

func (f EvaluateFunc) Enabled(key Key) bool { return f != nil && f(key) }

func Overrides(values map[Key]*bool) map[Key]bool {
	result := make(map[Key]bool)

	for key, value := range values {
		if value != nil {
			result[key] = *value
		}
	}

	return result
}

func EnvironmentKey(key Key) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(strings.ToUpper(key))
}

// ReadEnvironment reads declared keys only and reports names, never raw values.
// Prefix is application-owned, e.g. MY_APP_FEATURE_. No global environment read.
func ReadEnvironment(
	definitions []Definition,
	prefix string,
	lookup func(string) (string, bool),
) (map[Key]bool, error) {
	if _, err := NewSnapshot(definitions); err != nil {
		return nil, err
	}

	values := map[Key]bool{}
	if lookup == nil {
		return values, nil
	}

	for _, definition := range definitions {
		name := prefix + EnvironmentKey(definition.Key)
		if raw, present := lookup(name); present {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("%s must be boolean", name)
			}

			values[definition.Key] = value
		}
	}

	return values, nil
}
