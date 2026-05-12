package client

import "testing"

func TestNewControlPlaneClientRejectsInvalidBackend(t *testing.T) {
	client, err := NewControlPlaneClient("bad")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if client != nil {
		t.Fatalf("expected nil client, got %#v", client)
	}
}
