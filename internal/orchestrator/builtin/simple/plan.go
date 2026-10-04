package simple

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Events for a plan's progress, shown as a checklist while it runs.
const (
	EventPlanCreated = "plan.created"
	EventPlanStep    = "plan.step"
)

// noteRunes caps one step's notes, so the final answer sees every step.
const noteRunes = 1500

const workerInstructions = "You are working on one part of a larger request. Write short notes for that part only: " +
	"the facts, numbers, names, and links that matter, from the reference material when there is some. " +
	"Do not answer the whole request, and do not write an introduction or a conclusion."

const planGuidance = "Toskar worked through this request in parts; the notes from each part are in the reference material. " +
	"Use them to answer the whole request in one reply, and compare or combine them where the request asks. " +
	"Do not mention the notes or the parts."

// runPlan works through a request with several parts (spec §22–23). Web
// lookups for independent parts run at the same time. With worker slots
// (the Team strategy, or a worker model), each independent part has its own
// slot, which Norn can place on another computer, and parts on different
// computers are written side by side; otherwise the model writes notes for
// each part in turn. Later parts of a sequence see the notes before them.
// It returns the notes for the final answer.
func runPlan(ctx context.Context, env pluginapi.ExecutionEnvironment, ch chan<- pluginapi.OrchestrationEvent, profile contracts.AIProfile, role string, plan huginn.Plan, prompt, reference string, pages int) string {
	steps := plan.Steps
	// A step that asks for a file is done by the final answer, which writes
	// the file from everything gathered.
	if n := len(steps); n > 1 {
		if _, isFile := askedForFile(steps[n-1]); isFile {
			steps = steps[:n-1]
		}
	}
	if len(steps) == 0 {
		return ""
	}
	env.Emit(EventPlanCreated, map[string]any{"steps": plan.Steps, "parallel": plan.Parallel})
	web := plan.NeedsWeb && webAllowed(profile)

	looked := make([]string, len(steps))
	if web && plan.Parallel {
		var wg sync.WaitGroup
		for i, step := range steps {
			wg.Add(1)
			go func(i int, step string) {
				defer wg.Done()
				looked[i], _ = lookUp(ctx, env, profile, lookupQuery(step), step, pages)
			}(i, step)
		}
		wg.Wait()
	}

	spread := spreadWorkers(profile)
	roles := make([]string, len(steps))
	nodes := make([]string, len(steps))
	for i := range steps {
		roles[i] = role
		if spread {
			roles[i] = fmt.Sprintf("%s:%d", profiles.RoleWorker, i+1)
		}
		// Placed one at a time, so each slot sees where the others went.
		nodes[i], _ = env.NodeForRole(roles[i])
	}

	notes := make([]string, len(steps))
	done := make([]bool, len(steps))
	work := func(i int, earlier string) {
		step := steps[i]
		env.Emit(EventPlanStep, map[string]any{"index": i, "step": step, "status": "running", "role": roles[i], "node_id": nodes[i]})
		if spread {
			announceRole(env, roles[i])
		}
		material := looked[i]
		if web && !plan.Parallel {
			material, _ = lookUp(ctx, env, profile, lookupQuery(step), step, pages)
		}
		ref := joinReference(reference, material)
		if earlier != "" {
			ref = joinReference(ref, "Notes from earlier parts:\n"+earlier)
		}
		ask := []pluginapi.ChatMessage{
			{Role: "system", Content: workerInstructions},
			{Role: "user", Content: withReference("The whole request: "+prompt+"\n\nYour part: "+step, ref)},
		}
		content, metrics, err := generateText(ctx, env, roles[i], ask)
		if ctx.Err() != nil {
			// Stopped: keep the parts already finished, not this one.
			return
		}
		status := "done"
		note := strings.TrimSpace(tools.VisibleText(content))
		if err != nil || note == "" {
			status = "failed"
			note = "(no notes for this part)"
		}
		if utf8.RuneCountInString(note) > noteRunes {
			note = string([]rune(note)[:noteRunes]) + "…"
		}
		notes[i], done[i] = note, true
		if spread {
			reportRole(ch, env, roles[i], metrics)
		}
		env.Emit(EventPlanStep, map[string]any{"index": i, "step": step, "status": status, "role": roles[i], "node_id": nodes[i]})
	}

	if plan.Parallel && spread {
		// Parts on different computers run at the same time; parts that
		// share a computer run one after another there.
		byNode := map[string][]int{}
		order := []string{}
		for i, n := range nodes {
			if _, ok := byNode[n]; !ok {
				order = append(order, n)
			}
			byNode[n] = append(byNode[n], i)
		}
		var wg sync.WaitGroup
		for _, n := range order {
			wg.Add(1)
			go func(indices []int) {
				defer wg.Done()
				for _, i := range indices {
					if ctx.Err() != nil {
						return
					}
					work(i, "")
				}
			}(byNode[n])
		}
		wg.Wait()
	} else {
		var earlier strings.Builder
		for i := range steps {
			if ctx.Err() != nil {
				break
			}
			prior := ""
			if !plan.Parallel {
				prior = earlier.String()
			}
			work(i, prior)
			if done[i] {
				fmt.Fprintf(&earlier, "[%d] %s\n%s\n\n", i+1, steps[i], notes[i])
			}
		}
	}

	var out strings.Builder
	for i, step := range steps {
		if done[i] {
			fmt.Fprintf(&out, "[%d] %s\n%s\n\n", i+1, step, notes[i])
		}
	}
	if out.Len() == 0 {
		return ""
	}
	return "Notes from each part of the request:\n" + strings.TrimSpace(out.String())
}
