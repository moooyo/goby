package backuppg

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type recoveryConstraint struct {
	table, name string
	foreign     bool
}
type recoveryColumnDefault struct {
	table, column string
	generated     bool
}
type recoveryFunction struct{ name, arguments string }
type recoveryDropPlan struct {
	triggers    [][2]string
	constraints []recoveryConstraint
	indexes     []string
	defaults    []recoveryColumnDefault
	functions   []recoveryFunction
}

// Deletion selectors come from the compiled artifact, never a later scan of
// mutable database contents. An object introduced after validation is left for
// RESTRICT to reject rather than being adopted into the deletion plan.
func compiledRecoveryDropPlan(version int64) (recoveryDropPlan, error) {
	var plan recoveryDropPlan
	data, err := catalogFiles.ReadFile(fmt.Sprintf("catalogs/schema-%d-postgresql-17.json", version))
	if err != nil {
		return plan, ErrSchema
	}
	var baseline catalogBaseline
	if json.Unmarshal(data, &baseline) != nil || baseline.Version != version {
		return plan, ErrSchema
	}
	var objects []struct {
		Kind  string          `json:"kind"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(baseline.Objects, &objects) != nil {
		return plan, ErrSchema
	}
	constraintIndexes := make(map[string]bool)
	for _, object := range objects {
		switch object.Kind {
		case "trigger":
			table, name, ok := strings.Cut(object.Name, ".")
			if !ok || !identifierPattern.MatchString(table) || !identifierPattern.MatchString(name) {
				return plan, ErrSchema
			}
			plan.triggers = append(plan.triggers, [2]string{table, name})
		case "constraint":
			table, name, ok := strings.Cut(object.Name, ".")
			if !ok || !identifierPattern.MatchString(table) || !identifierPattern.MatchString(name) {
				return plan, ErrSchema
			}
			var value struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(object.Value, &value) != nil {
				return plan, ErrSchema
			}
			plan.constraints = append(plan.constraints, recoveryConstraint{table: table, name: name, foreign: value.Type == "f"})
			if value.Type == "p" || value.Type == "u" || value.Type == "x" {
				constraintIndexes[name] = true
			}
		case "index":
			if !identifierPattern.MatchString(object.Name) {
				return plan, ErrSchema
			}
			plan.indexes = append(plan.indexes, object.Name)
		case "column":
			table, _, ok := strings.Cut(object.Name, ".")
			if !ok || !identifierPattern.MatchString(table) {
				return plan, ErrSchema
			}
			var value struct {
				Name      string  `json:"name"`
				Default   *string `json:"default"`
				Generated string  `json:"generated"`
			}
			if json.Unmarshal(object.Value, &value) != nil || !identifierPattern.MatchString(value.Name) {
				return plan, ErrSchema
			}
			if value.Default != nil {
				plan.defaults = append(plan.defaults, recoveryColumnDefault{table: table, column: value.Name, generated: value.Generated != ""})
			}
		case "function":
			name, args, ok := strings.Cut(object.Name, "(")
			if !ok || !identifierPattern.MatchString(name) || !strings.HasSuffix(args, ")") {
				return plan, ErrSchema
			}
			plan.functions = append(plan.functions, recoveryFunction{name: name, arguments: strings.TrimSuffix(args, ")")})
		}
	}
	retainedIndexes := plan.indexes[:0]
	for _, name := range plan.indexes {
		if !constraintIndexes[name] {
			retainedIndexes = append(retainedIndexes, name)
		}
	}
	plan.indexes = retainedIndexes
	sort.Slice(plan.constraints, func(i, j int) bool {
		a, b := plan.constraints[i], plan.constraints[j]
		if a.foreign != b.foreign {
			return a.foreign
		}
		if a.table != b.table {
			return a.table < b.table
		}
		return a.name < b.name
	})
	return plan, nil
}
