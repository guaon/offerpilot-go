package session

import "testing"

func TestSessionAccessAndClaimOwnership(t *testing.T) {
	manager, err := NewSessionManager(nil)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}

	owned := manager.Create("user-1")
	if !manager.CanAccess(owned.ID, "user-1") {
		t.Fatal("owner should be able to access its session")
	}
	if manager.CanAccess(owned.ID, "user-2") || manager.CanAccess(owned.ID, "") {
		t.Fatal("another user or an anonymous caller must not access an owned session")
	}
	if err := manager.ClaimSession(owned.ID, "user-2"); err == nil {
		t.Fatal("claiming another user's session should fail")
	}

	anonymous := manager.Create("")
	if !manager.CanAccess(anonymous.ID, "") {
		t.Fatal("anonymous session should remain accessible to anonymous handler checks")
	}
	if err := manager.ClaimSession(anonymous.ID, "user-1"); err != nil {
		t.Fatalf("claim anonymous session: %v", err)
	}
	if manager.CanAccess(anonymous.ID, "") || !manager.CanAccess(anonymous.ID, "user-1") {
		t.Fatal("claimed session should only be accessible to its new owner")
	}
}

func TestDeleteRequiresExactOwner(t *testing.T) {
	manager, _ := NewSessionManager(nil)
	sess := manager.Create("user-1")

	if err := manager.Delete(sess.ID, "user-2"); err == nil {
		t.Fatal("another user must not delete the session")
	}
	if err := manager.Delete(sess.ID, "user-1"); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
}
