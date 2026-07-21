// Package dash is the always-on clubhouse dashboard: a live view of who's in
// the room, what files are locked, shared project memory, and a scrolling feed
// of activity.
package dash

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"clubhouse/internal/client"
	"clubhouse/internal/config"
	"clubhouse/internal/room"
	clubsession "clubhouse/internal/session"
)

func Run() error {
	cfg := config.LoadOrDefault()
	token := config.ReadToken()
	if cfg.Server == "" || token == "" {
		fmt.Println("not in a clubhouse - run: clubhouse enter rc://join/...")
		return nil
	}
	cfg.EnsureName()
	c := client.New(cfg.Server, token)
	if err := joinLounge(c, cfg.Name); err != nil {
		return err
	}
	_, err := tea.NewProgram(newModel(c), tea.WithAltScreen()).Run()
	return err
}

func joinLounge(c *client.Client, name string) error {
	_, err := clubsession.JoinOrResume(c, name, "watching lounge")
	return err
}

type dataMsg struct {
	room   room.Room
	invite string
}
type errMsg struct{ s string }
type tickMsg struct{}
type flashMsg struct{}

type viewMode int

const (
	modeOverview viewMode = iota
	modeLocks
	modeMemory
	modeFeed
)

var modeLabels = []string{"overview", "locks", "memory", "feed"}

const (
	minWindowWidth  = 80
	minWindowHeight = 24
)

type keyMap struct {
	Quit    key.Binding
	Tab     key.Binding
	BackTab key.Binding
	Search  key.Binding
	Clear   key.Binding
	Refresh key.Binding
	Help    key.Binding
	Esc     key.Binding
	Enter   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Search, k.Clear, k.Refresh, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab, k.BackTab, k.Search, k.Clear, k.Refresh},
		{k.Enter, k.Esc, k.Help, k.Quit},
	}
}

type model struct {
	client *client.Client
	invite string
	room   room.Room

	sp       spinner.Model
	locks    table.Model
	mem      viewport.Model
	feedVP   viewport.Model
	help     help.Model
	search   textinput.Model
	progress progress.Model
	uptime   stopwatch.Model
	keys     keyMap

	mode viewMode
	err  string
	w, h int

	flashUntil  time.Time // header flashes red while now < this
	lastBlocked time.Time // most recent blocked event we've seen
	lastJoined  time.Time
	joinName    string
	seeded      bool
}

func newModel(c *client.Client) model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = accent

	t := table.New(
		table.WithColumns(lockColumns(48)),
		table.WithFocused(false),
		table.WithHeight(8),
	)
	st := table.DefaultStyles()
	st.Header = st.Header.Foreground(accentBlue).Bold(true).BorderBottom(true).BorderForeground(borderColor)
	st.Cell = st.Cell.Foreground(textColor).Padding(0, 1)
	st.Selected = st.Selected.Foreground(accentCoral).Bold(true)
	t.SetStyles(st)

	search := textinput.New()
	search.Prompt = "filter "
	search.Placeholder = "name, file, status, memory"
	search.PromptStyle = accentStyle
	search.PlaceholderStyle = dimStyle
	search.TextStyle = textStyle
	search.CharLimit = 80
	search.Width = 38
	search.SetSuggestions([]string{"blocked", "locked", "released", "editing", "memory"})

	return model{
		client:   c,
		sp:       sp,
		locks:    t,
		mem:      viewport.New(40, 8),
		feedVP:   viewport.New(40, 8),
		help:     help.New(),
		search:   search,
		progress: progress.New(progress.WithScaledGradient("#7BD88F", "#FF6B6B"), progress.WithoutPercentage()),
		uptime:   stopwatch.NewWithInterval(time.Second),
		keys: keyMap{
			Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
			Tab:     key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab", "next view")),
			BackTab: key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab", "prev view")),
			Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			Clear:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "clear filter")),
			Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
			Esc:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "blur")),
			Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetch(), m.sp.Tick, m.uptime.Init(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(1200*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) fetch() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		r, err := c.Heartbeat("watching lounge")
		if err != nil {
			return errMsg{err.Error()}
		}
		invite, _ := c.Invite()
		return dataMsg{room: r, invite: invite}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	m.uptime, cmd = m.uptime.Update(msg)
	cmds = append(cmds, cmd)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resize()
		m.refresh()
		return m, tea.Batch(cmds...)
	case tea.KeyMsg:
		if m.search.Focused() {
			switch {
			case key.Matches(msg, m.keys.Esc), key.Matches(msg, m.keys.Enter):
				m.search.Blur()
				m.refresh()
			default:
				m.search, cmd = m.search.Update(msg)
				cmds = append(cmds, cmd)
				m.refresh()
			}
			return m, tea.Batch(cmds...)
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Tab):
			m.mode = (m.mode + 1) % viewMode(len(modeLabels))
		case key.Matches(msg, m.keys.BackTab):
			m.mode = (m.mode + viewMode(len(modeLabels)) - 1) % viewMode(len(modeLabels))
		case key.Matches(msg, m.keys.Search):
			cmds = append(cmds, m.search.Focus())
		case key.Matches(msg, m.keys.Clear):
			m.search.SetValue("")
			m.refresh()
		case key.Matches(msg, m.keys.Refresh):
			cmds = append(cmds, m.fetch())
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		}
	case tickMsg:
		return m, tea.Batch(append(cmds, m.fetch(), tick())...)
	case dataMsg:
		m.room, m.invite, m.err = msg.room, msg.invite, ""
		if jt, name := latestJoined(m.room.Events); !jt.IsZero() {
			if m.seeded && jt.After(m.lastJoined) {
				m.joinName = name
			}
			m.lastJoined = jt
		}
		if b := latestBlocked(m.room.Events); !b.IsZero() {
			if m.seeded && b.After(m.lastBlocked) {
				m.flashUntil = time.Now().Add(1300 * time.Millisecond)
				cmds = append(cmds, tea.Tick(1300*time.Millisecond, func(time.Time) tea.Msg { return flashMsg{} }))
			}
			m.lastBlocked = b
		}
		m.seeded = true
		m.refresh()
	case flashMsg:
		return m, tea.Batch(cmds...)
	case errMsg:
		m.err = msg.s
	case spinner.TickMsg:
		m.sp, cmd = m.sp.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.mode == modeMemory {
		m.mem, cmd = m.mem.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.mode == modeFeed {
		m.feedVP, cmd = m.feedVP.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) resize() {
	inner := m.innerW()
	m.help.Width = inner
	m.search.Width = clamp(inner-12, 24, 64)
	m.progress.Width = clamp(inner/4, 12, 24)
	left, right := m.columns()
	bodyH := m.bodyH()
	m.mem.Width = max(20, left-4)
	m.mem.Height = max(4, bodyH/2-3)
	m.feedVP.Width = max(20, right-4)
	m.feedVP.Height = max(5, bodyH/2-3)
	m.locks.SetColumns(lockColumns(max(32, right-4)))
	m.locks.SetHeight(max(4, bodyH/2-4))
}

func (m *model) refresh() {
	m.locks.SetRows(m.lockRows())
	m.mem.SetContent(m.memoryText())
	m.feedVP.SetContent(m.feedText(max(20, m.feedVP.Width)))
}

// ---- layout ----

func (m model) View() string {
	if m.w == 0 {
		return "starting..."
	}
	if m.tooSmall() {
		return m.minimumSizeView()
	}
	inner := m.innerW()
	return shell.Width(inner).Render(lipgloss.JoinVertical(
		lipgloss.Left,
		m.header(),
		m.tabs(),
		m.filterBar(),
		m.body(),
		m.footer(),
	))
}

func (m model) tooSmall() bool {
	return m.w < minWindowWidth || m.h < minWindowHeight
}

func (m model) minimumSizeView() string {
	msg := fmt.Sprintf("clubhouse: resize to %dx%d", minWindowWidth, minWindowHeight)
	if m.w < 42 || m.h < 8 {
		return trunc(msg, max(1, m.w))
	}
	width := clamp(m.w-10, 32, 56)
	body := strings.Join([]string{
		sectionTitle.Render("clubhouse lounge"),
		"",
		fmt.Sprintf("minimum window: %dx%d", minWindowWidth, minWindowHeight),
		fmt.Sprintf("current window: %dx%d", m.w, m.h),
		dimStyle.Render("resize the terminal to enter the lounge"),
	}, "\n")
	card := panel.Width(width).Render(body)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, card)
}

func (m model) header() string {
	hdr := headerBar
	title := fmt.Sprintf("clubhouse  |  %s  |  %d online", orDash(m.room.Name), m.onlineCount())
	if time.Now().Before(m.flashUntil) {
		hdr = flashBar
		title = "blocked edit  |  " + title
	} else if m.joinName != "" {
		title += "  |  latest join: " + m.joinName
	}
	inner := m.innerW()
	titleLine := hdr.Width(inner).Render(title)
	return lipgloss.JoinVertical(lipgloss.Left, titleLine, m.metricsLine())
}

func (m model) tabs() string {
	tabs := make([]string, 0, len(modeLabels))
	for i, label := range modeLabels {
		style := tabStyle
		if viewMode(i) == m.mode {
			style = activeTabStyle
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, tabs...)
}

func (m model) filterBar() string {
	inner := m.innerW()
	if m.search.Focused() {
		return filterActive.Width(inner).Render(m.search.View())
	}
	q := strings.TrimSpace(m.search.Value())
	if q == "" {
		return filterIdle.Width(inner).Render("filter: all activity")
	}
	return filterActive.Width(inner).Render("filter: " + q)
}

func (m model) body() string {
	switch m.mode {
	case modeLocks:
		return m.locksFull()
	case modeMemory:
		return m.memoryFull()
	case modeFeed:
		return m.feedFull()
	default:
		return m.overview()
	}
}

func (m model) overview() string {
	inner := m.innerW()
	bodyH := m.bodyH()
	gap := "\n"
	if inner < 96 {
		w := max(20, inner-4)
		return lipgloss.JoinVertical(lipgloss.Left,
			simplePanel("PEOPLE", m.presence(w-4), w, 6),
			simplePanel("LOCKS", m.compactLocks(w-4, 4), w, 7),
			simplePanel("EVENTS", m.feedText(w-4), w, max(7, bodyH-15)),
			simplePanel("MEMORY", m.memoryBrief(w-4, 3), w, 6),
		)
	}
	leftW := clamp(inner/2-2, 38, 58)
	rightW := max(36, inner-leftW-2)
	left := lipgloss.JoinVertical(lipgloss.Left,
		simplePanel("PEOPLE", m.presence(leftW-4), leftW, 8),
		simplePanel("MEMORY", m.memoryBrief(leftW-4, 6), leftW, max(7, bodyH-10)),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		simplePanel("LOCKS", m.compactLocks(rightW-4, 6), rightW, 8),
		simplePanel("EVENTS", m.feedText(rightW-4), rightW, max(7, bodyH-10)),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, gap, right)
}

func (m model) locksFull() string {
	return panel.Width(max(20, m.innerW()-4)).Height(m.bodyH()).Render(
		sectionTitle.Render("LIVE ACTIVITY") + "\n" +
			m.activitySummary() + "\n\n" +
			m.locks.View(),
	)
}

func (m model) memoryFull() string {
	return panel.Width(max(20, m.innerW()-4)).Height(m.bodyH()).Render(
		sectionTitle.Render("PROJECT MEMORY") + "\n" +
			dimStyle.Render("shared notes from the team") + "\n\n" +
			m.mem.View(),
	)
}

func (m model) feedFull() string {
	return panel.Width(max(20, m.innerW()-4)).Height(m.bodyH()).Render(
		sectionTitle.Render("EVENT FEED") + "\n" +
			dimStyle.Render("newest events at the bottom") + "\n\n" +
			m.feedVP.View(),
	)
}

func (m model) footer() string {
	invite := footStyle.Render(trunc("invite > "+m.invite, m.innerW()))
	if m.err != "" {
		invite = errStyle.Render(trunc("offline · "+m.err, m.innerW()))
	}
	helpView := m.help.View(m.keys)
	return footerStyle.Width(m.innerW()).Render(lipgloss.JoinVertical(lipgloss.Left, invite, helpView))
}

func (m model) columns() (left, right int) {
	inner := m.innerW()
	if inner < 96 {
		return inner, inner
	}
	left = clamp(inner/2-1, 34, 52)
	right = max(34, inner-left-2)
	return left, right
}

func (m model) bodyH() int {
	used := 9
	if m.help.ShowAll {
		used = 12
	}
	return max(12, m.h-used)
}

func (m model) metricsLine() string {
	blocked := m.countEvents("blocked")
	line := fmt.Sprintf("%d online  locks %d  joins %d  blocked %d  uptime %s  pulse %s",
		m.onlineCount(), len(m.room.Locks), m.countEvents("joined"), blocked, m.uptime.View(), m.sp.View())
	return dimStyle.Render(trunc(line, m.innerW()))
}

func (m model) innerW() int {
	return max(40, m.w-4)
}

// ---- content ----

func (m model) presence(width int) string {
	var b strings.Builder
	mem := m.filteredMembers()
	if len(mem) == 0 {
		b.WriteString(dimStyle.Render("no one online yet"))
	}
	for _, x := range mem {
		dot := offlineDot
		state := "away"
		if online(x) {
			dot = onlineDot
			state = "online"
		}
		line := dot.Render("*") + " " + nameStyle.Render(x.Name) + " " + dimStyle.Render(state)
		b.WriteString(trunc(line, width) + "\n")
		if x.Status != "" && online(x) {
			b.WriteString("  " + dimStyle.Render(trunc(x.Status, width-2)) + "\n")
		}
		if git := gitLine(x.Git); git != "" {
			b.WriteString("  " + dimStyle.Render(trunc(git, width-2)) + "\n")
		}
	}
	return b.String()
}

func (m model) activityBody() string {
	if len(m.lockRows()) == 0 {
		return dimStyle.Render("no files locked")
	}
	return m.locks.View()
}

func (m model) activitySummary() string {
	if len(m.room.Locks) == 0 {
		return dimStyle.Render("no active file claims")
	}
	return fmt.Sprintf("%s active claims · %s blocked events", metricStyle.Render(fmt.Sprint(len(m.room.Locks))), errStyle.Render(fmt.Sprint(m.countEvents("blocked"))))
}

func (m model) lockRows() []table.Row {
	locks := make([]room.Lock, 0, len(m.room.Locks))
	for _, l := range m.room.Locks {
		if m.matches(l.Path, l.Reason, memberName(m.room, l.Member)) {
			locks = append(locks, l)
		}
	}
	sort.Slice(locks, func(i, j int) bool { return locks[i].Path < locks[j].Path })
	rows := make([]table.Row, 0, len(locks))
	for _, l := range locks {
		rows = append(rows, table.Row{
			memberName(m.room, l.Member),
			l.Path,
			age(l.Since),
			firstNonEmpty(l.Reason, "editing"),
		})
	}
	return rows
}

func (m model) feedText(width int) string {
	ev := m.filteredEvents()
	maxRows := max(1, m.bodyH()/2-2)
	if m.mode == modeFeed {
		maxRows = max(1, m.bodyH()-5)
	}
	if len(ev) > maxRows {
		ev = ev[len(ev)-maxRows:]
	}
	if len(ev) == 0 {
		return dimStyle.Render("waiting for activity...")
	}
	var b strings.Builder
	for _, e := range ev {
		b.WriteString(eventLine(e, width) + "\n")
	}
	return b.String()
}

func eventLine(e room.Event, w int) string {
	icon, style := "·", dimStyle
	switch e.Kind {
	case "joined":
		icon, style = "join", onlineDot
	case "locked":
		icon, style = "lock", lockStyle
	case "released":
		icon, style = "done", dimStyle
	case "blocked":
		icon, style = "blocked", errStyle
	}
	line := fmt.Sprintf("%s %s %s %s", dimStyle.Render(e.At.Format("15:04")), badgeStyle.Render(icon), nameStyle.Render(e.Actor), style.Render(verb(e)+" "+e.Detail))
	return trunc(line, w)
}

func (m model) compactLocks(width, rows int) string {
	lockRows := m.lockRows()
	if len(lockRows) == 0 {
		return dimStyle.Render("no files locked") + "\n"
	}
	if len(lockRows) > rows {
		lockRows = lockRows[:rows]
	}
	var b strings.Builder
	for _, r := range lockRows {
		line := fmt.Sprintf("%s has %s open (%s)", r[0], r[1], r[3])
		b.WriteString(trunc(line, width) + "\n")
	}
	return b.String()
}

func simplePanel(title, body string, width, height int) string {
	return panel.Width(max(20, width-4)).Height(max(4, height)).Render(sectionTitle.Render(title) + "\n" + body)
}

func (m model) memoryBrief(width, rows int) string {
	notes := m.filteredNotes()
	if len(notes) == 0 {
		return dimStyle.Render("nothing pinned behind the bar")
	}
	if len(notes) > rows {
		notes = notes[len(notes)-rows:]
	}
	var b strings.Builder
	for _, n := range notes {
		line := n.Text
		if n.Author != "" {
			line += " - " + n.Author
		}
		b.WriteString(trunc(line, width) + "\n")
	}
	return b.String()
}

func (m model) lastJoinName() string {
	for i := len(m.room.Events) - 1; i >= 0; i-- {
		if m.room.Events[i].Kind == "joined" {
			return m.room.Events[i].Actor
		}
	}
	return ""
}

func (m model) memoryText() string {
	notes := m.filteredNotes()
	if len(notes) == 0 {
		return dimStyle.Render("nothing recorded yet")
	}
	var b strings.Builder
	for _, n := range notes {
		line := fmt.Sprintf("%s %s", accent.Render("•"), n.Text)
		if n.Author != "" {
			line += dimStyle.Render(" — " + n.Author)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m model) filteredMembers() []room.Member {
	out := membersSorted(m.room)
	if strings.TrimSpace(m.search.Value()) == "" {
		return out
	}
	filtered := out[:0]
	for _, x := range out {
		if m.matches(x.Name, x.Status, x.Git.Summary, x.Git.Branch) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}

func (m model) filteredNotes() []room.Note {
	if strings.TrimSpace(m.search.Value()) == "" {
		return m.room.Notes
	}
	var out []room.Note
	for _, n := range m.room.Notes {
		if m.matches(n.Author, n.Text) {
			out = append(out, n)
		}
	}
	return out
}

func (m model) filteredEvents() []room.Event {
	if strings.TrimSpace(m.search.Value()) == "" {
		return m.room.Events
	}
	var out []room.Event
	for _, e := range m.room.Events {
		if m.matches(e.Kind, e.Actor, e.Detail) {
			out = append(out, e)
		}
	}
	return out
}

func (m model) matches(values ...string) bool {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	if q == "" {
		return true
	}
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

func lockColumns(width int) []table.Column {
	fileW := clamp(width-32, 16, 70)
	return []table.Column{
		{Title: "who", Width: 14},
		{Title: "editing", Width: fileW},
		{Title: "age", Width: 7},
		{Title: "why", Width: 12},
	}
}

// ---- room helpers ----

func verb(e room.Event) string {
	switch e.Kind {
	case "joined":
		return "entered the lounge"
	case "locked":
		return "opened a tab for"
	case "released":
		return "closed the tab for"
	case "blocked":
		return "was blocked on"
	}
	return e.Kind
}

func latestJoined(events []room.Event) (time.Time, string) {
	var (
		t    time.Time
		name string
	)
	for _, e := range events {
		if e.Kind == "joined" && e.At.After(t) {
			t = e.At
			name = e.Actor
		}
	}
	return t, name
}

func latestBlocked(events []room.Event) time.Time {
	var t time.Time
	for _, e := range events {
		if e.Kind == "blocked" && e.At.After(t) {
			t = e.At
		}
	}
	return t
}

func (m model) onlineCount() int {
	n := 0
	for _, x := range m.room.Members {
		if online(x) {
			n++
		}
	}
	return n
}

func (m model) countEvents(kind string) int {
	n := 0
	for _, e := range m.room.Events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func membersSorted(r room.Room) []room.Member {
	out := make([]room.Member, 0, len(r.Members))
	for _, x := range r.Members {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool {
		if online(out[i]) != online(out[j]) {
			return online(out[i])
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func online(m room.Member) bool { return time.Since(m.LastSeen) < 10*time.Second }

func gitLine(g room.GitStatus) string {
	if g.Summary != "" {
		return "git: " + g.Summary
	}
	if g.Branch == "" && g.Dirty == 0 && g.Ahead == 0 && g.Behind == 0 {
		return ""
	}
	var parts []string
	if g.Branch != "" {
		parts = append(parts, g.Branch)
	}
	if g.Dirty == 0 {
		parts = append(parts, "clean")
	} else {
		parts = append(parts, fmt.Sprintf("%d changed", g.Dirty))
	}
	if g.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("ahead %d", g.Ahead))
	}
	if g.Behind > 0 {
		parts = append(parts, fmt.Sprintf("behind %d", g.Behind))
	}
	return "git: " + strings.Join(parts, " | ")
}

func memberName(r room.Room, id string) string {
	if x, ok := r.Members[id]; ok && x.Name != "" {
		return x.Name
	}
	return "someone"
}

func age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func trunc(s string, n int) string {
	if n < 1 {
		n = 1
	}
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func orDash(s string) string {
	if s == "" {
		return "clubhouse"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func centerASCII(s string, width int) string {
	if ansi.StringWidth(s) >= width {
		return trunc(s, width)
	}
	pad := (width - ansi.StringWidth(s)) / 2
	return strings.Repeat(" ", pad) + s
}
