package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/shlex"

	"github.com/gastownhall/gascity/internal/sessionlog"
	"github.com/gastownhall/gascity/internal/worker"
)

const sessionStructuredSchemaVersion = "session.structured.v1"

const (
	structuredTranscriptUnavailableCode    = "transcript_unavailable"
	structuredTranscriptUnavailableMessage = "provider transcript is unavailable; using provider-neutral text fallback"
)

// SessionStreamStructuredMessageEvent carries provider-normalized structured
// transcript messages on the session SSE stream.
type SessionStreamStructuredMessageEvent struct {
	ID                 string                     `json:"id"`
	Template           string                     `json:"template"`
	Provider           string                     `json:"provider" doc:"Producing provider identifier (claude, codex, gemini, open-code, etc.)."`
	Format             string                     `json:"format" doc:"Always structured for this event."`
	SchemaVersion      string                     `json:"schema_version" doc:"Structured session transcript schema version."`
	History            *SessionStructuredHistory  `json:"history,omitempty" doc:"Normalized worker-history envelope for this snapshot or stream batch."`
	StructuredMessages []SessionStructuredMessage `json:"structured_messages" doc:"Provider-normalized structured messages."`
	Pagination         *sessionlog.PaginationInfo `json:"pagination,omitempty"`
}

// SessionStructuredHistory is the normalized worker-history envelope projected
// onto the session transcript API.
type SessionStructuredHistory struct {
	GCSessionID           string                        `json:"gc_session_id,omitempty"`
	LogicalConversationID string                        `json:"logical_conversation_id,omitempty"`
	ProviderSessionID     string                        `json:"provider_session_id,omitempty"`
	TranscriptStreamID    string                        `json:"transcript_stream_id"`
	Generation            SessionStructuredGeneration   `json:"generation"`
	Cursor                SessionStructuredCursor       `json:"cursor"`
	Continuity            SessionStructuredContinuity   `json:"continuity"`
	TailState             SessionStructuredTailState    `json:"tail_state"`
	Diagnostics           []SessionStructuredDiagnostic `json:"diagnostics,omitempty"`
}

// SessionStructuredGeneration identifies a raw transcript stream instance.
type SessionStructuredGeneration struct {
	ID         string `json:"id"`
	ObservedAt string `json:"observed_at,omitempty"`
}

// SessionStructuredCursor identifies the normalized transcript tip.
type SessionStructuredCursor struct {
	AfterEntryID string `json:"after_entry_id,omitempty"`
}

// SessionStructuredContinuity describes compaction/branch evidence.
type SessionStructuredContinuity struct {
	Status          string `json:"status"`
	CompactionCount int    `json:"compaction_count,omitempty"`
	HasBranches     bool   `json:"has_branches,omitempty"`
	Note            string `json:"note,omitempty"`
}

// SessionStructuredTailState captures the current transcript tail state.
type SessionStructuredTailState struct {
	Activity              string   `json:"activity"`
	LastEntryID           string   `json:"last_entry_id,omitempty"`
	OpenToolCallIDs       []string `json:"open_tool_call_ids,omitempty"`
	PendingInteractionIDs []string `json:"pending_interaction_ids,omitempty"`
	Degraded              bool     `json:"degraded,omitempty"`
	DegradedReason        string   `json:"degraded_reason,omitempty"`
}

// SessionStructuredDiagnostic records normalized-history diagnostics.
type SessionStructuredDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	Count   int    `json:"count,omitempty"`
}

// SessionStructuredMessage is one provider-normalized transcript message.
type SessionStructuredMessage struct {
	ID               string                   `json:"id"`
	Role             string                   `json:"role"`
	Provider         string                   `json:"provider,omitempty"`
	Timestamp        string                   `json:"timestamp,omitempty"`
	Model            string                   `json:"model,omitempty"`
	StopReason       string                   `json:"stop_reason,omitempty"`
	Status           string                   `json:"status"`
	IsSubagent       bool                     `json:"is_subagent,omitempty"`
	ParentToolCallID string                   `json:"parent_tool_call_id,omitempty"`
	Blocks           []SessionStructuredBlock `json:"blocks"`
}

// SessionStructuredBlock is one structured content/tool/interaction block.
type SessionStructuredBlock struct {
	Type        string                        `json:"type"`
	Text        string                        `json:"text,omitempty"`
	Thinking    string                        `json:"thinking,omitempty"`
	Signature   string                        `json:"signature,omitempty"`
	ID          string                        `json:"id,omitempty"`
	ToolCallID  string                        `json:"tool_call_id,omitempty"`
	Name        string                        `json:"name,omitempty"`
	Input       *SessionStructuredToolInput   `json:"input,omitempty"`
	Content     string                        `json:"content,omitempty"`
	IsError     bool                          `json:"is_error,omitempty"`
	Structured  *SessionStructuredToolResult  `json:"structured,omitempty"`
	Interaction *SessionStructuredInteraction `json:"interaction,omitempty"`
}

// SessionStructuredToolInput is a provider-neutral projection of a tool call's
// input. Provider-native input JSON is available only through format=raw.
type SessionStructuredToolInput struct {
	Kind      string                      `json:"kind,omitempty" doc:"Provider-neutral input kind such as command, code, patch, search, file, arguments, or text."`
	Text      string                      `json:"text,omitempty"`
	Command   string                      `json:"command,omitempty"`
	Code      string                      `json:"code,omitempty"`
	Patch     string                      `json:"patch,omitempty"`
	FilePath  string                      `json:"file_path,omitempty"`
	Query     string                      `json:"query,omitempty"`
	Pattern   string                      `json:"pattern,omitempty"`
	Arguments []SessionStructuredArgument `json:"arguments,omitempty"`
}

// SessionStructuredArgument is one provider-neutral string argument.
type SessionStructuredArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SessionStructuredToolResult is a typed structured tool-result projection.
// The Kind field discriminates which fields are populated.
type SessionStructuredToolResult struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text,omitempty"`
	Stdout      string   `json:"stdout,omitempty"`
	Stderr      string   `json:"stderr,omitempty"`
	ExitCode    *int     `json:"exit_code,omitempty"`
	Interrupted bool     `json:"interrupted,omitempty"`
	Truncated   bool     `json:"truncated,omitempty"`
	IsImage     bool     `json:"is_image,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Filenames   []string `json:"filenames,omitempty"`
	NumFiles    int      `json:"num_files,omitempty"`
	Content     string   `json:"content,omitempty"`
	NumLines    int      `json:"num_lines,omitempty"`
	FilePath    string   `json:"file_path,omitempty"`
	Code        string   `json:"code,omitempty"`
	Patch       string   `json:"patch,omitempty"`
	StartLine   int      `json:"start_line,omitempty"`
	TotalLines  int      `json:"total_lines,omitempty"`
}

// SessionStructuredInteraction is a provider-neutral required interaction
// embedded in normalized history.
type SessionStructuredInteraction struct {
	RequestID string   `json:"request_id,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	State     string   `json:"state"`
	Prompt    string   `json:"prompt,omitempty"`
	Options   []string `json:"options,omitempty"`
	Action    string   `json:"action,omitempty"`
}

type structuredToolContext struct {
	Name  string
	Input *SessionStructuredToolInput
}

func structuredHistoryFromSnapshot(snapshot *worker.HistorySnapshot) *SessionStructuredHistory {
	if snapshot == nil {
		return nil
	}
	diagnostics := make([]SessionStructuredDiagnostic, 0, len(snapshot.Diagnostics))
	for _, d := range snapshot.Diagnostics {
		diagnostics = append(diagnostics, SessionStructuredDiagnostic{
			Code:    d.Code,
			Message: d.Message,
			Count:   d.Count,
		})
	}
	return &SessionStructuredHistory{
		GCSessionID:           snapshot.GCSessionID,
		LogicalConversationID: snapshot.LogicalConversationID,
		ProviderSessionID:     snapshot.ProviderSessionID,
		TranscriptStreamID:    snapshot.TranscriptStreamID,
		Generation: SessionStructuredGeneration{
			ID:         snapshot.Generation.ID,
			ObservedAt: formatOptionalTime(snapshot.Generation.ObservedAt),
		},
		Cursor: SessionStructuredCursor{
			AfterEntryID: snapshot.Cursor.AfterEntryID,
		},
		Continuity: SessionStructuredContinuity{
			Status:          string(snapshot.Continuity.Status),
			CompactionCount: snapshot.Continuity.CompactionCount,
			HasBranches:     snapshot.Continuity.HasBranches,
			Note:            snapshot.Continuity.Note,
		},
		TailState: SessionStructuredTailState{
			Activity:              string(snapshot.TailState.Activity),
			LastEntryID:           snapshot.TailState.LastEntryID,
			OpenToolCallIDs:       append([]string(nil), snapshot.TailState.OpenToolUseIDs...),
			PendingInteractionIDs: append([]string(nil), snapshot.TailState.PendingInteractionIDs...),
			Degraded:              snapshot.TailState.Degraded,
			DegradedReason:        snapshot.TailState.DegradedReason,
		},
		Diagnostics: diagnostics,
	}
}

func structuredFallbackHistory(sessionID, providerSessionID, activity string) *SessionStructuredHistory {
	now := time.Now().UTC()
	if sessionID == "" {
		sessionID = "unknown"
	}
	if providerSessionID == "" {
		providerSessionID = sessionID
	}
	if activity == "" {
		activity = string(worker.TailActivityUnknown)
	}
	streamID := "fallback:" + sessionID
	return &SessionStructuredHistory{
		GCSessionID:           sessionID,
		LogicalConversationID: sessionID,
		ProviderSessionID:     providerSessionID,
		TranscriptStreamID:    streamID,
		Generation: SessionStructuredGeneration{
			ID:         streamID,
			ObservedAt: now.Format(time.RFC3339Nano),
		},
		Continuity: SessionStructuredContinuity{
			Status: string(worker.ContinuityStatusDegraded),
			Note:   structuredTranscriptUnavailableMessage,
		},
		TailState: SessionStructuredTailState{
			Activity:       activity,
			Degraded:       true,
			DegradedReason: structuredTranscriptUnavailableMessage,
		},
		Diagnostics: []SessionStructuredDiagnostic{{
			Code:    structuredTranscriptUnavailableCode,
			Message: structuredTranscriptUnavailableMessage,
			Count:   1,
		}},
	}
}

func structuredFallbackMessages(sessionID, provider, text string) []SessionStructuredMessage {
	if strings.TrimSpace(text) == "" {
		return []SessionStructuredMessage{}
	}
	if sessionID == "" {
		sessionID = "unknown"
	}
	return []SessionStructuredMessage{{
		ID:       "fallback:" + sessionID + ":1",
		Role:     "output",
		Provider: provider,
		Status:   string(worker.ContinuityStatusDegraded),
		Blocks: []SessionStructuredBlock{{
			Type: string(worker.BlockKindText),
			Text: text,
		}},
	}}
}

func historySnapshotStructuredMessages(snapshot *worker.HistorySnapshot, includeThinking bool) ([]SessionStructuredMessage, []string) {
	if snapshot == nil {
		return nil, nil
	}
	toolContexts := structuredToolContexts(snapshot.Entries)
	messages := make([]SessionStructuredMessage, 0, len(snapshot.Entries))
	ids := make([]string, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		msg := historyEntryToStructuredMessage(entry, includeThinking, toolContexts)
		if len(msg.Blocks) == 0 && msg.Role == "" {
			continue
		}
		messages = append(messages, msg)
		ids = append(ids, entry.ID)
	}
	return messages, ids
}

func structuredToolContexts(entries []worker.HistoryEntry) map[string]structuredToolContext {
	out := make(map[string]structuredToolContext)
	for _, entry := range entries {
		for _, block := range entry.Blocks {
			if block.Kind != worker.BlockKindToolUse || strings.TrimSpace(block.ToolUseID) == "" {
				continue
			}
			out[block.ToolUseID] = structuredToolContext{
				Name:  block.Name,
				Input: normalizeStructuredToolInput(block.Name, block.Input),
			}
		}
	}
	return out
}

func historyEntryToStructuredMessage(entry worker.HistoryEntry, includeThinking bool, toolContexts map[string]structuredToolContext) SessionStructuredMessage {
	role := entry.Kind
	if role == "" {
		role = string(entry.Actor)
	}
	msg := SessionStructuredMessage{
		ID:       entry.ID,
		Role:     role,
		Provider: entry.Provenance.Provider,
		Status:   string(entry.Status),
		Blocks:   make([]SessionStructuredBlock, 0, len(entry.Blocks)),
	}
	if entry.Timestamp != nil {
		msg.Timestamp = entry.Timestamp.Format(time.RFC3339Nano)
	}
	for _, block := range entry.Blocks {
		if structured := historyBlockToStructuredBlock(block, includeThinking, toolContexts); structured != nil {
			msg.Blocks = append(msg.Blocks, *structured)
		}
	}
	return msg
}

func historyBlockToStructuredBlock(block worker.HistoryBlock, includeThinking bool, toolContexts map[string]structuredToolContext) *SessionStructuredBlock {
	out := &SessionStructuredBlock{
		Type:       string(block.Kind),
		Text:       block.Text,
		ToolCallID: block.ToolUseID,
		Name:       block.Name,
		Input:      normalizeStructuredToolInput(block.Name, block.Input),
		Content:    structuredJSONText(block.Content),
		IsError:    block.IsError,
	}
	switch block.Kind {
	case worker.BlockKindThinking:
		out.Text = ""
		if includeThinking {
			out.Thinking = block.Text
		}
	case worker.BlockKindToolUse:
		out.ID = block.ToolUseID
	case worker.BlockKindToolResult:
		if out.Content == "" {
			out.Content = block.Text
		}
		context := toolContexts[block.ToolUseID]
		out.Structured = inferStructuredToolResult(block, context, out.Content)
	case worker.BlockKindInteraction:
		out.Interaction = structuredInteraction(block.Interaction)
	}
	return out
}

func normalizeStructuredToolInput(name string, raw json.RawMessage) *SessionStructuredToolInput {
	if len(raw) == 0 {
		return nil
	}
	text := structuredJSONText(raw)
	out := &SessionStructuredToolInput{}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if lowerName == "apply_patch" {
		patch, filePath := editPatchFromRawInput(raw)
		if patch == "" {
			patch = text
			filePath = patchFilePath(text)
		}
		out.Kind = "patch"
		out.Patch = patch
		out.FilePath = filePath
		return out
	}
	if looksLikePatch(text) {
		out.Kind = "patch"
		out.Patch = text
		out.FilePath = patchFilePath(text)
		return out
	}

	for _, field := range structuredJSONFields(raw) {
		switch normalizeStructuredFieldName(field.Name) {
		case "command":
			out.Command = firstNonEmptyString(out.Command, field.Value)
		case "code":
			out.Code = firstNonEmptyString(out.Code, field.Value)
		case "patch":
			out.Patch = firstNonEmptyString(out.Patch, field.Value)
		case "file_path":
			out.FilePath = firstNonEmptyString(out.FilePath, field.Value)
		case "query":
			out.Query = firstNonEmptyString(out.Query, field.Value)
		case "pattern":
			out.Pattern = firstNonEmptyString(out.Pattern, field.Value)
		case "text":
			out.Text = firstNonEmptyString(out.Text, field.Value)
		default:
			out.Arguments = append(out.Arguments, field)
		}
	}

	if patch, filePath := editPatchFromRawInput(raw); patch != "" && isEditTool(lowerName, out) {
		out.Kind = "patch"
		out.Patch = patch
		out.FilePath = firstNonEmptyString(out.FilePath, filePath)
		return out
	}
	if out.Command != "" {
		if derived := shellDerivedStructuredInput(out.Command); derived != nil {
			derived.Command = out.Command
			if len(out.Arguments) > 0 {
				derived.Arguments = append(derived.Arguments, out.Arguments...)
			}
			return derived
		}
	}

	switch {
	case out.Command != "":
		out.Kind = "command"
	case out.Code != "":
		out.Kind = "code"
	case out.Patch != "":
		out.Kind = "patch"
	case out.FilePath != "":
		out.Kind = "file"
	case out.Query != "" || out.Pattern != "":
		out.Kind = "search"
	case out.Text != "":
		out.Kind = "text"
	case len(out.Arguments) > 0:
		out.Kind = "arguments"
	case text != "":
		out.Kind = "text"
		out.Text = text
	}
	if out.Kind == "" {
		return nil
	}
	return out
}

func inferStructuredToolResult(block worker.HistoryBlock, context structuredToolContext, content string) *SessionStructuredToolResult {
	if content == "" {
		return nil
	}
	name := strings.ToLower(strings.TrimSpace(firstNonEmptyString(block.Name, context.Name)))
	if isPythonTool(name, context.Input) {
		stdout, stderr, exitCode, interrupted, truncated, isImage := commandResultFields(block.Content, content)
		return &SessionStructuredToolResult{
			Kind:        "python",
			Text:        content,
			Code:        firstNonEmptyString(inputCode(context.Input), resultCode(block.Content)),
			Stdout:      stdout,
			Stderr:      stderr,
			ExitCode:    exitCode,
			Interrupted: interrupted,
			Truncated:   truncated,
			IsImage:     isImage,
		}
	}
	if isReadTool(name, context.Input) {
		readContent := commandOutputPayload(content)
		startLine, endLine := shellReadRange(inputCommand(context.Input))
		numLines := countLines(readContent)
		if startLine > 0 && endLine >= startLine {
			numLines = endLine - startLine + 1
		}
		return &SessionStructuredToolResult{
			Kind:       "read",
			FilePath:   inputFilePath(context.Input),
			Content:    readContent,
			NumLines:   numLines,
			StartLine:  startLine,
			TotalLines: endLine,
		}
	}
	if isSearchTool(name, context.Input) {
		searchContent := commandOutputPayload(content)
		filenames := searchResultFilenames(searchContent)
		return &SessionStructuredToolResult{
			Kind:      "grep",
			Mode:      searchMode(context.Input),
			Filenames: filenames,
			NumFiles:  len(filenames),
			Content:   searchContent,
			NumLines:  countLines(searchContent),
		}
	}
	if isCommandTool(name, context.Input) {
		stdout, stderr, exitCode, interrupted, truncated, isImage := commandResultFields(block.Content, content)
		text := firstNonEmptyString(stdout, stderr, content)
		return &SessionStructuredToolResult{
			Kind:        "bash",
			Text:        text,
			Stdout:      stdout,
			Stderr:      stderr,
			ExitCode:    exitCode,
			Interrupted: interrupted,
			Truncated:   truncated,
			IsImage:     isImage,
		}
	}
	if name == "apply_patch" || isEditTool(name, context.Input) || (context.Input != nil && context.Input.Kind == "patch") || looksLikePatch(content) {
		resultPatch, resultFile := editPatchFromRawResult(block.Content)
		patch := firstNonEmptyString(resultPatch, patchContent(content))
		return &SessionStructuredToolResult{
			Kind:     "edit",
			FilePath: firstNonEmptyString(resultFile, patchFilePath(patch), patchFilePath(content), inputFilePath(context.Input)),
			Patch:    patch,
			Content:  content,
		}
	}
	return &SessionStructuredToolResult{
		Kind:    "text",
		Text:    content,
		Content: content,
	}
}

func structuredInteraction(in *worker.HistoryInteraction) *SessionStructuredInteraction {
	if in == nil {
		return nil
	}
	return &SessionStructuredInteraction{
		RequestID: in.RequestID,
		Kind:      in.Kind,
		State:     string(in.State),
		Prompt:    in.Prompt,
		Options:   append([]string(nil), in.Options...),
		Action:    in.Action,
	}
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func structuredJSONText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var textBlocks []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &textBlocks); err == nil {
		parts := make([]string, 0, len(textBlocks))
		for _, block := range textBlocks {
			switch {
			case block.Text != "":
				parts = append(parts, block.Text)
			case block.Content != "":
				parts = append(parts, block.Content)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	var object struct {
		Output  string `json:"output"`
		Stdout  string `json:"stdout"`
		Stderr  string `json:"stderr"`
		Text    string `json:"text"`
		Content string `json:"content"`
		Error   string `json:"error"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return strings.Join(nonEmptyStrings(
			object.Output,
			object.Stdout,
			object.Stderr,
			object.Text,
			object.Content,
			object.Error,
			object.Result,
		), "\n")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return buf.String()
	}
	return string(raw)
}

func structuredJSONFields(raw json.RawMessage) []SessionStructuredArgument {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) == 0 {
		return nil
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]SessionStructuredArgument, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, SessionStructuredArgument{
			Name:  key,
			Value: structuredJSONText(object[key]),
		})
	}
	return fields
}

func normalizeStructuredFieldName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cmd", "command", "shell_command":
		return "command"
	case "code", "python", "script":
		return "code"
	case "patch", "diff", "file_diff", "filediff":
		return "patch"
	case "file", "file_path", "filepath", "path":
		return "file_path"
	case "q", "query", "search_query":
		return "query"
	case "pattern", "regexp", "regex":
		return "pattern"
	case "content", "new_string", "newstring", "new_str", "old_string", "oldstring", "old_str", "replacement", "text":
		return "text"
	default:
		return name
	}
}

func looksLikePatch(text string) bool {
	return strings.Contains(text, "*** Begin Patch") || strings.Contains(text, "\n@@")
}

func patchFilePath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return ""
}

func patchContent(content string) string {
	if looksLikePatch(content) {
		return content
	}
	return ""
}

func editPatchFromRawInput(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	text := structuredJSONText(raw)
	if looksLikePatch(text) {
		return text, patchFilePath(text)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
		return "", ""
	}
	if patch := jsonStringField(object, "patch", "diff", "file_diff", "fileDiff"); patch != "" {
		return patch, firstNonEmptyString(jsonStringField(object, "file_path", "filePath", "path", "file"), patchFilePath(patch))
	}
	filePath := jsonStringField(object, "file_path", "filePath", "path", "file")
	if patch := editPatchFromEditArray(object, filePath); patch != "" {
		return patch, filePath
	}
	oldText := jsonStringField(object, "old_string", "oldString", "old_str", "oldStr", "old")
	newText := jsonStringField(object, "new_string", "newString", "new_str", "newStr", "replacement", "new")
	if oldText != "" || newText != "" {
		return buildUnifiedPatch(filePath, []editPatchHunk{{OldText: oldText, NewText: newText}}), filePath
	}
	content := jsonStringField(object, "content", "file_text", "fileText", "new_content", "newContent")
	if content != "" {
		return buildUnifiedPatch(filePath, []editPatchHunk{{NewText: content}}), filePath
	}
	return "", ""
}

func editPatchFromRawResult(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
		return "", ""
	}
	if displayRaw, ok := object["resultDisplay"]; ok {
		if patch, filePath := editPatchFromResultDisplay(displayRaw); patch != "" {
			return patch, filePath
		}
	}
	for _, key := range []string{"tool_result", "toolUseResult", "provider_result"} {
		if resultRaw, ok := object[key]; ok {
			if patch, filePath := editPatchFromRawResult(resultRaw); patch != "" {
				return patch, filePath
			}
		}
	}
	if patch, filePath := editPatchFromResultDisplay(raw); patch != "" {
		return patch, filePath
	}
	if patch, filePath := editPatchFromStructuredPatch(object); patch != "" {
		return patch, filePath
	}
	if patch := jsonStringField(object, "patch", "diff", "file_diff", "fileDiff"); patch != "" {
		return patch, firstNonEmptyString(jsonStringField(object, "file_path", "filePath", "path", "file"), patchFilePath(patch))
	}
	return "", ""
}

func editPatchFromResultDisplay(raw json.RawMessage) (string, string) {
	var display map[string]json.RawMessage
	if json.Unmarshal(raw, &display) != nil || len(display) == 0 {
		return "", ""
	}
	filePath := jsonStringField(display, "file_path", "filePath", "fileName", "file")
	if patch := jsonStringField(display, "file_diff", "fileDiff", "patch", "diff"); patch != "" {
		return patch, firstNonEmptyString(filePath, patchFilePath(patch))
	}
	oldText := jsonStringField(display, "original_content", "originalContent", "old_content", "oldContent")
	newText := jsonStringField(display, "new_content", "newContent", "content")
	if oldText != "" || newText != "" {
		return buildUnifiedPatch(filePath, []editPatchHunk{{OldText: oldText, NewText: newText}}), filePath
	}
	return "", ""
}

func editPatchFromEditArray(object map[string]json.RawMessage, filePath string) string {
	rawEdits, ok := object["edits"]
	if !ok {
		return ""
	}
	var edits []map[string]json.RawMessage
	if json.Unmarshal(rawEdits, &edits) != nil || len(edits) == 0 {
		return ""
	}
	hunks := make([]editPatchHunk, 0, len(edits))
	for _, edit := range edits {
		oldText := jsonStringField(edit, "old_string", "oldString", "old_str", "oldStr", "old")
		newText := jsonStringField(edit, "new_string", "newString", "new_str", "newStr", "replacement", "new")
		if oldText == "" && newText == "" {
			continue
		}
		hunks = append(hunks, editPatchHunk{OldText: oldText, NewText: newText})
	}
	if len(hunks) == 0 {
		return ""
	}
	return buildUnifiedPatch(filePath, hunks)
}

type editPatchHunk struct {
	OldText string
	NewText string
}

func buildUnifiedPatch(filePath string, hunks []editPatchHunk) string {
	if len(hunks) == 0 {
		return ""
	}
	from := firstNonEmptyString(filePath, "file")
	to := from
	if len(hunks) == 1 {
		if hunks[0].OldText == "" && hunks[0].NewText != "" {
			from = "/dev/null"
		}
		if hunks[0].OldText != "" && hunks[0].NewText == "" {
			to = "/dev/null"
		}
	}

	var b strings.Builder
	b.WriteString("--- ")
	b.WriteString(from)
	b.WriteString("\n+++ ")
	b.WriteString(to)
	for _, hunk := range hunks {
		b.WriteString("\n@@\n")
		appendPatchLines(&b, "-", hunk.OldText)
		appendPatchLines(&b, "+", hunk.NewText)
	}
	return b.String()
}

func appendPatchLines(b *strings.Builder, prefix, text string) {
	if text == "" {
		return
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			continue
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func jsonStringField(object map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := object[name]
		if !ok || len(raw) == 0 {
			continue
		}
		if text := structuredJSONText(raw); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func editPatchFromStructuredPatch(object map[string]json.RawMessage) (string, string) {
	rawPatch, ok := object["structuredPatch"]
	if !ok {
		return "", ""
	}
	var hunks []struct {
		OldStart int      `json:"oldStart"`
		OldLines int      `json:"oldLines"`
		NewStart int      `json:"newStart"`
		NewLines int      `json:"newLines"`
		Lines    []string `json:"lines"`
	}
	if json.Unmarshal(rawPatch, &hunks) != nil || len(hunks) == 0 {
		return "", ""
	}
	filePath := jsonStringField(object, "file_path", "filePath", "path", "file")
	var b strings.Builder
	from := firstNonEmptyString(filePath, "file")
	b.WriteString("--- ")
	b.WriteString(from)
	b.WriteString("\n+++ ")
	b.WriteString(from)
	for _, hunk := range hunks {
		b.WriteString("\n@@")
		if hunk.OldStart > 0 || hunk.NewStart > 0 {
			b.WriteString(" -")
			b.WriteString(formatPatchRange(hunk.OldStart, hunk.OldLines))
			b.WriteString(" +")
			b.WriteString(formatPatchRange(hunk.NewStart, hunk.NewLines))
			b.WriteString(" ")
		}
		b.WriteString("@@\n")
		for _, line := range hunk.Lines {
			if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\\") {
				b.WriteString(line)
			} else {
				b.WriteString(" ")
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
	}
	return b.String(), filePath
}

func formatPatchRange(start, lines int) string {
	if start <= 0 {
		start = 1
	}
	if lines <= 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if lines == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, lines)
}

func inputFilePath(input *SessionStructuredToolInput) string {
	if input == nil {
		return ""
	}
	return input.FilePath
}

func inputCode(input *SessionStructuredToolInput) string {
	if input == nil {
		return ""
	}
	return input.Code
}

func inputCommand(input *SessionStructuredToolInput) string {
	if input == nil {
		return ""
	}
	return input.Command
}

func resultCode(raw json.RawMessage) string {
	var object struct {
		Code   string `json:"code"`
		Script string `json:"script"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &object) == nil {
		return firstNonEmptyString(object.Code, object.Script)
	}
	return ""
}

func isPythonTool(name string, input *SessionStructuredToolInput) bool {
	if input != nil && input.Kind == "code" {
		return true
	}
	switch name {
	case "python", "python_execution", "pythonexecution":
		return true
	default:
		return false
	}
}

func isCommandTool(name string, input *SessionStructuredToolInput) bool {
	if input != nil && input.Kind == "command" {
		return true
	}
	switch name {
	case "bash", "shell", "sh", "run_command", "exec_command", "shell_command", "terminal", "terminal.exec":
		return true
	default:
		return false
	}
}

func isReadTool(name string, input *SessionStructuredToolInput) bool {
	if input != nil && input.Kind == "file" {
		return true
	}
	switch name {
	case "read", "read_file", "view", "cat", "open_file":
		return true
	default:
		return false
	}
}

func isEditTool(name string, _ *SessionStructuredToolInput) bool {
	if name == "" {
		return false
	}
	switch name {
	case "edit", "write", "write_file", "writefile", "multi_edit", "multiedit",
		"create_file", "replace", "str_replace", "str_replace_editor":
		return true
	default:
		return strings.Contains(name, "edit") || strings.Contains(name, "write")
	}
}

func isSearchTool(name string, input *SessionStructuredToolInput) bool {
	if input != nil && input.Kind == "search" {
		return true
	}
	return strings.Contains(name, "grep") ||
		strings.Contains(name, "search") ||
		name == "rg"
}

func searchMode(input *SessionStructuredToolInput) string {
	if input == nil {
		return ""
	}
	if input.Pattern != "" {
		return "pattern"
	}
	if input.Query != "" {
		return "query"
	}
	return ""
}

func shellDerivedStructuredInput(command string) *SessionStructuredToolInput {
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "cat":
		if filePath := lastNonOptionArg(args[1:]); filePath != "" {
			return &SessionStructuredToolInput{Kind: "file", FilePath: filePath}
		}
	case "sed":
		if filePath := lastNonOptionArg(args[1:]); filePath != "" {
			return &SessionStructuredToolInput{Kind: "file", FilePath: filePath}
		}
	case "nl":
		if filePath := nlInputFile(args); filePath != "" {
			return &SessionStructuredToolInput{Kind: "file", FilePath: filePath}
		}
	case "rg", "grep":
		pattern, paths := grepPatternAndPaths(args)
		if pattern == "" {
			return nil
		}
		input := &SessionStructuredToolInput{
			Kind:    "search",
			Pattern: pattern,
		}
		if len(paths) == 1 {
			input.FilePath = paths[0]
		}
		for _, path := range paths {
			input.Arguments = append(input.Arguments, SessionStructuredArgument{Name: "path", Value: path})
		}
		return input
	}
	return nil
}

func lastNonOptionArg(args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		arg := strings.TrimSpace(args[i])
		if arg == "" || arg == "|" || strings.HasPrefix(arg, "-") || looksLikeSedAddress(arg) {
			continue
		}
		return arg
	}
	return ""
}

func nlInputFile(args []string) string {
	pipeIndex := len(args)
	for i, arg := range args {
		if arg == "|" {
			pipeIndex = i
			break
		}
	}
	return lastNonOptionArg(args[1:pipeIndex])
}

func grepPatternAndPaths(args []string) (string, []string) {
	if len(args) < 2 {
		return "", nil
	}
	var pattern string
	var paths []string
	skipNext := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			if i+1 < len(args) && pattern == "" {
				pattern = args[i+1]
				paths = append(paths, args[i+2:]...)
			}
			break
		}
		if strings.HasPrefix(arg, "-") {
			if flagTakesValue(arg) && i+1 < len(args) {
				skipNext = true
			}
			continue
		}
		if pattern == "" {
			pattern = arg
			continue
		}
		paths = append(paths, arg)
	}
	return pattern, paths
}

func flagTakesValue(flag string) bool {
	switch flag {
	case "-e", "--regexp", "-g", "--glob", "-t", "--type", "-m", "--max-count", "-A", "--after-context", "-B", "--before-context", "-C", "--context":
		return true
	default:
		return false
	}
}

func commandOutputPayload(content string) string {
	before, after, ok := strings.Cut(content, "\nOutput:")
	if !ok {
		return content
	}
	if !strings.HasPrefix(strings.TrimSpace(before), "Command:") {
		return content
	}
	return strings.TrimPrefix(after, "\n")
}

func shellReadRange(command string) (int, int) {
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 {
		return 0, 0
	}
	for _, arg := range args {
		start, end, ok := parseSedAddress(arg)
		if ok {
			return start, end
		}
	}
	return 0, 0
}

func looksLikeSedAddress(value string) bool {
	_, _, ok := parseSedAddress(value)
	return ok
}

func parseSedAddress(value string) (int, int, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "p")
	if value == "" {
		return 0, 0, false
	}
	startText, endText, hasComma := strings.Cut(value, ",")
	if !hasComma {
		line, ok := parsePositiveInt(startText)
		if !ok {
			return 0, 0, false
		}
		return line, line, true
	}
	start, ok := parsePositiveInt(startText)
	if !ok {
		return 0, 0, false
	}
	end, ok := parsePositiveInt(endText)
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func parsePositiveInt(value string) (int, bool) {
	var out int
	if value == "" {
		return 0, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		out = out*10 + int(r-'0')
	}
	if out <= 0 {
		return 0, false
	}
	return out, true
}

func searchResultFilenames(content string) []string {
	seen := make(map[string]struct{})
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var filename string
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			filename = strings.TrimRight(firstWhitespaceDelimitedToken(line), ":")
		} else {
			var ok bool
			filename, _, ok = strings.Cut(line, ":")
			if !ok {
				continue
			}
		}
		filename = strings.TrimSpace(filename)
		if filename == "" || strings.Contains(filename, " ") {
			continue
		}
		seen[filename] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	filenames := make([]string, 0, len(seen))
	for filename := range seen {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	return filenames
}

func firstWhitespaceDelimitedToken(line string) string {
	for i, r := range line {
		if r == ' ' || r == '\t' {
			return line[:i]
		}
	}
	return line
}

func commandResultFields(raw json.RawMessage, content string) (stdout string, stderr string, exitCode *int, interrupted bool, truncated bool, isImage bool) {
	stdout, stderr, exitCode, interrupted, truncated, isImage = commandResultFieldsDepth(raw, 0)
	if stdout == "" && stderr == "" {
		stdout = content
	}
	return stdout, stderr, exitCode, interrupted, truncated, isImage
}

func commandResultFieldsDepth(raw json.RawMessage, depth int) (stdout string, stderr string, exitCode *int, interrupted bool, truncated bool, isImage bool) {
	if len(raw) == 0 || depth > 4 {
		return "", "", nil, false, false, false
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		encoded = strings.TrimSpace(encoded)
		if encoded != "" && json.Valid([]byte(encoded)) {
			return commandResultFieldsDepth(json.RawMessage(encoded), depth+1)
		}
		return "", "", nil, false, false, false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
		return "", "", nil, false, false, false
	}
	for _, key := range []string{"tool_result", "toolUseResult", "provider_result"} {
		if nested, ok := object[key]; ok {
			stdout, stderr, exitCode, interrupted, truncated, isImage = commandResultFieldsDepth(nested, depth+1)
			if stdout != "" || stderr != "" || exitCode != nil || interrupted || truncated || isImage {
				return stdout, stderr, exitCode, interrupted, truncated, isImage
			}
		}
	}
	stdout = firstNonEmptyString(
		jsonStringField(object, "stdout"),
		jsonStringField(object, "output"),
		jsonStringField(object, "text"),
		jsonStringField(object, "result"),
	)
	stderr = jsonStringField(object, "stderr", "error")
	exitCode = jsonIntField(object, "exit_code", "exitCode")
	if exitCode == nil {
		if metadata, ok := object["metadata"]; ok {
			var metadataObject map[string]json.RawMessage
			if json.Unmarshal(metadata, &metadataObject) == nil {
				exitCode = jsonIntField(metadataObject, "exit_code", "exitCode")
			}
		}
	}
	interrupted = jsonBoolField(object, "interrupted") || jsonBoolField(object, "canceled") || jsonBoolField(object, "canceled")
	truncated = jsonBoolField(object, "truncated")
	isImage = jsonBoolField(object, "is_image") || jsonBoolField(object, "isImage")
	return stdout, stderr, exitCode, interrupted, truncated, isImage
}

func jsonIntField(object map[string]json.RawMessage, names ...string) *int {
	for _, name := range names {
		raw, ok := object[name]
		if !ok || len(raw) == 0 {
			continue
		}
		var value int
		if json.Unmarshal(raw, &value) == nil {
			return &value
		}
	}
	return nil
}

func jsonBoolField(object map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		raw, ok := object[name]
		if !ok || len(raw) == 0 {
			continue
		}
		var value bool
		if json.Unmarshal(raw, &value) == nil && value {
			return true
		}
	}
	return false
}

func countLines(content string) int {
	content = strings.TrimRight(content, "\r\n")
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
