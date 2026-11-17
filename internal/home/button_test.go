package home

// User story: the cat feeder in Home Assistant is a button entity
// ("выдать корм"). When the user says «покорми котов», the home route must
// find that button and press it, but only when the owner has exposed it to
// the voice assistant. No feeding phrases live in the bridge: the phrase
// names no device type, so the default inventory must include buttons, and
// the planner decides which exposed button matches.

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"quasarbridge/internal/llm"
)

type buttonFixture struct {
	exposed bool
	presses []map[string]any
}

func (f *buttonFixture) Call(_ context.Context, name string, args map[string]any) (jsontext.Value, error) {
	var value any
	switch name {
	case "ha_get_skill_guide":
		value = map[string]any{"success": true}
	case "ha_search":
		entities := []any{}
		if args["domain_filter"] == "button" {
			entities = append(entities, map[string]any{"entity_id": "button.feeder_food_out", "friendly_name": "Кормушка", "state": "2026-11-17T17:00:00+00:00"})
		}
		value = map[string]any{"entities": entities, "has_more": false}
	case "ha_get_entity_exposure":
		value = map[string]any{"success": true, "exposed_to": map[string]any{"conversation": f.exposed}}
	case "ha_call_service":
		f.presses = append(f.presses, args)
		value = map[string]any{"success": true}
	case "ha_get_state":
		value = map[string]any{"data": map[string]any{"entity_id": "button.feeder_food_out", "state": "2026-11-18T00:00:00+00:00"}}
	default:
		panic("unexpected tool " + name)
	}
	data, _ := json.Marshal(value)
	return data, nil
}

func pressFeeder(context.Context, []llm.Message, bool) (string, error) {
	return `{"answer":"","actions":[{"domain":"button","service":"press","entity_id":"button.feeder_food_out","data":{}}]}`, nil
}

func TestFeedTheCatsPressesTheExposedFeederButton(t *testing.T) {
	f := &buttonFixture{exposed: true}
	c := Controller{Tools: f, Planner: plannerFunc(pressFeeder)}
	answer, err := c.Handle(t.Context(), "Покорми котов")
	if err != nil {
		t.Fatalf("answer=%q error=%v", answer, err)
	}
	if len(f.presses) != 1 || f.presses[0]["domain"] != "button" || f.presses[0]["service"] != "press" || f.presses[0]["entity_id"] != "button.feeder_food_out" {
		t.Fatalf("presses=%v", f.presses)
	}
	if answer != "Команда отправлена: Кормушка." {
		t.Fatalf("answer=%q", answer)
	}
}

func TestButtonHiddenFromTheVoiceAssistantIsNeverPressed(t *testing.T) {
	f := &buttonFixture{exposed: false}
	c := Controller{Tools: f, Planner: plannerFunc(pressFeeder)}
	if _, err := c.Handle(t.Context(), "Покорми котов"); err != nil {
		t.Fatal(err)
	}
	if len(f.presses) != 0 {
		t.Fatalf("hidden button pressed: %v", f.presses)
	}
}
