# LSP

LSP Protocol for Go. (v3.17)

Made specifically for GLuaX, but can be used for anything really.

It's a work in progress, and not all features are implemented yet.

## Installation

`go get -u github.com/gluax-lang/lsp@latest`

## Example

```go
type Handler struct {
	*protocol.Server
}

func NewHandler() *Handler {
	h := &Handler{
		fileCache: make(map[string]string),
		mu:        sync.Mutex{},
	}
	h.Server = protocol.NewServer(os.Stdin, os.Stdout, h)
	return h
}

func (h *Handler) Initialize(p *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
		HoverProvider: protocol.NewHoverProviderBool(true),
		TextDocumentSync: protocol.NewTextDocumentSyncOptions(protocol.TextDocumentSyncOptions{
			OpenClose: true,
			Change:    protocol.TextDocumentSyncKindFull,
			Save: &protocol.SaveOptions{
				IncludeText: true,
			},
		}),
		InlayHintProvider: protocol.NewInlayHintProviderOptions(protocol.InlayHintOptions{
			ResolveProvider: false,
			WorkDoneProgressOptions: protocol.WorkDoneProgressOptions{
				WorkDoneProgress: false,
			},
		}),
	}}, nil
}

func (h *Handler) Initialized() error {
	log.Println("Initialized")
	return nil
}

func (h *Handler) Hover(p *protocol.HoverParams) (*protocol.Hover, error) {
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  "markdown",
			Value: "Hello from **your-lsp**!",
		},
	}, nil
}

func (h *Handler) InlayHint(p *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	return nil, nil
}

func (h *Handler) DidOpen(p *protocol.DidOpenTextDocumentParams) error {
	return nil
}

func (h *Handler) DidChange(p *protocol.DidChangeTextDocumentParams) error {
	return nil
}

func (h *Handler) DidClose(p *protocol.DidCloseTextDocumentParams) error {
	return nil
}

func (h *Handler) DidSave(p *protocol.DidSaveTextDocumentParams) error {
	return nil
}

func StartServer() {
	if err := NewHandler().Serve(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```
