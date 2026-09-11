package liverecipes

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ArmTriggers is the set of GitHub events that must reach each live arm, by
// the recipe name that is the join key between a workflow job and a half.
//
// This exists because the arm split is decided by a job-level `if:`, and a
// conditional CI job is invisible when it stops running: GitHub reports the
// job as skipped, the check is green, and nothing anywhere says which events
// the arm was supposed to have. Dropping an event from that `if:` is
// therefore a silent removal of a gate — the failure mode bd gqlc-zase was
// opened about, one layer up. Declared here, the removal is a red test naming
// the arm and the event.
//
// Per arm and never as a total: the two halves run on different event sets by
// design (merge_group is the neo4j half's alone, decision 0010), so any rule
// over their union is satisfied by one healthy half while the other goes
// dark.
//
// Update rule: changing a job's `if:` in codegen-live.yml means changing the
// row here in the same commit, and the reason for the new set belongs in that
// job's comment rather than in this one.
var ArmTriggers = map[string][]string{
	// The PR-blocking half. It carries no `if:` at all, so it runs on every
	// event the workflow declares.
	Neo4jRecipe: {"merge_group", "pull_request", "schedule", "workflow_dispatch"},
	// PR-blocking since bd gqlc-ezwae, on the wall-time measurement recorded
	// in codegen-live.yml's header. merge_group is deliberately absent: a
	// queued merge is not charged the AGE containers (decision 0010).
	AgeRecipe: {"pull_request", "schedule", "workflow_dispatch"},
}

// filterKeys are the `on.<event>` sub-keys that stop an event from firing on
// every occurrence of its kind. A live arm behind one of these is the path
// filter gqlc-zase rejected by name — a false skip recreates the unwitnessed
// exposure silently, one layer down — so reaching a live recipe through a
// filtered trigger is a complaint rather than a narrower event set.
var filterKeys = []string{"paths", "paths-ignore", "branches", "branches-ignore"}

// ReadTriggers is every event that reaches each live arm in the repository
// rooted at root, keyed by recipe name, alongside the complaints about
// READING the workflows — an `if:` in a shape this cannot enumerate, an
// enumerated event the workflow never fires, a trigger behind a path filter.
//
// Complaints are not a narrower answer: each one is a shape whose real event
// set this reader would have to guess at, and a guess here reads as an arm
// that runs when it does not.
func ReadTriggers(root string) (map[string][]string, []string, error) {
	dir := filepath.Join(root, workflowDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	reached := make(map[string]map[string]bool)
	var complaints []string
	read := 0
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // a path read out of the caller's own repo root
		if err != nil {
			return nil, nil, err
		}
		byRecipe, fileComplaints, err := WorkflowTriggers(src)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		read++
		for _, complaint := range fileComplaints {
			complaints = append(complaints, entry.Name()+": "+complaint)
		}
		for recipe, events := range byRecipe {
			if reached[recipe] == nil {
				reached[recipe] = make(map[string]bool)
			}
			for _, event := range events {
				reached[recipe][event] = true
			}
		}
	}
	if read == 0 {
		complaints = append(complaints, fmt.Sprintf(
			"no workflow file was read from %s, so every arm below reads as running on no event", dir))
	}
	triggers := make(map[string][]string, len(reached))
	for recipe, events := range reached {
		triggers[recipe] = slices.Sorted(maps.Keys(events))
	}
	return triggers, complaints, nil
}

// WorkflowTriggers is every event that reaches each recipe one workflow's jobs
// invoke `just` with.
//
// A job with no `if:` runs on every event the workflow declares; a job with
// one runs on the events that `if:` enumerates. Only `run:` steps are read,
// which is the same reading WorkflowRecipes does and has the same blind spot:
// a recipe reached through a composite action is found here by neither.
func WorkflowTriggers(src []byte) (map[string][]string, []string, error) {
	var doc struct {
		On   yaml.Node `yaml:"on"`
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, nil, err
	}
	declared, filtered, complaints := workflowEvents(&doc.On)

	byRecipe := make(map[string][]string)
	for _, id := range slices.Sorted(maps.Keys(doc.Jobs)) {
		job := doc.Jobs[id]
		var recipes []string
		for _, step := range job.Steps {
			for _, recipe := range JustRecipes(step.Run) {
				if _, live := ArmTriggers[recipe]; live {
					recipes = append(recipes, recipe)
				}
			}
		}
		// Jobs reaching no live arm are skipped entirely rather than read and
		// discarded: their conditions and trigger filters are legitimate
		// shapes this guard has no claim on, and complaining about them would
		// red the live-arm guard for a reason that is not about the arms.
		if len(recipes) == 0 {
			continue
		}
		events, jobComplaints := jobEvents(id, job.If, declared)
		complaints = append(complaints, jobComplaints...)
		for _, event := range events {
			if filtered[event] {
				complaints = append(complaints, fmt.Sprintf(
					"job %s is reached by `on.%s`, which carries a trigger filter: a recipe behind one "+
						"runs on some pull requests and not others, and the ones it skips are green with "+
						"nothing said", id, event))
			}
		}
		for _, recipe := range recipes {
			byRecipe[recipe] = append(byRecipe[recipe], events...)
		}
	}
	return byRecipe, complaints, nil
}

// jobEvents is the events an `if:` selects out of the workflow's own triggers.
// An enumerated event the workflow does not declare is a complaint and not a
// narrower set: it is a condition written for an event that never arrives, so
// the arm it guards runs nowhere and reads here as gated.
func jobEvents(id, expr string, declared []string) ([]string, []string) {
	if strings.TrimSpace(expr) == "" {
		return slices.Clone(declared), nil
	}
	enumerated, readable := EnumeratedEvents(expr)
	if !readable {
		return nil, []string{fmt.Sprintf(
			"job %s carries an `if:` this reader cannot enumerate (%s): the arm split is stated as a "+
				"disjunction of `github.event_name == '<event>'` so that removing an event is visible "+
				"here (decision 0010)", id, strings.TrimSpace(expr))}
	}
	var events, complaints []string
	for _, event := range enumerated {
		if !slices.Contains(declared, event) {
			complaints = append(complaints, fmt.Sprintf(
				"job %s enumerates %q, which this workflow's `on:` does not declare: the condition "+
					"can never be true, so the job is gated rather than run", id, event))
			continue
		}
		events = append(events, event)
	}
	return events, complaints
}

// EnumeratedEvents reads a job-level `if:` as the positive enumeration
// decision 0010 requires it to be: a disjunction of
// `github.event_name == '<event>'` and nothing else.
//
// readable is false for every other shape, a negation included. `!=` is the
// form decision 0010 removed — it went TRUE on a merge group and charged
// every queued merge the AGE containers — and a reader that evaluated it
// would have to enumerate the complement, which is the set of events GitHub
// defines rather than the set this workflow fires. Refusing is the fail-loud
// direction: an unreadable condition reddens the guard instead of quietly
// reporting an arm as running on events nobody wrote down.
func EnumeratedEvents(expr string) (events []string, readable bool) {
	expr = strings.TrimSpace(expr)
	if inner, wrapped := strings.CutPrefix(expr, "${{"); wrapped {
		trimmed, closed := strings.CutSuffix(strings.TrimSpace(inner), "}}")
		if !closed {
			return nil, false
		}
		expr = strings.TrimSpace(trimmed)
	}
	if expr == "" {
		return nil, false
	}
	for _, term := range strings.Split(expr, "||") {
		name, ok := eventEquality(term)
		if !ok {
			return nil, false
		}
		events = append(events, name)
	}
	return events, true
}

// eventEquality reads one disjunct as `github.event_name == '<event>'`. The
// quoted value may carry no quote of its own, which is what stops a term that
// hides a second operator (`'x' && github.event_name == 'y'`) from being read
// as the single event `x`.
func eventEquality(term string) (string, bool) {
	lhs, rhs, found := strings.Cut(term, "==")
	if !found || strings.TrimSpace(lhs) != "github.event_name" {
		return "", false
	}
	rhs = strings.TrimSpace(rhs)
	for _, quote := range []string{"'", `"`} {
		inner, opened := strings.CutPrefix(rhs, quote)
		if !opened {
			continue
		}
		name, closed := strings.CutSuffix(inner, quote)
		if closed && name != "" && !strings.ContainsAny(name, `'"`) {
			return name, true
		}
	}
	return "", false
}

// workflowEvents is the events an `on:` block declares and which of them carry
// a trigger filter. The mapping form is the only one this repository writes;
// the sequence and scalar forms are read too because they are the same
// declaration, and anything else is a complaint rather than an empty set — an
// `on:` this could not read would make every job below it look unreachable.
func workflowEvents(on *yaml.Node) ([]string, map[string]bool, []string) {
	filtered := make(map[string]bool)
	switch on.Kind {
	case yaml.MappingNode:
		var events []string
		for i := 0; i+1 < len(on.Content); i += 2 {
			event := on.Content[i].Value
			events = append(events, event)
			filtered[event] = hasFilterKey(on.Content[i+1])
		}
		return events, filtered, nil
	case yaml.SequenceNode:
		var events []string
		for _, item := range on.Content {
			events = append(events, item.Value)
		}
		return events, filtered, nil
	case yaml.ScalarNode:
		return []string{on.Value}, filtered, nil
	default:
		return nil, filtered, []string{fmt.Sprintf(
			"`on:` is a YAML node of kind %d this reader cannot enumerate, so no job below it has a "+
				"known event set", on.Kind)}
	}
}

// hasFilterKey reports whether one event's configuration narrows when it
// fires.
func hasFilterKey(config *yaml.Node) bool {
	if config.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(config.Content); i += 2 {
		if slices.Contains(filterKeys, config.Content[i].Value) {
			return true
		}
	}
	return false
}
