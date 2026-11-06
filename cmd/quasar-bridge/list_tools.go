package main

import (
	"slices"

	"quasarbridge/internal/assistant"
	"quasarbridge/internal/bridge"
	"quasarbridge/internal/llm"
)

// listWriteTool is the ha-mcp tool that creates and updates todo items.
const listWriteTool = "ha_set_todo_item"

// useAssistant gives answers to the tool agent. Its client is a copy: home
// planning keeps one tool call per step.
func useAssistant(engine *bridge.Engine, answers *llm.Client, tools assistant.Toolset, cfg assistant.Config) {
	client := *answers
	client.ParallelTools = cfg.ParallelCalls
	engine.Assistant = &assistant.Agent{Client: &client, Tools: tools, MaxRounds: cfg.MaxRounds, MaxCalls: cfg.MaxCalls}
	engine.ShoppingByTools = claudeWritesTheList(cfg)
}

// claudeWritesTheList reports a deployment that gave Claude the write tool of ha-mcp.
func claudeWritesTheList(cfg assistant.Config) bool {
	return cfg.Enabled && slices.ContainsFunc(cfg.MCPServers, func(server assistant.MCPServerConfig) bool {
		return !server.Disabled && slices.Contains(server.AllowedTools, listWriteTool)
	})
}
