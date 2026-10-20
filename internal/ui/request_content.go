package ui

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/a-bali/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
)

type previewWork struct {
	token  uint64
	cancel context.CancelFunc
}
type previewMsg struct {
	id      string
	token   uint64
	content gomodel.RequestContent
	err     error
}

func (m *Model) refreshVisibleLogs() {
	selectedID, topID := "", ""
	if m.selected >= 0 && m.selected < len(m.logs) {
		selectedID = m.logs[m.selected].ID
	}
	if m.logOffset >= 0 && m.logOffset < len(m.logs) {
		topID = m.logs[m.logOffset].ID
	}
	if m.allLogs == nil {
		m.allLogs = append([]gomodel.Request(nil), m.logs...)
	}
	m.logs = make([]gomodel.Request, 0, len(m.allLogs))
	cutoff := time.Now().Add(-time.Duration(m.window))
	for _, row := range m.allLogs {
		inWindow := row.Timestamp.IsZero() || !row.Timestamp.Before(cutoff)
		if inWindow && (m.includeMCP || !row.IsMCP()) {
			m.logs = append(m.logs, row)
		}
	}
	for index, row := range m.logs {
		if row.ID == selectedID {
			m.selected = index
		}
		if row.ID == topID {
			m.logOffset = index
		}
	}
	m.selected = min(max(0, m.selected), max(0, len(m.logs)-1))
	m.logOffset = min(max(0, m.logOffset), max(0, len(m.logs)-visibleLogRows(m.height)))
	if m.following {
		m.selected = max(0, len(m.logs)-1)
		m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
	}
	m.rebuildLogIndex()
}

func (m *Model) schedulePreviews() tea.Cmd {
	if m.stopping || m.client == nil || m.width == 0 || m.popup || m.mcpScreen || m.latencyScreen || m.usageScreen {
		m.cancelPreviews()
		return nil
	}
	if m.previewPending == nil {
		m.previewPending = make(map[string]previewWork)
	}
	if m.previewDone == nil {
		m.previewDone = make(map[string]bool)
	}
	if m.previewRetry == nil {
		m.previewRetry = make(map[string]time.Time)
	}
	first := min(max(0, m.logOffset), len(m.logs))
	last := min(len(m.logs), first+visibleLogRows(m.height))
	visible := make(map[string]bool)
	for _, row := range m.logs[first:last] {
		if row.ID != "" && row.ID == logicalRequestID(row.ID) && row.Terminal && !row.IsMCP() && row.LastTurn == "" && row.Error == "" {
			visible[row.ID] = true
		}
	}
	for id, work := range m.previewPending {
		if !visible[id] {
			work.cancel()
			delete(m.previewPending, id)
			delete(m.previewRetry, id)
		}
	}
	var commands []tea.Cmd
	for index := last - 1; index >= first && len(m.previewPending) < 2; index-- {
		row := m.logs[index]
		if !visible[row.ID] || m.previewDone[row.ID] || m.previewPending[row.ID].cancel != nil || time.Since(m.previewRetry[row.ID]) < 30*time.Second {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		m.previewSequence++
		token, id, logID, client := m.previewSequence, row.ID, row.AuditLogID, m.client
		m.previewPending[id] = previewWork{token: token, cancel: cancel}
		m.previewRetry[id] = time.Now()
		commands = append(commands, func() tea.Msg {
			defer cancel()
			event, err := client.RequestDetail(ctx, logID, id)
			var content gomodel.RequestContent
			if err == nil {
				content, err = gomodel.ContentFromDetail(event)
			}
			content.RawJSON = "" // The scrolling log retains only a bounded preview.
			return previewMsg{id: id, token: token, content: content, err: err}
		})
	}
	return tea.Batch(commands...)
}

func (m *Model) receivePreview(msg previewMsg) {
	work, ok := m.previewPending[msg.id]
	if !ok || work.token != msg.token {
		return
	}
	work.cancel()
	delete(m.previewPending, msg.id)
	if msg.err != nil {
		return
	}
	m.previewDone[msg.id] = true
	m.enrichContent(msg.id, msg.content)
}

func (m *Model) enrichContent(id string, content gomodel.RequestContent) {
	m.reducer.EnrichContent(id, content)
	if m.allLogs == nil {
		m.allLogs = append([]gomodel.Request(nil), m.logs...)
	}
	for index := range m.allLogs {
		row := &m.allLogs[index]
		if logicalRequestID(row.ID) != id {
			continue
		}
		if row.AuditLogID == "" {
			row.AuditLogID = content.AuditLogID
		}
		if row.ID == id {
			row.EnrichContent(content)
		}
	}
	m.refreshVisibleLogs()
}

func (m *Model) cancelPreviews() {
	for id, work := range m.previewPending {
		work.cancel()
		delete(m.previewPending, id)
		delete(m.previewRetry, id)
	}
}
func (m *Model) prunePreviewState() {
	active := make(map[string]bool, len(m.allLogs))
	for _, row := range m.allLogs {
		active[logicalRequestID(row.ID)] = true
	}
	for id := range m.previewDone {
		if !active[id] {
			delete(m.previewDone, id)
		}
	}
	for id := range m.previewRetry {
		if !active[id] {
			delete(m.previewRetry, id)
		}
	}
}
func (m *Model) cancelPopupDetail() {
	m.popupGeneration++
	if m.popupCancel != nil {
		m.popupCancel()
		m.popupCancel = nil
	}
}
func (m *Model) stopContentWork() {
	m.stopping = true
	m.cancelPreviews()
	m.cancelPopupDetail()
}
func (m *Model) loadPopupDetail() tea.Cmd {
	if m.client == nil || m.popupRequestID == "" {
		return nil
	}
	m.cancelPopupDetail()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	m.popupCancel = cancel
	m.popupLoading, m.popupLoadError = true, ""
	command := auditDetailCmd(ctx, m.client, m.popupAuditLogID, m.popupRequestID, m.popupGeneration)
	return func() tea.Msg { defer cancel(); return command() }
}
func (m *Model) receivePopupDetail(msg auditDetailMsg) {
	content, err := gomodel.ContentFromDetail(msg.event)
	if err != nil {
		m.popupLoadError = "Could not decode full content. Press R to retry."
		return
	}
	m.enrichContent(msg.requestID, content)
	request := m.popupRecord
	if index, ok := m.logIndex[msg.requestID]; ok {
		request = m.logs[index]
	}
	request.EnrichContent(content)
	request.RawJSON = content.RawJSON
	raw, offset, focused := m.popupRaw, m.popupOffset, m.popupMessage
	previous := m.popupMessages
	m.openPopup(request)
	m.popupRaw = raw
	if len(previous) > 0 {
		for index := range m.popupMessages {
			if index < len(previous) {
				m.popupMessages[index].expanded = previous[index].expanded
			}
		}
		m.popupMessage = min(focused, max(0, len(m.popupMessages)-1))
	}
	m.popupOffset = min(offset, max(0, len(m.popupContentLines())-popupRows(m.height)))
}

func formatTokens(value int, known bool) string {
	if value < 0 || !known && value == 0 {
		return "—"
	}
	text := strconv.Itoa(value)
	for index := len(text) - 3; index > 0; index -= 3 {
		text = text[:index] + "," + text[index:]
	}
	return text
}
func formatCache(value float64, known bool) string {
	if !known && value == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", value*100)
}
