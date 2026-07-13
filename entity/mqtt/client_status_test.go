package mqtt

import (
	"testing"
)

func TestNewDisconnectedStatus(t *testing.T) {
	s := NewDisconnectedStatus()
	if s.IsConnected() {
		t.Error("NewDisconnectedStatus should have Connected=false")
	}
	if len(s.Subscriptions) != 0 {
		t.Error("NewDisconnectedStatus should have empty Subscriptions")
	}
}

func TestNewConnectedStatus(t *testing.T) {
	s := NewConnectedStatus("tcp://broker:1883", "client-1")
	if !s.IsConnected() {
		t.Error("NewConnectedStatus should have Connected=true")
	}
	if s.BrokerURL != "tcp://broker:1883" {
		t.Errorf("BrokerURL = %s, want tcp://broker:1883", s.BrokerURL)
	}
	if s.ClientID != "client-1" {
		t.Errorf("ClientID = %s, want client-1", s.ClientID)
	}
}

func TestAddAndRemoveSubscription(t *testing.T) {
	s := NewConnectedStatus("tcp://broker:1883", "client-1")

	s.AddSubscription("iot/test", 1)
	if !s.HasSubscriptions() {
		t.Error("HasSubscriptions should return true after AddSubscription")
	}

	s.AddSubscription("iot/test2", 2)
	snapshot := s.Snapshot()
	if len(snapshot.Subscriptions) != 2 {
		t.Errorf("Subscriptions count = %d, want 2", len(snapshot.Subscriptions))
	}

	s.RemoveSubscription("iot/test")
	snapshot = s.Snapshot()
	if len(snapshot.Subscriptions) != 1 {
		t.Errorf("Subscriptions count after remove = %d, want 1", len(snapshot.Subscriptions))
	}
}

func TestIncrementCounters(t *testing.T) {
	s := NewConnectedStatus("tcp://broker:1883", "client-1")

	s.IncrementSent()
	s.IncrementSent()
	s.IncrementReceived()
	s.IncrementReconnect()

	snapshot := s.Snapshot()
	if snapshot.TotalMessagesSent != 2 {
		t.Errorf("TotalMessagesSent = %d, want 2", snapshot.TotalMessagesSent)
	}
	if snapshot.TotalMessagesReceived != 1 {
		t.Errorf("TotalMessagesReceived = %d, want 1", snapshot.TotalMessagesReceived)
	}
	if snapshot.ReconnectCount != 1 {
		t.Errorf("ReconnectCount = %d, want 1", snapshot.ReconnectCount)
	}
}

func TestSnapshotIsIndependent(t *testing.T) {
	s := NewConnectedStatus("tcp://broker:1883", "client-1")
	s.AddSubscription("iot/test", 1)

	snapshot := s.Snapshot()
	snapshot.Connected = false
	snapshot.Subscriptions["iot/other"] = 2

	if !s.IsConnected() {
		t.Error("Original should still be Connected after snapshot modification")
	}
	if s.HasSubscriptions() && len(s.Snapshot().Subscriptions) == 2 {
		t.Error("Original subscriptions should not be affected by snapshot modification")
	}
}
