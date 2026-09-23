package models

import "testing"

func TestTransitions(t *testing.T) {
	states := []string{"pending_confirmation", "accepted", "preparing", "ready", "delivering", "delivered", "paid", "cancelled", "unknown"}
	allowed := map[string]bool{"pending_confirmation:accepted": true, "pending_confirmation:cancelled": true, "accepted:preparing": true, "accepted:cancelled": true, "preparing:ready": true, "ready:delivered": true, "ready:delivering": true, "delivering:delivered": true, "delivered:paid": true}
	for _, from := range states {
		for _, to := range states {
			if CanTransition(from, to) != allowed[from+":"+to] {
				t.Errorf("unexpected transition %s -> %s", from, to)
			}
		}
	}
}
