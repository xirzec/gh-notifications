package main

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestMarkCmd(t *testing.T) {
	t.Run("read issues PATCH", func(t *testing.T) {
		doer := &recordingDoer{}
		n := Notification{ID: "42", Subject: NotificationSubject{Title: "T"}}
		msg := markCmd(doer, n, "read")()
		mm, ok := msg.(markedMsg)
		if !ok {
			t.Fatalf("expected markedMsg, got %T", msg)
		}
		if mm.err != nil || mm.id != "42" || mm.action != "read" {
			t.Errorf("got %+v", mm)
		}
		if len(doer.calls) != 1 || doer.calls[0] != "PATCH notifications/threads/42" {
			t.Errorf("calls = %v", doer.calls)
		}
	})

	t.Run("done issues DELETE", func(t *testing.T) {
		doer := &recordingDoer{}
		n := Notification{ID: "7", Subject: NotificationSubject{Title: "T"}}
		msg := markCmd(doer, n, "done")()
		mm := msg.(markedMsg)
		if mm.action != "done" {
			t.Errorf("action = %q", mm.action)
		}
		if len(doer.calls) != 1 || doer.calls[0] != "DELETE notifications/threads/7" {
			t.Errorf("calls = %v", doer.calls)
		}
	})

	t.Run("unsubscribe issues DELETE subscription then DELETE thread", func(t *testing.T) {
		doer := &recordingDoer{}
		n := Notification{ID: "5", Subject: NotificationSubject{Title: "T"}}
		msg := markCmd(doer, n, "unsubscribe")()
		mm := msg.(markedMsg)
		if mm.action != "unsubscribe" {
			t.Errorf("action = %q", mm.action)
		}
		want := []string{
			"DELETE notifications/threads/5/subscription",
			"DELETE notifications/threads/5",
		}
		if len(doer.calls) != 2 || doer.calls[0] != want[0] || doer.calls[1] != want[1] {
			t.Errorf("calls = %v, want %v", doer.calls, want)
		}
	})
}

func newTestPicker(t *testing.T, notifications []Notification, doer requestDoer) pickerModel {
	t.Helper()
	m := newPickerModel(doer, notifications)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(pickerModel)
}

func TestPickerMarkReadConfirmAndCancel(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
	}

	t.Run("r enters confirming", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('r'))
		m = updated.(pickerModel)
		if !m.confirming {
			t.Fatal("expected confirming state")
		}
		if m.confirmTarget.ID != "1" {
			t.Errorf("confirmTarget = %q", m.confirmTarget.ID)
		}
		if m.confirmAction != "read" {
			t.Errorf("confirmAction = %q, want read", m.confirmAction)
		}
	})

	t.Run("d enters confirming with done action", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('d'))
		m = updated.(pickerModel)
		if !m.confirming {
			t.Fatal("expected confirming state")
		}
		if m.confirmAction != "done" {
			t.Errorf("confirmAction = %q, want done", m.confirmAction)
		}
	})

	t.Run("u enters confirming with unsubscribe action", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('u'))
		m = updated.(pickerModel)
		if !m.confirming {
			t.Fatal("expected confirming state")
		}
		if m.confirmAction != "unsubscribe" {
			t.Errorf("confirmAction = %q, want unsubscribe", m.confirmAction)
		}
	})

	t.Run("n cancels without confirming", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('r'))
		m = updated.(pickerModel)
		updated, _ = m.Update(runeKey('n'))
		m = updated.(pickerModel)
		if m.confirming {
			t.Error("expected confirming cleared after cancel")
		}
	})

	t.Run("y resolves confirmation", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('r'))
		m = updated.(pickerModel)
		updated, cmd := m.Update(runeKey('y'))
		m = updated.(pickerModel)
		if m.confirming {
			t.Error("expected confirming cleared after y")
		}
		if cmd == nil {
			t.Error("expected a command to be issued")
		}
	})
}

func TestPickerMarkedMsgRemovesItem(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	m := newTestPicker(t, notifications, &recordingDoer{})

	updated, _ := m.Update(markedMsg{id: "1", title: "A"})
	m = updated.(pickerModel)

	items := m.list.Items()
	if len(items) != 1 {
		t.Fatalf("expected 1 item left, got %d", len(items))
	}
	if ni, ok := items[0].(notificationItem); !ok || ni.n.ID != "2" {
		t.Errorf("remaining item = %+v", items[0])
	}
}

func TestPickerMarkedMsgErrorKeepsItems(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	m := newTestPicker(t, notifications, &recordingDoer{})

	updated, _ := m.Update(markedMsg{id: "1", title: "A", err: errors.New("boom")})
	m = updated.(pickerModel)

	if len(m.list.Items()) != 1 {
		t.Errorf("expected item retained on error, got %d", len(m.list.Items()))
	}
}
