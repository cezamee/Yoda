package main

import (
	"fmt"
	"strings"

	"github.com/cezamee/Yoda/internal/core/pb"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cliModel struct {
	client         pb.PTYShellClient
	input          string
	history        []string
	prompt         string
	lastCommand    string
	commandHistory []string
	historyIndex   int
}

var (
	promptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#569CD6"))
	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DCDCAA"))

	welcomeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4FC1FF"))

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F44747"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CE9178"))
)

func newCLIModel(client pb.PTYShellClient) cliModel {
	return cliModel{
		client:         client,
		prompt:         "yoda",
		history:        []string{},
		lastCommand:    "",
		commandHistory: []string{},
		historyIndex:   -1,
	}
}

func (m cliModel) Init() tea.Cmd {
	return nil
}

func (m cliModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if strings.TrimSpace(m.input) == "" {
				return m, nil
			}
			return m.handleCommand()
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case "up":
			if len(m.commandHistory) > 0 {
				if m.historyIndex == -1 {
					m.historyIndex = len(m.commandHistory) - 1
				} else if m.historyIndex > 0 {
					m.historyIndex--
				}
				if m.historyIndex >= 0 && m.historyIndex < len(m.commandHistory) {
					m.input = m.commandHistory[m.historyIndex]
				}
			}
		case "down":
			if len(m.commandHistory) > 0 && m.historyIndex != -1 {
				if m.historyIndex < len(m.commandHistory)-1 {
					m.historyIndex++
					m.input = m.commandHistory[m.historyIndex]
				} else {
					m.historyIndex = -1
					m.input = ""
				}
			}
		case "tab":
			if m.input == "" {
				m.input = "shell"
			} else {
				commands := []string{"shell", "help", "clear", "exit", "quit"}
				var matches []string

				for _, cmd := range commands {
					if strings.HasPrefix(cmd, m.input) {
						matches = append(matches, cmd)
					}
				}

				if len(matches) == 1 {
					m.input = matches[0]
				} else if len(matches) > 1 {
					common := matches[0]
					for _, match := range matches[1:] {
						for i := 0; i < len(common) && i < len(match); i++ {
							if common[i] != match[i] {
								common = common[:i]
								break
							}
						}
					}
					if len(common) > len(m.input) {
						m.input = common
					}
				}
			}
		default:
			if len(msg.String()) == 1 {
				m.input += msg.String()
				m.historyIndex = -1
			}
		}
	}
	return m, nil
}

func (m cliModel) handleCommand() (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(m.input)
	m.lastCommand = command

	if command != "" && (len(m.commandHistory) == 0 || m.commandHistory[len(m.commandHistory)-1] != command) {
		m.commandHistory = append(m.commandHistory, command)
		if len(m.commandHistory) > 50 {
			m.commandHistory = m.commandHistory[1:]
		}
	}

	m.input = ""
	m.historyIndex = -1

	switch command {
	case "shell":
		return m, tea.Quit
	case "exit", "quit", "q":
		return m, tea.Quit
	case "help", "?":
		m.history = append(m.history, welcomeStyle.Render("📖 Available commands:"))
		m.history = append(m.history, inputStyle.Render("  shell")+helpStyle.Render("    - Start interactive shell session"))
		m.history = append(m.history, inputStyle.Render("  help")+helpStyle.Render("     - Show this help"))
		m.history = append(m.history, inputStyle.Render("  clear")+helpStyle.Render("    - Clear screen"))
		m.history = append(m.history, inputStyle.Render("  exit")+helpStyle.Render("     - Exit the CLI"))
		m.history = append(m.history, helpStyle.Render(""))
		m.history = append(m.history, promptStyle.Render("💡 Tip: Use TAB for auto-completion, ↑↓ for history"))
	case "clear":
		m.history = []string{}
	default:
		m.history = append(m.history, errorStyle.Render(fmt.Sprintf("Unknown command: %s (try 'help')", command)))
	}

	return m, nil
}

func (m cliModel) View() string {
	var s strings.Builder

	s.WriteString(welcomeStyle.Render("🚀 Yoda Remote CLI") + "\n")
	s.WriteString(helpStyle.Render("Type ") + inputStyle.Render("'help'") + helpStyle.Render(" for commands, ") +
		inputStyle.Render("'shell'") + helpStyle.Render(" to start, ") +
		inputStyle.Render("'exit'") + helpStyle.Render(" to quit\n\n"))

	start := 0
	if len(m.history) > 12 {
		start = len(m.history) - 12
	}
	for i := start; i < len(m.history); i++ {
		s.WriteString(m.history[i] + "\n")
	}
	s.WriteString("\n")
	s.WriteString(promptStyle.Render(fmt.Sprintf("%s> ", m.prompt)))
	s.WriteString(inputStyle.Render(m.input))
	s.WriteString("█")

	return s.String()
}

func RunCLI(client pb.PTYShellClient) error {
	for {
		model := newCLIModel(client)

		p := tea.NewProgram(
			model,
			tea.WithAltScreen(),
		)

		finalModel, err := p.Run()
		if err != nil {
			return err
		}

		if m, ok := finalModel.(cliModel); ok {
			if m.lastCommand == "shell" {
				fmt.Print("\033[2J\033[H")
				runShellSession(client)
				continue
			}
		}
		break
	}

	return nil
}
