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
		if len(m.confirmTargets) != 1 || m.confirmTargets[0].ID != "1" {
			t.Errorf("confirmTargets = %v", m.confirmTargets)
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

func TestActionForKey(t *testing.T) {
	cases := []struct {
		key    string
		action string
		bulk   bool
		ok     bool
	}{
		{"r", "read", false, true},
		{"d", "done", false, true},
		{"u", "unsubscribe", false, true},
		{"R", "read", true, true},
		{"D", "done", true, true},
		{"U", "unsubscribe", true, true},
		{"x", "", false, false},
	}
	for _, c := range cases {
		action, bulk, ok := actionForKey(c.key)
		if action != c.action || bulk != c.bulk || ok != c.ok {
			t.Errorf("actionForKey(%q) = (%q,%v,%v), want (%q,%v,%v)", c.key, action, bulk, ok, c.action, c.bulk, c.ok)
		}
	}
}

func TestPickerBulkActionTargetsAllVisible(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "3", Subject: NotificationSubject{Title: "C"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	m := newTestPicker(t, notifications, &recordingDoer{})

	// Shift+D => bulk mark done on all visible items.
	updated, _ := m.Update(runeKey('D'))
	m = updated.(pickerModel)
	if !m.confirming {
		t.Fatal("expected confirming state")
	}
	if m.confirmAction != "done" {
		t.Errorf("confirmAction = %q, want done", m.confirmAction)
	}
	if len(m.confirmTargets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(m.confirmTargets))
	}
}

func TestPickerBulkConfirmIssuesAllCommands(t *testing.T) {
	doer := &recordingDoer{}
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	m := newTestPicker(t, notifications, doer)

	updated, _ := m.Update(runeKey('D'))
	m = updated.(pickerModel)
	updated, cmd := m.Update(runeKey('y'))
	m = updated.(pickerModel)
	if m.confirming {
		t.Error("expected confirming cleared after y")
	}
	if cmd == nil {
		t.Fatal("expected commands to be issued")
	}
	// Execute the batch to trigger the underlying mark commands.
	drainCmd(cmd)
	if len(doer.calls) != 2 {
		t.Errorf("expected 2 mark calls, got %v", doer.calls)
	}
}

// drainCmd recursively executes a tea.Cmd (including batches) so the side
// effects of the underlying commands run.
func drainCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drainCmd(c)
		}
	}
}

func TestPickerFocusRepo(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "a/x"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "a/x"}},
		{ID: "3", Subject: NotificationSubject{Title: "C"}, Repository: NotificationRepo{FullName: "b/y"}},
	}

	t.Run("f focuses selected repo", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		// Selected item is index 0 (repo a/x).
		updated, _ := m.Update(runeKey('f'))
		m = updated.(pickerModel)
		if m.focusRepo != "a/x" {
			t.Fatalf("focusRepo = %q, want a/x", m.focusRepo)
		}
		if got := len(m.list.VisibleItems()); got != 2 {
			t.Errorf("visible = %d, want 2", got)
		}
	})

	t.Run("esc clears focus", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('f'))
		m = updated.(pickerModel)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = updated.(pickerModel)
		if m.focusRepo != "" {
			t.Errorf("focusRepo = %q, want empty", m.focusRepo)
		}
		if got := len(m.list.VisibleItems()); got != 3 {
			t.Errorf("visible = %d, want 3", got)
		}
	})

	t.Run("bulk action while focused targets only that repo", func(t *testing.T) {
		m := newTestPicker(t, notifications, &recordingDoer{})
		updated, _ := m.Update(runeKey('f'))
		m = updated.(pickerModel)
		updated, _ = m.Update(runeKey('D'))
		m = updated.(pickerModel)
		if !m.confirming {
			t.Fatal("expected confirming")
		}
		if len(m.confirmTargets) != 2 {
			t.Errorf("targets = %d, want 2 (only focused repo)", len(m.confirmTargets))
		}
		for _, n := range m.confirmTargets {
			if n.Repository.FullName != "a/x" {
				t.Errorf("unexpected target repo %q", n.Repository.FullName)
			}
		}
	})
}

func TestPickerMarkedMsgRemovesFromSourceOfTruth(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "a/x"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "b/y"}},
	}
	m := newTestPicker(t, notifications, &recordingDoer{})

	updated, _ := m.Update(markedMsg{id: "1", title: "A"})
	m = updated.(pickerModel)

	if len(m.all) != 1 || m.all[0].ID != "2" {
		t.Errorf("all = %v, want only ID 2", m.all)
	}
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

func TestBulkSummary(t *testing.T) {
	t.Run("all succeeded", func(t *testing.T) {
		m := pickerModel{bulkAction: "done", bulkTotal: 10, bulkSucceeded: 10, bulkFailed: 0}
		if got, want := m.bulkSummary(), "Marked 10/10 as done"; got != want {
			t.Errorf("bulkSummary() = %q, want %q", got, want)
		}
	})

	t.Run("partial failure", func(t *testing.T) {
		m := pickerModel{bulkAction: "done", bulkTotal: 10, bulkSucceeded: 8, bulkFailed: 2}
		if got, want := m.bulkSummary(), "Marked 8/10 as done (2 failed)"; got != want {
			t.Errorf("bulkSummary() = %q, want %q", got, want)
		}
	})

	t.Run("read action phrasing", func(t *testing.T) {
		m := pickerModel{bulkAction: "read", bulkTotal: 3, bulkSucceeded: 2, bulkFailed: 1}
		if got, want := m.bulkSummary(), "Marked 2/3 as read (1 failed)"; got != want {
			t.Errorf("bulkSummary() = %q, want %q", got, want)
		}
	})
}

func TestPickerBulkAggregatesPartialFailure(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "3", Subject: NotificationSubject{Title: "C"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	m := newTestPicker(t, notifications, &recordingDoer{})

	// Shift+D then confirm => bulk done on all three visible items.
	updated, _ := m.Update(runeKey('D'))
	m = updated.(pickerModel)
	updated, _ = m.Update(runeKey('y'))
	m = updated.(pickerModel)
	if m.bulkRemaining != 3 || m.bulkTotal != 3 || m.bulkAction != "done" {
		t.Fatalf("bulk state = remaining %d total %d action %q", m.bulkRemaining, m.bulkTotal, m.bulkAction)
	}

	// Two succeed, one fails.
	updated, _ = m.Update(markedMsg{id: "1", title: "A", action: "done"})
	m = updated.(pickerModel)
	updated, _ = m.Update(markedMsg{id: "2", title: "B", action: "done", err: errors.New("boom")})
	m = updated.(pickerModel)
	updated, cmd := m.Update(markedMsg{id: "3", title: "C", action: "done"})
	m = updated.(pickerModel)

	if m.bulkRemaining != 0 {
		t.Errorf("bulkRemaining = %d, want 0", m.bulkRemaining)
	}
	if m.bulkSucceeded != 2 || m.bulkFailed != 1 {
		t.Errorf("succeeded = %d, failed = %d; want 2 and 1", m.bulkSucceeded, m.bulkFailed)
	}
	if got, want := m.bulkSummary(), "Marked 2/3 as done (1 failed)"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if cmd == nil {
		t.Error("expected a status-message command after the final result")
	}

	// The failed item (ID 2) remains; the successful ones are gone.
	items := m.list.Items()
	if len(items) != 1 {
		t.Fatalf("expected 1 item retained, got %d", len(items))
	}
	if ni, ok := items[0].(notificationItem); !ok || ni.n.ID != "2" {
		t.Errorf("remaining item = %+v, want failed ID 2", items[0])
	}
}

func TestPickerSingleActionDoesNotEnterBulk(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	m := newTestPicker(t, notifications, &recordingDoer{})

	// Lowercase d acts on the selected item only.
	updated, _ := m.Update(runeKey('d'))
	m = updated.(pickerModel)
	updated, _ = m.Update(runeKey('y'))
	m = updated.(pickerModel)
	if m.bulkRemaining != 0 {
		t.Errorf("single action should not start bulk tracking, got remaining %d", m.bulkRemaining)
	}
}
