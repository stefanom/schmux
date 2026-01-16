package main

import (
	"context"
	"fmt"

	"github.com/sergek/schmux/pkg/cli"
)

// RefreshOverlayCommand implements the refresh-overlay command.
type RefreshOverlayCommand struct {
	client cli.DaemonClient
}

// NewRefreshOverlayCommand creates a new refresh-overlay command.
func NewRefreshOverlayCommand(client cli.DaemonClient) *RefreshOverlayCommand {
	return &RefreshOverlayCommand{client: client}
}

// Run executes the refresh-overlay command.
func (cmd *RefreshOverlayCommand) Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: schmux refresh-overlay <workspace-id>")
	}

	workspaceID := args[0]

	// Check if daemon is running
	if !cmd.client.IsRunning() {
		return fmt.Errorf("daemon is not running. Start it with: schmux start")
	}

	// Verify workspace exists
	workspaces, err := cmd.client.GetWorkspaces()
	if err != nil {
		return fmt.Errorf("failed to get workspaces: %w", err)
	}

	var found bool
	var wsPath string
	for _, ws := range workspaces {
		if ws.ID == workspaceID {
			found = true
			wsPath = ws.Path
			break
		}
	}

	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Refresh the overlay
	fmt.Printf("Refreshing overlay for workspace %s (%s)...\n", workspaceID, wsPath)
	if err := cmd.client.RefreshOverlay(context.Background(), workspaceID); err != nil {
		return fmt.Errorf("failed to refresh overlay: %w", err)
	}

	fmt.Printf("Overlay refreshed successfully for workspace %s.\n", workspaceID)
	return nil
}
