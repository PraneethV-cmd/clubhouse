// Package session joins or resumes this process' clubhouse member identity.
package session

import (
	"fmt"

	"clubhouse/internal/client"
	"clubhouse/internal/config"
	"clubhouse/internal/room"
)

// Client builds a configured client and makes sure the saved member id is
// actually present in the room. If the coordinator has pruned the old member,
// it joins again and stores the fresh id.
func Client(status string) (*client.Client, room.Room, error) {
	cfg := config.LoadOrDefault()
	token := config.ReadToken()
	if cfg.Server == "" || token == "" {
		return nil, room.Room{}, fmt.Errorf("not in a clubhouse")
	}
	cfg.EnsureName()
	c := client.New(cfg.Server, token)
	r, err := JoinOrResume(c, cfg.Name, status)
	return c, r, err
}

// JoinOrResume reuses the cached member id only when the server still knows
// about it. This prevents invisible locks/events after stale sessions.
func JoinOrResume(c *client.Client, name, status string) (room.Room, error) {
	if id := config.ReadSession(); id != "" {
		c.SetID(id)
		r, err := c.Heartbeat(status)
		if err != nil {
			return room.Room{}, err
		}
		if _, ok := r.Members[id]; ok {
			return r, nil
		}
	}
	r, err := c.Join(name)
	if err != nil {
		return room.Room{}, err
	}
	if err := config.WriteSession(c.ID()); err != nil {
		return room.Room{}, err
	}
	return r, nil
}
