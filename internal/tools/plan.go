package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// PresentPlan is plan mode's mechanical exit: the model proposes, the
// user decides. Draft-Only tier — presenting a plan changes nothing,
// so the tool is offered (and allowed) in every mode; in plan mode it
// is the ONLY way the model can graduate from research to action.
type PresentPlan struct {
	// Approve shows the plan to the user and blocks for the verdict
	// (the same pattern as the gate's prompt): proceed, whether
	// action-tier calls should run without prompting, or decline.
	// Nil fails closed: nobody can approve, so nothing proceeds.
	Approve func(plan string) (proceed, autoActions bool)
}

func (PresentPlan) Name() string { return "present_plan" }

func (PresentPlan) Description() string {
	return "Present your plan for approval before making any changes. Use after researching in plan mode: markdown with the goal, concrete steps, and risks. The user will approve or decline."
}

func (PresentPlan) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"plan": {"type": "string", "description": "The plan as markdown: goal, steps, risks"}
		},
		"required": ["plan"]
	}`)
}

// Tier implements Tool: Draft-Only — a proposal, never an application.
func (PresentPlan) Tier() Tier { return TierDraftOnly }

func (p PresentPlan) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Plan string `json:"plan"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if a.Plan == "" {
		return "", fmt.Errorf("present_plan: plan is required")
	}
	if p.Approve == nil {
		// Fail closed: with nobody to ask, no plan may proceed.
		return "The user cannot be reached to approve this plan. Do not make changes.", nil
	}
	proceed, auto := p.Approve(a.Plan)
	switch {
	case proceed && auto:
		return "The user approved the plan and enabled full-auto: implement it now with the tools now available; action-tier calls will run without prompting.", nil
	case proceed:
		return "The user approved the plan: implement it now with the tools now available; action-tier calls will ask the user before running.", nil
	default:
		return "The user declined the plan. Do not make changes; ask clarifying questions or revise the plan and present it again.", nil
	}
}
