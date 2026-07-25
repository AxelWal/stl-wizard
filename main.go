package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "STL Cutter",
		Width:            1280,
		Height:           800,
		MinWidth:         800,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 43, G: 43, B: 43, A: 255},
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: newPartHandler(app.session),
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	})
	if err != nil {
		panic(err)
	}
}
