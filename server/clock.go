// The clock package contains a minimal implementation of a vector clock, as well as some functions
// for persisting it to disk, restoring it from disk, and incrementing/updating it.
package server

import (
	"reflect"
	"go.uber.org/zap"
)

// Vector clock implementation that can be persisted to disk and restored
type VectorClock struct {
	Vector			map[string]int
	NodeId			string
	Logger			*zap.SugaredLogger
}

// Restore vector clock from disk
func (c *VectorClock) InitVector(num_nodes int) {
	// initialize vector clock timestamps to 0
	c.Vector = make(map[string]int)
	for i, _ := range c.Vector {
		c.Vector[i] = 0
	}
}

// Update vector clock based on incoming vector clock from other node
func (c *VectorClock) UpdateAndIncrement(incoming_clock map[string]int) {
	// merge in updated timestampsfor other nodes
	for id, _ := range incoming_clock {
		if c.Vector[id] < incoming_clock[id] {
			c.Vector[id] = incoming_clock[id] 
		}
	}
	// increment our own timestamp
	c.Vector[c.NodeId] += 1
	c.Logger.Infof("Updated and incremented local vector clock to: %v", c.Vector)
}

// Increment this nodes timestamp and persist vector clock to disk
func (c *VectorClock) Increment() {
	c.Vector[c.NodeId] += 1
	c.Logger.Infof("Incremented local vector clock to: %v", c.Vector)
}

// Check if the clock 1 vector clock is before (less than) clock 2 vector clock
func IsClockBefore(clock1 []int32, clock2 []int32) bool {
	// if clocks are equal return false
	if reflect.DeepEqual(clock1, clock2) {
		return false
	}
	// at every index clock1[i] must be <= clock2[i] otherwise return false
	for i, _ := range clock1 {
		if clock1[i] > clock2[i] {
			return false
		} 
	}
	return true
}