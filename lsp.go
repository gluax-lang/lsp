// LSP v3.17
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/textproto"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
)

// ===================== JSON-RPC 2.0 transport ==============================

func readMsg(r *bufio.Reader) ([]byte, error) {
	tp := textproto.NewReader(r)
	// Read MIME-style headers
	mimeHeader, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	lenStr := mimeHeader.Get("Content-Length")
	if lenStr == "" {
		return nil, errors.New("missing Content-Length")
	}
	length, err := strconv.Atoi(lenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid Content-Length: %w", err)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ========================= RPC wire structs ===============================

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// ========================== LSP core types ================================

type uinteger = uint32

// Position in a text document expressed as zero‑based line and UTF‑16 code‑unit
// offset.  (For ASCII / UTF‑8 the code‑unit index equals the byte index.)

type Position struct {
	Line      uinteger `json:"line"`
	Character uinteger `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

func (r Range) Contains(pos Position) bool {
	// Position is before the range start
	if pos.Line < r.Start.Line {
		return false
	}

	// Position is after the range end
	if pos.Line > r.End.Line {
		return false
	}

	// Position is on the start line
	if pos.Line == r.Start.Line {
		if pos.Character < r.Start.Character {
			return false
		}
	}

	// Position is on the end line
	if pos.Line == r.End.Line {
		if pos.Character >= r.End.Character {
			return false
		}
	}

	return true
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// --------------------------------------------------------------------------
//   Capability helper types
// --------------------------------------------------------------------------

type WorkDoneProgressOptions struct {
	WorkDoneProgress bool `json:"workDoneProgress,omitempty"`
}

type HoverOptions struct {
	WorkDoneProgressOptions
}

type SaveOptions struct {
	IncludeText bool `json:"includeText,omitempty"`
}

type TextDocumentSyncOptions struct {
	OpenClose         bool         `json:"openClose,omitempty"`
	Change            int          `json:"change,omitempty"`
	WillSave          bool         `json:"willSave,omitempty"`
	WillSaveWaitUntil bool         `json:"willSaveWaitUntil,omitempty"`
	Save              *SaveOptions `json:"save,omitempty"` // NB: spec allows boolean | object
}

const (
	TextDocumentSyncKindNone        = 0
	TextDocumentSyncKindFull        = 1
	TextDocumentSyncKindIncremental = 2
)

type InlayHintOptions struct {
	WorkDoneProgressOptions
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// ---------------------------------------------------------------------------
//  Raw‑pass‑through helper
// ---------------------------------------------------------------------------

type rawPass []byte

func makeRawPass(v any) rawPass {
	d, _ := json.Marshal(v)
	return rawPass(d)
}

func (b rawPass) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("null"), nil
	}
	return b, nil // no extra quoting, no base‑64
}

func (b *rawPass) UnmarshalJSON(data []byte) error {
	if b == nil {
		return errors.New("rawPass: UnmarshalJSON on nil *rawPass")
	}
	*b = append((*b)[:0], data...)
	return nil
}

// --- json‑union helpers ----------------------------------------------------

type HoverProvider = rawPass

type InlayHintProvider = rawPass

type TextDocumentSync = rawPass

func NewHoverProviderBool(b bool) HoverProvider            { return makeRawPass(b) }
func NewHoverProviderOptions(o HoverOptions) HoverProvider { return makeRawPass(o) }

func NewInlayHintProviderBool(b bool) InlayHintProvider                { return makeRawPass(b) }
func NewInlayHintProviderOptions(o InlayHintOptions) InlayHintProvider { return makeRawPass(o) }

func NewTextDocumentSyncKind(k int) TextDocumentSync                        { return makeRawPass(k) }
func NewTextDocumentSyncOptions(o TextDocumentSyncOptions) TextDocumentSync { return makeRawPass(o) }

// --- ServerCapabilities ----------------------------------------------------

type ServerCapabilities struct {
	HoverProvider      HoverProvider     `json:"hoverProvider,omitempty"`
	TextDocumentSync   TextDocumentSync  `json:"textDocumentSync,omitempty"`
	InlayHintProvider  InlayHintProvider `json:"inlayHintProvider,omitempty"`
	CompletionProvider CompletionOptions `json:"completionProvider,omitempty"`
	DefinitionProvider bool              `json:"definitionProvider,omitempty"`
}

// -- initialize -------------------------------------------------------------

type WorkspaceFolder struct {
	URI  string `json:"uri"`  // e.g. "file:///home/alice/project"
	Name string `json:"name"` // UI-friendly name shown in the editor
}

type InitializeParams struct {
	ProcessID        *int               `json:"processId,omitempty"`
	RootURI          *string            `json:"rootUri,omitempty"`
	WorkspaceFolders *[]WorkspaceFolder `json:"workspaceFolders,omitempty"`
	Capabilities     json.RawMessage    `json:"capabilities"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// -- didOpen ----------------------------------------------------------------

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// -- didClose ----------------------------------------------------------------

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// -- didSave ----------------------------------------------------------------

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

// -- didChange --------------------------------------------------------------

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentContentChangeEvent struct {
	// Range of the document that changed.  If nil, the change applies to the
	// whole document (full‑text sync).
	Range *Range `json:"range,omitempty"`

	// Deprecated: RangeLength is kept for compatibility with older clients.
	RangeLength *uinteger `json:"rangeLength,omitempty"`

	// New text for the provided range (or the whole document).
	Text string `json:"text"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// -- hover ------------------------------------------------------------------

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// -- inlayHint --------------------------------------------------------------

type InlayHintKind int

const (
	InlayHintKindType  InlayHintKind = 1
	InlayHintKindParam InlayHintKind = 2
)

type InlayHintLabelPart struct {
	Value string `json:"value"`
}

type InlayHint struct {
	Position Position             `json:"position"`
	Label    []InlayHintLabelPart `json:"label"`
	Kind     *InlayHintKind       `json:"kind,omitempty"`
}

type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// -- completion -------------------------------------------------------------

type CompletionItemKind int

const (
	CompletionItemKindText          CompletionItemKind = 1
	CompletionItemKindMethod        CompletionItemKind = 2
	CompletionItemKindFunction      CompletionItemKind = 3
	CompletionItemKindConstructor   CompletionItemKind = 4
	CompletionItemKindField         CompletionItemKind = 5
	CompletionItemKindVariable      CompletionItemKind = 6
	CompletionItemKindClass         CompletionItemKind = 7
	CompletionItemKindInterface     CompletionItemKind = 8
	CompletionItemKindModule        CompletionItemKind = 9
	CompletionItemKindProperty      CompletionItemKind = 10
	CompletionItemKindUnit          CompletionItemKind = 11
	CompletionItemKindValue         CompletionItemKind = 12
	CompletionItemKindEnum          CompletionItemKind = 13
	CompletionItemKindKeyword       CompletionItemKind = 14
	CompletionItemKindSnippet       CompletionItemKind = 15
	CompletionItemKindColor         CompletionItemKind = 16
	CompletionItemKindFile          CompletionItemKind = 17
	CompletionItemKindReference     CompletionItemKind = 18
	CompletionItemKindFolder        CompletionItemKind = 19
	CompletionItemKindEnumMember    CompletionItemKind = 20
	CompletionItemKindConstant      CompletionItemKind = 21
	CompletionItemKindStruct        CompletionItemKind = 22
	CompletionItemKindEvent         CompletionItemKind = 23
	CompletionItemKindOperator      CompletionItemKind = 24
	CompletionItemKindTypeParameter CompletionItemKind = 25
)

type CompletionItem struct {
	Label         string              `json:"label"`
	Kind          *CompletionItemKind `json:"kind,omitempty"`
	Detail        *string             `json:"detail,omitempty"`
	Documentation *MarkupContent      `json:"documentation,omitempty"`
	FilterText    *string             `json:"filterText,omitempty"`
	InsertText    *string             `json:"insertText,omitempty"`
	SortText      *string             `json:"sortText,omitempty"`
}

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionOptions struct {
	WorkDoneProgressOptions
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	AllCommitCharacters []string `json:"allCommitCharacters,omitempty"`
	ResolveProvider     bool     `json:"resolveProvider,omitempty"`
}

// -- definition ---------------------------------------------------------

type DefinitionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// ---------------------------------------------------------------------------
//   diagnostics
// ---------------------------------------------------------------------------

// LSP‑spec severities start at 1, not 0.

type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

type Diagnostic struct {
	Range    Range               `json:"range"`
	Severity *DiagnosticSeverity `json:"severity,omitempty"`
	Source   string              `json:"source,omitempty"`
	Message  string              `json:"message"`
}

// Params for the server -> client notification.

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ========================= Optional interfaces ============================

type Initializer interface {
	Initialize(p *InitializeParams) (*InitializeResult, error)
}

type Initialized interface{ Initialized() error }

type Shutdowner interface{ Shutdown() error }

type DidOpener interface {
	DidOpen(p *DidOpenTextDocumentParams) error
}

type DidCloser interface {
	DidClose(p *DidCloseTextDocumentParams) error
}

type DidSaver interface {
	DidSave(p *DidSaveTextDocumentParams) error
}

type DidChanger interface {
	DidChange(p *DidChangeTextDocumentParams) error
}

type Hoverer interface {
	Hover(p *HoverParams) (*Hover, error)
}

type InlayHinter interface {
	InlayHint(p *InlayHintParams) ([]InlayHint, error)
}

type Completer interface {
	Complete(p *CompletionParams) (*CompletionList, error)
}

type Definer interface {
	Definition(p *DefinitionParams) ([]Location, error)
}

// ========================== Server engine ==================================

type Server struct {
	in           *bufio.Reader
	out          io.Writer
	mu           sync.Mutex // protects out
	handler      any
	shutdownSeen atomic.Bool
	wg           sync.WaitGroup
}

func NewServer(in io.Reader, out io.Writer, handler any) *Server {
	return &Server{
		in:      bufio.NewReader(in),
		out:     out,
		handler: handler,
	}
}

func (s *Server) writeRPC(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// log.Printf("writeRPC: %s", string(data))
	header := fmt.Sprintf(
		"Content-Length: %d\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n",
		len(data),
	)
	if _, err := io.WriteString(s.out, header); err != nil {
		return err
	}
	_, err = s.out.Write(data)
	return err
}

func (s *Server) Serve(ctx context.Context) error { return s.serve(ctx) }

func (s *Server) Start(ctx context.Context) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- s.serve(ctx) }()
	return ch
}

// ---------------- helper: Notify / Respond --------------------------------

func (s *Server) Notify(method string, params any) error {
	return s.writeRPC(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (s *Server) RespondOK(id json.RawMessage, result any) {
	if err := s.writeRPC(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}); err != nil {
		log.Printf("RespondOK error: %v", err)
	}
}

func (s *Server) RespondErr(id json.RawMessage, code int, message string) {
	if err := s.writeRPC(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &responseError{
			Code:    code,
			Message: message,
		},
	}); err != nil {
		log.Printf("RespondErr error: %v", err)
	}

}

// ---------------------------- main loop ------------------------------------

func (s *Server) serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, err := readMsg(s.in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.wg.Wait() // Wait for ongoing handlers
				return nil
			}
			return err
		}
		var req rpcRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			s.RespondErr(nil, codeParseError, err.Error())
			continue
		}
		s.wg.Add(1)
		go func(r *rpcRequest) {
			defer s.wg.Done()
			s.dispatch(r)
		}(&req)
	}
}

// ----------------------------- dispatch ------------------------------------

func (s *Server) dispatch(req *rpcRequest) {
	// log.Println("dispatch", req.Method)
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized":
		s.handleInitialized()
	case "shutdown":
		s.handleShutdown(req)
	case "exit":
		s.handleExit()
	case "textDocument/didOpen":
		s.handleDidOpen(req)
	case "textDocument/didClose":
		s.handleDidClose(req)
	case "textDocument/didSave":
		s.handleDidSave(req)
	case "textDocument/didChange":
		s.handleDidChange(req)
	case "textDocument/hover":
		s.handleHover(req)
	case "textDocument/inlayHint":
		s.handleInlayHint(req)
	case "textDocument/completion":
		s.handleCompletion(req)
	case "textDocument/definition":
		s.handleDefinition(req)
	default:
		if req.ID != nil {
			s.RespondErr(req.ID, codeMethodNotFound, "unknown method: "+req.Method)
		}
	}
}

func (s *Server) handleExit() {
	// Spec 3.16: If the server receives `exit` before `shutdown`` it
	// must terminate with exit status 1, otherwise 0.
	code := 0
	if !s.shutdownSeen.Load() {
		code = 1
	}

	// Finish any in-flight writes, then close the stream.
	if closer, ok := s.out.(io.Closer); ok {
		_ = closer.Close()
	}

	os.Exit(code)
}

// --------------------------- request handlers ------------------------------

func decode[T any](id json.RawMessage, raw json.RawMessage, dst *T, s *Server) bool {
	if err := json.Unmarshal(raw, dst); err != nil {
		if id != nil {
			s.RespondErr(id, codeInvalidParams, err.Error())
		}
		return false
	}
	return true
}

func (s *Server) handleInitialize(req *rpcRequest) {
	var p InitializeParams
	if !decode(req.ID, req.Params, &p, s) {
		return
	}
	if h, ok := s.handler.(Initializer); ok {
		if res, err := h.Initialize(&p); err == nil {
			s.RespondOK(req.ID, res)
		} else {
			s.RespondErr(req.ID, codeInternalError, err.Error())
		}
		return
	}
	s.RespondOK(req.ID, &InitializeResult{Capabilities: ServerCapabilities{
		HoverProvider: NewHoverProviderBool(true),
		TextDocumentSync: NewTextDocumentSyncOptions(TextDocumentSyncOptions{
			OpenClose: true,
			Change:    TextDocumentSyncKindFull,
			Save:      &SaveOptions{IncludeText: false},
		}),
	}})
}

func (s *Server) handleInitialized() {
	if h, ok := s.handler.(Initialized); ok {
		_ = h.Initialized()
	}
}

func (s *Server) handleShutdown(req *rpcRequest) {
	if h, ok := s.handler.(Shutdowner); ok {
		_ = h.Shutdown()
	}
	s.shutdownSeen.Store(true)
	s.RespondOK(req.ID, nil)
}

func (s *Server) handleDidOpen(req *rpcRequest) {
	var p DidOpenTextDocumentParams
	if json.Unmarshal(req.Params, &p) != nil {
		return
	}
	if h, ok := s.handler.(DidOpener); ok {
		_ = h.DidOpen(&p)
	}
}

func (s *Server) handleDidClose(req *rpcRequest) {
	var p DidCloseTextDocumentParams
	if json.Unmarshal(req.Params, &p) != nil {
		return
	}
	if h, ok := s.handler.(DidCloser); ok {
		_ = h.DidClose(&p)
	}
}

func (s *Server) handleDidSave(req *rpcRequest) {
	var p DidSaveTextDocumentParams
	if json.Unmarshal(req.Params, &p) != nil {
		return
	}
	if h, ok := s.handler.(DidSaver); ok {
		_ = h.DidSave(&p)
	}
}

func (s *Server) handleDidChange(req *rpcRequest) {
	var p DidChangeTextDocumentParams
	if json.Unmarshal(req.Params, &p) != nil {
		return
	}
	if h, ok := s.handler.(DidChanger); ok {
		_ = h.DidChange(&p)
	}
}

func (s *Server) handleHover(req *rpcRequest) {
	var p HoverParams
	if !decode(req.ID, req.Params, &p, s) {
		return
	}
	if h, ok := s.handler.(Hoverer); ok {
		if hv, err := h.Hover(&p); err == nil {
			s.RespondOK(req.ID, hv)
		} else {
			s.RespondErr(req.ID, codeInternalError, err.Error())
		}
	}
}

func (s *Server) handleInlayHint(req *rpcRequest) {
	var p InlayHintParams
	if !decode(req.ID, req.Params, &p, s) {
		return
	}
	if h, ok := s.handler.(InlayHinter); ok {
		if hints, err := h.InlayHint(&p); err == nil {
			s.RespondOK(req.ID, hints)
		} else {
			s.RespondErr(req.ID, codeInternalError, err.Error())
		}
	} else {
		s.RespondOK(req.ID, []InlayHint{}) // handler doesnt implement it
	}
}

func (s *Server) handleCompletion(req *rpcRequest) {
	var p CompletionParams
	if !decode(req.ID, req.Params, &p, s) {
		return
	}
	if h, ok := s.handler.(Completer); ok {
		if result, err := h.Complete(&p); err == nil {
			s.RespondOK(req.ID, result)
		} else {
			s.RespondErr(req.ID, codeInternalError, err.Error())
		}
	} else {
		// Return empty completion list if handler doesn't implement completion
		s.RespondOK(req.ID, &CompletionList{
			IsIncomplete: false,
			Items:        []CompletionItem{},
		})
	}
}

func (s *Server) handleDefinition(req *rpcRequest) {
	var p DefinitionParams
	if !decode(req.ID, req.Params, &p, s) {
		return
	}
	if h, ok := s.handler.(Definer); ok {
		if locations, err := h.Definition(&p); err == nil {
			s.RespondOK(req.ID, locations)
		} else {
			s.RespondErr(req.ID, codeInternalError, err.Error())
		}
	} else {
		// Return empty array if handler doesn't implement definition
		s.RespondOK(req.ID, []Location{})
	}
}

func (s *Server) PublishDiagnostics(uri string, diags []Diagnostic) {
	// Make sure we never pass a nil slice
	if diags == nil {
		diags = make([]Diagnostic, 0)
	}
	_ = s.Notify("textDocument/publishDiagnostics",
		PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: diags,
		})
}
