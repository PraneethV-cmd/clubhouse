package main

import (
	"clubhouse/internal/clog"
	"clubhouse/internal/dash"
	"clubhouse/internal/mcp"
)

func runMCP() {
	if err := mcp.Run(); err != nil {
		clog.Stderr().Fatal("MCP stopped", "err", err)
	}
}

func runWatch() {
	if err := dash.Run(); err != nil {
		clog.Stderr().Fatal("lounge stopped", "err", err)
	}
}
