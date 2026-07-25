package main

import "context"

// App is the bound service. Every exported method on it is callable from the
// frontend, and together they are the application's entire command API.
type App struct {
	ctx     context.Context
	session *Session
}

func NewApp() *App { return &App{session: &Session{}} }

// startup stores the context Wails hands us. Every runtime call — dialogs,
// events — needs it.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Ping exists so there is something bound to call before the real methods land.
// Remove it once OpenModel is in place.
func (a *App) Ping() string { return "ok" }
