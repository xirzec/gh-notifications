package main

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cli/go-gh/v2/pkg/browser"
)

// notificationItem adapts a Notification to the bubbles/list item interface.
type notificationItem struct {
	n Notification
}

func (i notificationItem) Title() string { return i.n.Subject.Title }

func (i notificationItem) Description() string {
	return fmt.Sprintf("%s  [%s]  %s ago", i.n.Repository.FullName, i.n.Reason, relativeAge(i.n.UpdatedAt, time.Now()))
}

func (i notificationItem) FilterValue() string {
	return i.n.Repository.FullName + " " + i.n.Reason + " " + i.n.Subject.Title
}

// openedMsg is emitted after attempting to open a notification in the browser.
type openedMsg struct {
	url string
	err error
}

// openCmd resolves a notification's web URL and opens it in the browser,
// running asynchronously so the UI never blocks.
func openCmd(doer requestDoer, n Notification) tea.Cmd {
	return func() tea.Msg {
		url := resolveWebURL(doer, n)
		// Discard launcher output so it cannot corrupt the alt-screen UI.
		err := browser.New("", io.Discard, io.Discard).Browse(url)
		return openedMsg{url: url, err: err}
	}
}

// markedMsg is emitted after attempting to mark a notification (read or done).
type markedMsg struct {
	id     string
	title  string
	action string
	err    error
}

// markCmd applies the given thread action ("read", "done", or "unsubscribe") to
// a notification asynchronously.
func markCmd(doer requestDoer, n Notification, action string) tea.Cmd {
	return func() tea.Msg {
		err := threadActions[action].apply(doer, n.ID)
		return markedMsg{id: n.ID, title: n.Subject.Title, action: action, err: err}
	}
}

// pickerModel is the Bubble Tea model backing the interactive notification list.
type pickerModel struct {
	list           list.Model
	doer           requestDoer
	all            []Notification // source of truth, independent of any focus
	focusRepo      string         // when set, the list is scoped to this OWNER/REPO
	width          int
	height         int
	confirming     bool
	confirmTargets []Notification
	confirmAction  string
}

// pickerHint is the keybinding summary shown in the list title.
const pickerHint = "enter open · r/d/u act · R/D/U all · f focus repo · esc back · q quit"

func newPickerModel(doer requestDoer, notifications []Notification) pickerModel {
	m := pickerModel{doer: doer, all: notifications}
	m.list = list.New(m.itemsForFocus(), list.NewDefaultDelegate(), 0, 0)
	m.list.SetStatusBarItemName("notification", "notifications")
	m.updateTitle()
	return m
}

// itemsForFocus returns the list items for the current focus: every notification
// when unfocused, or only those in the focused repository.
func (m pickerModel) itemsForFocus() []list.Item {
	out := make([]list.Item, 0, len(m.all))
	for _, n := range m.all {
		if m.focusRepo == "" || n.Repository.FullName == m.focusRepo {
			out = append(out, notificationItem{n: n})
		}
	}
	return out
}

// updateTitle sets the list title to reflect the current focus.
func (m *pickerModel) updateTitle() {
	if m.focusRepo != "" {
		m.list.Title = m.focusRepo + "  (" + pickerHint + ")"
	} else {
		m.list.Title = "Notifications  (" + pickerHint + ")"
	}
}

// applyFocus rebuilds the visible items from the current focus, resetting any
// text filter and the selection.
func (m *pickerModel) applyFocus() tea.Cmd {
	m.list.ResetFilter()
	cmd := m.list.SetItems(m.itemsForFocus())
	m.list.ResetSelected()
	m.updateTitle()
	return cmd
}

// removeFromAll drops the notification with the given ID from the source of
// truth so it does not reappear when the focus changes.
func (m *pickerModel) removeFromAll(id string) {
	for i, n := range m.all {
		if n.ID == id {
			m.all = append(m.all[:i], m.all[i+1:]...)
			return
		}
	}
}

func (m pickerModel) Init() tea.Cmd { return nil }

// reservedRows is the number of rows kept below the list for the confirm prompt.
const reservedRows = 2

// resizeList sizes the list, reserving space for the confirm prompt when active.
func (m *pickerModel) resizeList() {
	height := m.height
	if m.confirming {
		height -= reservedRows
	}
	m.list.SetSize(m.width, height)
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeList()
		return m, nil

	case openedMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage("Error opening: " + msg.err.Error())
		}
		return m, m.list.NewStatusMessage("Opened " + msg.url)

	case markedMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage("Error: " + msg.err.Error())
		}
		m.removeFromAll(msg.id)
		for i, it := range m.list.Items() {
			if ni, ok := it.(notificationItem); ok && ni.n.ID == msg.id {
				m.list.RemoveItem(i)
				break
			}
		}
		return m, m.list.NewStatusMessage(threadActions[msg.action].past + ": " + msg.title)

	case tea.KeyMsg:
		// Ctrl+C always quits, even while filtering or confirming.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// While a confirmation is pending, capture y/n and nothing else.
		if m.confirming {
			action := m.confirmAction
			targets := m.confirmTargets
			m.confirming = false
			m.confirmTargets = nil
			m.resizeList()
			switch msg.String() {
			case "y", "Y":
				cmds := make([]tea.Cmd, 0, len(targets)+1)
				cmds = append(cmds, m.list.NewStatusMessage("Working…"))
				for _, t := range targets {
					cmds = append(cmds, markCmd(m.doer, t, action))
				}
				return m, tea.Batch(cmds...)
			default:
				return m, m.list.NewStatusMessage("Cancelled")
			}
		}

		// While filtering, let the list consume all other keys.
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "esc":
				// Pop back out of a repository focus, unless a text filter is
				// applied (in which case the list handles esc to clear it).
				if m.focusRepo != "" && m.list.FilterState() == list.Unfiltered {
					m.focusRepo = ""
					return m, m.applyFocus()
				}
			case "f":
				if it, ok := m.list.SelectedItem().(notificationItem); ok {
					m.focusRepo = it.n.Repository.FullName
					return m, m.applyFocus()
				}
			case "enter":
				if it, ok := m.list.SelectedItem().(notificationItem); ok {
					return m, tea.Batch(
						m.list.NewStatusMessage("Opening…"),
						openCmd(m.doer, it.n),
					)
				}
			default:
				if action, bulk, ok := actionForKey(msg.String()); ok {
					var targets []Notification
					if bulk {
						targets = visibleNotifications(m.list)
					} else if it, ok := m.list.SelectedItem().(notificationItem); ok {
						targets = []Notification{it.n}
					}
					if len(targets) > 0 {
						m.confirming = true
						m.confirmTargets = targets
						m.confirmAction = action
						m.resizeList()
						return m, nil
					}
				}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// actionForKey maps a key to a thread action and whether it applies to all
// visible items (bulk). Lowercase keys act on the selected item; uppercase keys
// act on every visible (filtered) item.
func actionForKey(key string) (action string, bulk bool, ok bool) {
	switch key {
	case "r":
		return "read", false, true
	case "d":
		return "done", false, true
	case "u":
		return "unsubscribe", false, true
	case "R":
		return "read", true, true
	case "D":
		return "done", true, true
	case "U":
		return "unsubscribe", true, true
	default:
		return "", false, false
	}
}

// visibleNotifications returns the notifications currently visible in the list,
// reflecting any active filter.
func visibleNotifications(l list.Model) []Notification {
	items := l.VisibleItems()
	out := make([]Notification, 0, len(items))
	for _, it := range items {
		if ni, ok := it.(notificationItem); ok {
			out = append(out, ni.n)
		}
	}
	return out
}

func (m pickerModel) View() string {
	v := m.list.View()
	if m.confirming {
		var prompt string
		if len(m.confirmTargets) == 1 {
			prompt = fmt.Sprintf(threadActions[m.confirmAction].prompt, m.confirmTargets[0].Subject.Title)
		} else {
			prompt = fmt.Sprintf(threadActions[m.confirmAction].confirm, len(m.confirmTargets))
		}
		v += "\n\n" + prompt + " (y/N) "
	}
	return v
}

// selectAndOpen runs the interactive picker, letting the user open notifications
// in the browser and returning to the list after each one. The user exits with
// "q" or Ctrl+C.
func selectAndOpen(doer requestDoer, notifications []Notification) error {
	if len(notifications) == 0 {
		fmt.Println("No unread notifications")
		return nil
	}

	m := newPickerModel(doer, notifications)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
