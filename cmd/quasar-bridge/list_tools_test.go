package main

// User story: the owner gives Claude the two ha-mcp list tools. Claude then
// knows how to keep the shopping list and may write several items in one step.
// Home planning stays on its own client and keeps one call per step.

import (
	"testing"

	"quasarbridge/internal/assistant"
	"quasarbridge/internal/bridge"
	"quasarbridge/internal/llm"
)

func withListTools(names ...string) assistant.Config {
	cfg := assistant.DefaultConfig()
	cfg.Enabled = true
	cfg.MCPServers = []assistant.MCPServerConfig{{Name: "ha", AllowedTools: names}}
	return cfg
}

func TestClaudeKeepsTheListOnlyWhenItWasGivenTheWriteTool(t *testing.T) {
	disabled := withListTools("ha_set_todo_item")
	disabled.Enabled = false
	off := withListTools("ha_set_todo_item")
	off.MCPServers[0].Disabled = true
	cases := []struct {
		name string
		cfg  assistant.Config
		want bool
	}{
		{"write tool given", withListTools("ha_get_todo", "ha_set_todo_item"), true},
		{"only reading", withListTools("ha_get_todo"), false},
		{"tools off", disabled, false},
		{"server off", off, false},
		{"no servers", assistant.DefaultConfig(), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := claudeWritesTheList(c.cfg); got != c.want {
				t.Fatalf("claudeWritesTheList=%v", got)
			}
		})
	}
}

func TestParallelCallsReachOnlyTheClientOfTheToolAgent(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{"assistant_tools":{"parallel_calls":true}}`))
	if err != nil || !cfg.AssistantTools.ParallelCalls {
		t.Fatalf("parallel=%v err=%v", cfg.AssistantTools.ParallelCalls, err)
	}
	main := &llm.Client{Model: "fast"}
	engine := &bridge.Engine{}
	useAssistant(engine, answerClient(main, ""), nil, cfg.AssistantTools)
	agent, ok := engine.Assistant.(*assistant.Agent)
	if !ok {
		t.Fatalf("assistant=%T", engine.Assistant)
	}
	client, ok := agent.Client.(*llm.Client)
	if !ok || !client.ParallelTools || client.Model != "fast" {
		t.Fatalf("client=%+v", agent.Client)
	}
	if main.ParallelTools {
		t.Fatal("the client of home planning changed")
	}
}

func TestTheToolAgentGetsTheConfiguredLimitsAndKnowsTheList(t *testing.T) {
	cfg := withListTools("ha_set_todo_item")
	cfg.MaxRounds, cfg.MaxCalls = 5, 10
	engine := &bridge.Engine{}
	useAssistant(engine, &llm.Client{}, nil, cfg)
	agent := engine.Assistant.(*assistant.Agent)
	if agent.MaxRounds != 5 || agent.MaxCalls != 10 || !engine.ShoppingByTools {
		t.Fatalf("rounds=%d calls=%d list=%v", agent.MaxRounds, agent.MaxCalls, engine.ShoppingByTools)
	}
}
