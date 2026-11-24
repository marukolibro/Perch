// Perch is a lightweight native code reader and editor for working alongside
// coding agents.
package main

import (
	"log/slog"
	"os"

	"github.com/egoist/mygo"

	"github.com/marukolibro/Perch/internal/app"
)

func main() {
	if app.HandleCLIInfo(os.Args[1:], mygo.App.Version(), os.Stdout) {
		return
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	desktop := app.NewDesktop()
	mygo.App.WhenReady(func() {
		desktop.Start(os.Args[1:])
	})
	if err := mygo.App.Run(); err != nil {
		slog.Error("perch stopped", "error", err)
		os.Exit(1)
	}
}
