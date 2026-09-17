package ui

// ApplyBoardHideEmptyColumnsPreference starts the board in Hide Empty mode.
// It is intentionally session-only; the e key continues to cycle the existing
// board modes without persisting a change.
func (m *Model) ApplyBoardHideEmptyColumnsPreference() {
	hideEmpty := false
	m.board.showEmptyColumns = &hideEmpty
	m.board.updateActiveColumns()
}
