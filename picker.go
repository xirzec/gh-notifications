package main

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cli/go-gh/v2/pkg/api"
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

// pickerModel is the Bubble Tea model backing the interactive notification list.
type pickerModel struct {
	list list.Model
	doer requestDoer
}

func newPickerModel(doer requestDoer, notifications []Notification) pickerModel {
	items := make([]list.Item, len(notifications))
	for i, n := range notifications {
		items[i] = notificationItem{n: n}
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Notifications"
	l.SetStatusBarItemName("notification", "notifications")

	return pickerModel{list: l, doer: doer}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil

	case openedMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage("Error opening: " + msg.err.Error())
		}
		return m, m.list.NewStatusMessage("Opened " + msg.url)

	case tea.KeyMsg:
		// Ctrl+C always quits, even while filtering.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// While filtering, let the list consume all other keys.
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "enter":
				if it, ok := m.list.SelectedItem().(notificationItem); ok {
					return m, tea.Batch(
						m.list.NewStatusMessage("Opening…"),
						openCmd(m.doer, it.n),
					)
				}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m pickerModel) View() string {
	return m.list.View()
}

// selectAndOpen runs the interactive picker, letting the user open notifications
// in the browser and returning to the list after each one. The user exits with
// "q" or Ctrl+C.
func selectAndOpen(client *api.RESTClient, notifications []Notification) error {
	if len(notifications) == 0 {
		fmt.Println("No unread notifications")
		return nil
	}

	m := newPickerModel(client, notifications)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
