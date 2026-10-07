package automations

import (
	"regexp"
	"strings"
)

// A condition needs a machine-readable value in the result: the price for a
// threshold, availability, or whether it's significant. The server adds the
// instruction for it when a run starts (#204), so a saved prompt is only
// the task the person wrote, whatever made it: the page, toskarctl, chat, or
// the API. The sentences are the ones the page used to store in prompts,
// so TaskPrompt recognizes and removes them from automations saved before.

const (
	availableInstruction   = `Include a JSON object in the result, {"available": true} when the item is available and {"available": false} when it is not.`
	significantInstruction = `Include a JSON object in the result, {"significant": true} when this is worth a notification and {"significant": false} when it is not.`
)

var (
	priceInstructionLine = regexp.MustCompile(`Include a JSON object in the result with the numeric price(?: in [A-Z]{3})?, for example \{"price": 420\}\.`)
	extraBlankLines      = regexp.MustCompile(`\n{3,}`)
)

func priceInstruction(currency string) string {
	unit := ""
	if currency != "" {
		unit = " in " + currency
	}
	return `Include a JSON object in the result with the numeric price` + unit + `, for example {"price": 420}.`
}

// SignalInstruction is what a run is asked to include for its condition, or
// "" when it needs nothing.
func SignalInstruction(n Notification) string {
	if n.Mode != NotifyOnCondition || n.Condition == nil {
		return ""
	}
	switch n.Condition.Kind {
	case ConditionThreshold:
		return priceInstruction(n.Condition.Currency)
	case ConditionAvailable:
		return availableInstruction
	case ConditionSignificant:
		return significantInstruction
	}
	return ""
}

// TaskPrompt is a prompt without a condition's instruction: the task the
// person wrote.
func TaskPrompt(prompt string) string {
	text := priceInstructionLine.ReplaceAllString(strings.TrimSpace(prompt), "")
	for _, line := range []string{availableInstruction, significantInstruction} {
		text = strings.ReplaceAll(text, line, "")
	}
	return strings.TrimSpace(extraBlankLines.ReplaceAllString(text, "\n\n"))
}

// RunPrompt is what a run is given: the task, then its condition's
// instruction.
func RunPrompt(a Automation) string {
	task := TaskPrompt(a.Prompt)
	if instruction := SignalInstruction(a.Notification); instruction != "" {
		return task + "\n\n" + instruction
	}
	return task
}
