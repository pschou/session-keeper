package main

import (
	"fmt"

	"github.com/getlantern/systray"
	"github.com/getlantern/systray/example/icon"
)

func postSetup() {
	go systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTemplateIcon(icon.Data, icon.Data)
	//systray.SetIcon(icon.Data)
	systray.SetTitle("SessionKeeper " + *listen + "->" + *target)
	systray.SetTooltip("Session keeper " + *listen + "->" + *target)
	mQuit := systray.AddMenuItem("Quit", "Quit and close all connections")

	// Sets the icon of a menu item. Only available on Mac and Windows.
	//mQuit.SetIcon(icon.Data)
	go func() {
		for {
			select {
			case <-mQuit.ClickedCh:
				systray.Quit()
				fmt.Println("Quitting now...")
				return
			}
		}
	}()
}

func onExit() {
}
